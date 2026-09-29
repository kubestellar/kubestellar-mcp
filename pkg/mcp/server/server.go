package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/internal/version"
	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/rpcloop"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

const (
	ServerName = "kubestellar-ops"
	MCPVersion = protocol.MCPVersion
)

// ServerVersion is reported in the MCP initialize handshake under
// serverInfo.version. It shadows internal/version.Version so the handshake and
// the CLI --version flag stay in lock-step; internal/version.Version is
// populated at build time by ldflags (see Makefile). Declared as a var rather
// than a const because version.Version is itself a var.
var ServerVersion = version.Version

// Type aliases so tool registry files continue to compile unchanged.
type (
	Request          = protocol.Request
	Response         = protocol.Response
	Error            = protocol.Error
	ServerInfo       = protocol.ServerInfo
	InitializeResult = protocol.InitializeResult
	Capabilities     = protocol.Capabilities
	ToolsCapability  = protocol.ToolsCapability
	Tool             = protocol.Tool
	InputSchema      = protocol.InputSchema
	Property         = protocol.Property
	Items            = protocol.Items
	ToolsListResult  = protocol.ToolsListResult
	CallToolParams   = protocol.CallToolParams
	CallToolResult   = protocol.CallToolResult
	ContentBlock     = protocol.ContentBlock
)

// Server implements an MCP server over stdio. It owns the protocol boundary
// (reader/writer/mutex) plus the injectable factories that are handed to tool
// handlers as a *handlers.Deps; handlers never see *Server itself.
type Server struct {
	kubeconfig    string
	discoverer    handlers.Discoverer
	clientFactory func(clusterName string) (kubernetes.Interface, error)
	// restConfigFactory is an injectable factory for REST configs.
	// When nil, Deps.GetRESTConfigForCluster falls back to loading kubeconfig.
	restConfigFactory func(clusterName string) (*rest.Config, error)
	// dynamicClientFactory is an injectable factory for dynamic clients.
	// When nil, Deps.GetDynamicClientForCluster falls back to building a
	// real client from kubeconfig. Tests set this to inject a fake.
	dynamicClientFactory  func(clusterName string) (dynamic.Interface, error)
	manifestReaderFactory func() handlers.ManifestReader
	driftDetectorFactory  func(config *rest.Config) (handlers.DriftDetector, error)
	// reader is the raw request stream; framing (newline-delimited frames
	// bounded by rpcloop.DefaultMaxFrameSize) is owned by pkg/mcp/rpcloop
	// rather than by this package's own buffered reader. It was previously
	// an uncapped bufio.Reader.ReadBytes('\n') read - see
	// kubestellar-mcp#1017.
	reader io.Reader
	writer io.Writer
	// mu serializes every write to writer, including the shared rpcloop
	// Loop's own writes (Run hands it to Loop.SetWriteMutex), so read-loop
	// responses and direct send calls share one serialization domain.
	mu sync.Mutex
}

// deps projects the server's injectable dependencies into the *handlers.Deps
// that tool handlers receive. It is built per call so that factories set on
// the Server after construction (as tests do) are always observed.
func (s *Server) deps() *handlers.Deps {
	return &handlers.Deps{
		Kubeconfig:            s.kubeconfig,
		Discoverer:            s.discoverer,
		ClientFactory:         s.clientFactory,
		DynamicClientFactory:  s.dynamicClientFactory,
		RESTConfigFactory:     s.restConfigFactory,
		ManifestReaderFactory: s.manifestReaderFactory,
		DriftDetectorFactory:  s.driftDetectorFactory,
	}
}

// NewServer creates a new MCP server
func NewServer(kubeconfig string) *Server {
	return &Server{
		kubeconfig: kubeconfig,
		discoverer: cluster.NewDiscoverer(kubeconfig),
		reader:     os.Stdin,
		writer:     os.Stdout,
	}
}

// Run starts the MCP server. The stdio transport - newline-delimited
// JSON-RPC framing bounded by rpcloop.DefaultMaxFrameSize, parse-error
// replies, context cancellation and EOF-as-clean-shutdown - is owned by
// pkg/mcp/rpcloop, shared with the sibling kubestellar-deploy server (see
// kubestellar-mcp#1017). Handlers keep writing their responses through
// s.send, and the Loop writes its own parse-error replies under the same
// s.mu, so all output to s.writer stays serialized on one mutex.
func (s *Server) Run(ctx context.Context) error {
	loop := rpcloop.NewLoop(s.reader, s.writer)
	loop.SetWriteMutex(&s.mu)

	err := loop.Run(ctx, func(ctx context.Context, req *protocol.Request) *protocol.Response {
		s.handleRequest(ctx, req)
		// Responses are written by s.send from within handleRequest, so
		// the Loop itself has nothing left to write for this request.
		return nil
	})

	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		// Preserves this server's pre-existing read-failure wrapping.
		return fmt.Errorf("failed to read request: %w", err)
	}
}

func (s *Server) handleRequest(ctx context.Context, req *Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		// No response needed for notification
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	case "ping":
		s.sendResult(req.ID, map[string]interface{}{})
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

func (s *Server) handleInitialize(req *Request) {
	result := InitializeResult{
		ProtocolVersion: protocol.MCPVersion,
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	s.sendResult(req.ID, result)
}

func (s *Server) handleToolsList(req *Request) {
	s.sendResult(req.ID, ToolsListResult{Tools: registeredTools()})
}

func (s *Server) handleToolsCall(ctx context.Context, req *Request) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	// Bounded before use as a span attribute or metrics/log label: raw
	// tool-call arguments are client-controlled, so unrecognized cluster
	// names are mapped to a fixed label (see boundedClusterLabel).
	cluster := s.boundedClusterLabel(clusterArg(params.Arguments))

	var result string
	var isError bool

	// The span/timing/metrics/structured-logging wrapper used to be
	// hand-written here and mirrored, statement for statement, in the
	// sibling kubestellar-deploy server; it now lives once in
	// rpcloop.InstrumentToolCall (see kubestellar-mcp#1017). tool.name is
	// bounded (a registered tool name or the unknown-tool arm below) and
	// cluster is bounded by boundedClusterLabel, so neither can widen the
	// span-attribute or metrics label space.
	outcome := rpcloop.InstrumentToolCall(ctx, params.Name, cluster, func(ctx context.Context) rpcloop.ToolCallOutcome {
		handler := findToolHandler(params.Name)
		if handler == nil {
			return rpcloop.ToolCallOutcome{Found: false}
		}

		start := time.Now()
		result, isError = handler(ctx, s.deps(), params.Arguments)
		return rpcloop.ToolCallOutcome{
			Found:    true,
			IsError:  isError,
			ErrKind:  errKindFromContext(ctx),
			Duration: time.Since(start),
		}
	})

	if !outcome.Found {
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name), nil)
		return
	}

	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: result}},
		IsError: isError,
	})
}

// errKindFromContext classifies a tool-call error using only the objective
// context signal available at the handleToolsCall call site: whether ctx was
// canceled or hit its deadline by the time the handler returned. It returns
// metrics.ErrorKindTimeout in that case, and "" otherwise (which
// RecordToolCall normalizes to metrics.ErrorKindUnknown for actual errors).
// This does not classify k8s_api or marshal errors - see tracked issue #748
// for why that requires wider changes across ~30+ tool handlers.
func errKindFromContext(ctx context.Context) metrics.ErrorKind {
	if ctx.Err() != nil {
		return metrics.ErrorKindTimeout
	}
	return ""
}

// clusterArg extracts a "cluster" argument from tool call arguments, if
// present, so single-cluster-scoped calls can be attributed to that cluster
// in metrics. It returns "" when absent, which RecordToolCall normalizes to
// a bounded "none" label value.
func clusterArg(args map[string]interface{}) string {
	if v, ok := args["cluster"].(string); ok {
		return v
	}
	return ""
}

// otherClusterLabel is the bounded label value used in place of a
// caller-supplied cluster name that does not match any cluster known to
// this server's kubeconfig.
const otherClusterLabel = "other"

// boundedClusterLabel validates a caller-supplied cluster name against this
// server's discovered kubeconfig contexts before it is used as a Prometheus
// metrics or log label. Tool-call arguments are entirely client-controlled,
// so passing clusterArg's return value through unchecked would let a caller
// generate an unbounded number of distinct label values (one per arbitrary
// string it sends), regardless of the metrics package's "cluster names are
// capped by the discovered cluster set" invariant. Unrecognized names are
// mapped to the fixed otherClusterLabel value; "" (no cluster argument) is
// passed through unchanged so RecordToolCall can normalize it to "none".
func (s *Server) boundedClusterLabel(cluster string) string {
	if cluster == "" {
		return cluster
	}
	if s.discoverer == nil {
		return otherClusterLabel
	}
	known, err := s.discoverer.DiscoverClusters("kubeconfig")
	if err != nil {
		return otherClusterLabel
	}
	for _, c := range known {
		if c.Name == cluster {
			return cluster
		}
	}
	return otherClusterLabel
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.send(*protocol.NewResult(id, result))
}

func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	s.send(*protocol.NewError(id, code, message, data))
}

// send writes resp to the transport, serialized against every other writer
// (including the rpcloop Loop's own parse-error replies) by s.mu. A response
// that cannot be marshaled is logged and dropped rather than emitting a
// partial frame.
func (s *Server) send(resp Response) {
	if err := rpcloop.SendResponse(&s.mu, s.writer, &resp); err != nil {
		klog.Errorf("Failed to marshal MCP response: %v", err)
	}
}
