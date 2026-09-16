package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

const (
	ServerName    = "kubestellar-deploy"
	ServerVersion = "0.8.0"
)

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

// Type aliases from shared protocol package.
type (
	MCPRequest  = protocol.Request
	MCPResponse = protocol.Response
	MCPError    = protocol.Error
)

// RunMCPServer starts the MCP server on stdin/stdout
func RunMCPServer() error {
	server, err := NewServer()
	if err != nil {
		return err
	}
	return server.Run()
}

// Run starts the server loop
func (s *Server) Run() error {
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		response := s.handleRequest(&req)
		if response != nil {
			s.sendResponse(response)
		}
	}

	return scanner.Err()
}

// handleRequest processes an MCP request and returns a response
func (s *Server) handleRequest(req *MCPRequest) *MCPResponse {
	ctx := context.Background()

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleToolCall(ctx, req)
	case "initialized", "notifications/initialized":
		// No response needed for MCP lifecycle notification
		return nil
	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: "Method not found"},
		}
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(req *MCPRequest) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    ServerName,
				"version": ServerVersion,
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
		},
	}
}

// handleListTools returns the list of available tools. Each tool's schema is
// contributed by the tools_*.go file that also implements its handler (see
// registry.go); this loop just projects those definitions into the MCP
// tools/list response shape, preserving registration order.
func (s *Server) handleListTools(req *MCPRequest) *MCPResponse {
	defs := s.toolDefs()
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"name":        d.Name,
			"description": d.Description,
			"inputSchema": d.InputSchema,
		})
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

// handleToolCall dispatches tool calls to handlers
func (s *Server) handleToolCall(ctx context.Context, req *MCPRequest) *MCPResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: "Invalid params"},
		}
	}

	var result interface{}
	var err error

	// Root span for the tool-dispatch request path, matching the sibling
	// kubestellar-mcp server's instrumentation of
	// pkg/mcp/server.handleToolsCall (see tracing.go for why this is a
	// free, no-op span unless an operator registers a TracerProvider).
	// tool.name is set from the client-supplied value, same as the
	// sibling server's span attribute; it is only ever used as a trace
	// attribute here, never as a Prometheus metric label, so unbounded
	// cardinality does not apply.
	ctx, span := tracer.Start(ctx, "mcp.tool.call", trace.WithAttributes(
		attribute.String("tool.name", params.Name),
	))
	defer span.End()

	// start/duration bracket the dispatched handler call below so every
	// recognized tool (fixed switch-case set) is timed and recorded via
	// metrics.RecordToolCall, matching the sibling kubestellar-mcp server's
	// instrumentation of pkg/mcp/server.handleToolsCall. The default
	// (unrecognized-tool) arm returns before this point, so a
	// client-supplied tool name can never reach RecordToolCall as a label
	// value. No per-request cluster scoping is available at this dispatch
	// point, so cluster is left empty and normalized to the bounded "none"
	// label by RecordToolCall.
	start := time.Now()

	// Single map lookup against the registry built from every tools_*.go
	// file's *ToolDefs() (see registry.go), replacing what used to be a
	// 27-case switch duplicating the tool names already listed in
	// handleListTools.
	def, ok := s.findToolDef(params.Name)
	if !ok {
		span.SetStatus(codes.Error, "unknown tool")
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: fmt.Sprintf("Unknown tool: %s", params.Name)},
		}
	}
	result, err = def.Handler(ctx, params.Arguments)

	errKind := metrics.ErrorKind("")
	if err != nil {
		errKind = metrics.ClassifyError(err)
	}
	duration := time.Since(start)
	metrics.RecordToolCall(params.Name, "", duration, err != nil, errKind)

	// Structured, bounded lifecycle logging mirroring the sibling
	// kubestellar-mcp server (pkg/mcp/server.handleToolsCall): tool comes
	// from the fixed switch-case set reached above, so this never logs
	// raw error text - only the same status/timing data already exposed
	// via metrics. Uses klog's key/value form (InfoS/ErrorS) so the
	// fields are structured rather than baked into a free-form message.
	if err != nil {
		span.SetStatus(codes.Error, "tool call returned an error result")
		klog.ErrorS(nil, "tool call failed", "tool", params.Name, "duration", duration)
	} else {
		klog.V(2).InfoS("tool call succeeded", "tool", params.Name, "duration", duration)
	}

	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Error: %v", err),
					},
				},
				"isError": true,
			},
		}
	}

	// Format result as MCP content
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(resultJSON),
				},
			},
		},
	}
}

// sendResponse writes a response to stdout
func (s *Server) sendResponse(resp *MCPResponse) {
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

// sendError sends an error response
func (s *Server) sendError(id interface{}, code int, message string) {
	resp := &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: code, Message: message},
	}
	s.sendResponse(resp)
}
