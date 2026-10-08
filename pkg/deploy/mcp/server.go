package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/internal/version"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/rpcloop"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

const (
	ServerName = "kubestellar-deploy"
)

// ServerVersion is reported in the MCP initialize handshake under
// serverInfo.version. It shadows internal/version.Version so the handshake and
// the CLI --version flag stay in lock-step; internal/version.Version is
// populated at build time by ldflags (see Makefile). Declared as a var rather
// than a const because version.Version is itself a var.
var ServerVersion = version.Version

type manifestSyncer interface {
	Sync(ctx context.Context, manifests []gitops.Manifest, clusterName string, opts gitops.SyncOptions) (*gitops.SyncSummary, error)
}

type driftDetector interface {
	DetectDrift(ctx context.Context, manifests []gitops.Manifest, clusterName string) ([]gitops.DriftResult, error)
}

// Server implements the MCP server for kubestellar-deploy
type Server struct {
	manager  *multicluster.ClientManager
	executor *multicluster.Executor
	selector *multicluster.Selector
	// newManifestReader is a factory for creating manifest readers.
	// Tests can override this to allow file:// URLs for local test repos.
	newManifestReader func() *gitops.ManifestReader
	// newManifestSyncer is a factory for creating manifest syncers.
	// Tests can override this to avoid talking to a real API server.
	newManifestSyncer func(*rest.Config) (manifestSyncer, error)
	// newDriftDetector is a factory for creating drift detectors.
	// Tests can override this to avoid talking to a real API server.
	newDriftDetector func(*rest.Config) (driftDetector, error)
	// writeMu serializes every write to stdout via Loop.SetWriteMutex in
	// Run, guarding the shared rpcloop.Loop's writes. Previously this
	// server had no write-safety guarantee at all here; see
	// kubestellar-mcp#1017/#1018.
	writeMu sync.Mutex
	// discoveryTimer measures SLO 2 (Cluster Discovery Latency, docs/slo.md)
	// from this server's handleInitialize to its first handleListTools.
	discoveryTimer rpcloop.DiscoveryTimer
}

// NewServer creates a new MCP server
func NewServer() (*Server, error) {
	manager, err := multicluster.NewClientManager("")
	if err != nil {
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	executor := multicluster.NewExecutor(manager)
	selector := multicluster.NewSelector(executor)

	return &Server{
		manager:           manager,
		executor:          executor,
		selector:          selector,
		newManifestReader: gitops.NewManifestReader,
		newManifestSyncer: func(config *rest.Config) (manifestSyncer, error) {
			return gitops.NewSyncer(config)
		},
		newDriftDetector: func(config *rest.Config) (driftDetector, error) {
			return gitops.NewDriftDetector(config)
		},
	}, nil
}

// getManifestReader returns a manifest reader using the configured factory.
func (s *Server) getManifestReader() *gitops.ManifestReader {
	if s.newManifestReader != nil {
		return s.newManifestReader()
	}
	return gitops.NewManifestReader()
}

func (s *Server) getManifestSyncer(config *rest.Config) (manifestSyncer, error) {
	if s.newManifestSyncer != nil {
		return s.newManifestSyncer(config)
	}
	return gitops.NewSyncer(config)
}

// getDriftDetector returns a drift detector using the configured factory.
func (s *Server) getDriftDetector(config *rest.Config) (driftDetector, error) {
	if s.newDriftDetector != nil {
		return s.newDriftDetector(config)
	}
	return gitops.NewDriftDetector(config)
}

// RunMCPServer starts the MCP server on stdin/stdout, cancellable via ctx.
// Cancellation on ctx propagates through the rpcloop.Loop into every
// in-flight tool handler; passing context.Background() preserves the
// previous "runs until stdin EOF" behaviour verbatim.
func RunMCPServer(ctx context.Context) error {
	server, err := NewServer()
	if err != nil {
		return err
	}
	return server.Run(ctx)
}

// Run starts the server loop. The stdio transport (newline-delimited
// JSON-RPC framing up to rpcloop.DefaultMaxFrameSize, EOF-as-clean-shutdown)
// is owned by pkg/mcp/rpcloop, shared with the sibling kubestellar-mcp
// server's dispatch loop (see kubestellar-mcp#1017/#1018). os.Stdin/os.Stdout
// are read at call time (not cached on Server) so tests that swap them
// before calling Run continue to work unchanged. The ctx passed in flows
// through the loop into handleRequest → rpcloop.Dispatch → the ToolsCall
// handler chain so a SIGINT/SIGTERM or per-caller deadline can actually
// unblock an in-flight tool handler (kubestellar-mcp#1077).
func (s *Server) Run(ctx context.Context) error {
	loop := rpcloop.NewLoop(os.Stdin, os.Stdout)
	loop.SetWriteMutex(&s.writeMu)
	return loop.Run(ctx, func(ctx context.Context, req *protocol.Request) *protocol.Response {
		return s.handleRequest(ctx, req)
	})
}

// handleRequest processes an MCP request and returns a response. The method
// table (lifecycle notifications, ping, unknown-method errors) is shared with
// the kubestellar-ops server via rpcloop.Dispatch (see kubestellar-mcp#1017);
// this server supplies only its initialize, tools/list and tools/call
// handlers. ctx is threaded from Run's loop callback into Dispatch so
// cancellation reaches handleToolCall's InstrumentToolCall (kubestellar-mcp#1077).
func (s *Server) handleRequest(ctx context.Context, req *protocol.Request) *protocol.Response {
	return rpcloop.Dispatch(ctx, req, rpcloop.Methods{
		Initialize: func(_ context.Context, req *protocol.Request) *protocol.Response {
			s.discoveryTimer.Initialize()
			return s.handleInitialize(req)
		},
		ToolsList: func(_ context.Context, req *protocol.Request) *protocol.Response {
			resp := s.handleListTools(req)
			s.discoveryTimer.ToolsList()
			return resp
		},
		ToolsCall: s.handleToolCall,
	})
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(req *protocol.Request) *protocol.Response {
	return protocol.NewResult(req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    ServerName,
			"version": ServerVersion,
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
	})
}

// handleListTools returns the list of available tools. Each tool's schema is
// contributed by the tools_*.go file that also implements its handler (see
// registry.go); this loop just projects those definitions into the MCP
// tools/list response shape, preserving registration order.
func (s *Server) handleListTools(req *protocol.Request) *protocol.Response {
	defs := s.toolDefs()
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"name":        d.Name,
			"description": d.Description,
			"inputSchema": d.InputSchema,
		})
	}

	return protocol.NewResult(req.ID, map[string]interface{}{
		"tools": tools,
	})
}

// handleToolCall dispatches tool calls to handlers
func (s *Server) handleToolCall(ctx context.Context, req *protocol.Request) *protocol.Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return protocol.NewError(req.ID, -32602, "Invalid params", nil)
	}

	var result interface{}
	var err error

	// The span/timing/metrics/structured-logging wrapper below used to be
	// hand-duplicated here (with a comment pointing at the sibling
	// kubestellar-mcp server's identical block) - it now lives once in
	// pkg/mcp/rpcloop.InstrumentToolCall (see kubestellar-mcp#1017/#1018).
	// dispatch performs the single map lookup against the registry built
	// from every tools_*.go file's *ToolDefs() (see registry.go), then
	// invokes the handler using the traced ctx InstrumentToolCall hands it.
	// No per-request cluster scoping is available at this dispatch point,
	// so cluster is left empty and normalized to the bounded "none" label
	// by metrics.RecordToolCall.
	outcome := rpcloop.InstrumentToolCall(ctx, params.Name, "", func(ctx context.Context) rpcloop.ToolCallOutcome {
		def, ok := s.findToolDef(params.Name)
		if !ok {
			return rpcloop.ToolCallOutcome{Found: false}
		}
		start := time.Now()
		result, err = def.Handler(ctx, params.Arguments)
		errKind := metrics.ErrorKind("")
		if err != nil {
			errKind = metrics.ClassifyError(err)
		}
		return rpcloop.ToolCallOutcome{
			Found:    true,
			IsError:  err != nil,
			ErrKind:  errKind,
			Duration: time.Since(start),
		}
	})

	if !outcome.Found {
		return protocol.NewError(req.ID, -32601, fmt.Sprintf("Unknown tool: %s", params.Name), nil)
	}

	if err != nil {
		return protocol.NewResult(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Error: %v", err),
				},
			},
			"isError": true,
		})
	}

	// Format result as MCP content
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return protocol.NewResult(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(resultJSON),
			},
		},
	})
}
