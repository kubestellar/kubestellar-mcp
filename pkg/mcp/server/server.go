package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

const (
	ServerName    = "kubestellar-ops"
	ServerVersion = "0.8.0"
	MCPVersion    = protocol.MCPVersion
)

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

type discoverer interface {
	DiscoverClusters(source string) ([]cluster.ClusterInfo, error)
	CheckHealthByContext(contextName string) (*cluster.HealthInfo, error)
}

// Server implements an MCP server over stdio
type Server struct {
	kubeconfig    string
	discoverer    discoverer
	clientFactory func(clusterName string) (kubernetes.Interface, error)
	// restConfigFactory is an injectable factory for REST configs.
	// When nil, getRestConfigForCluster falls back to loading kubeconfig.
	restConfigFactory func(clusterName string) (*rest.Config, error)
	// dynamicClientFactory is an injectable factory for dynamic clients.
	// When nil, getDynamicClientForCluster falls back to building a real
	// client from kubeconfig. Tests set this to inject a fake.
	dynamicClientFactory  func(clusterName string) (dynamic.Interface, error)
	manifestReaderFactory func() manifestReader
	driftDetectorFactory  func(config *rest.Config) (driftDetector, error)
	reader                *bufio.Reader
	writer                io.Writer
	mu                    sync.Mutex

	// clusterLabelMu guards the cached set of known cluster names/contexts
	// used to bound the "cluster" metric label (see clusterMetricLabel).
	clusterLabelMu       sync.Mutex
	clusterLabelSet      map[string]bool
	clusterLabelCachedAt time.Time
}

// NewServer creates a new MCP server
func NewServer(kubeconfig string) *Server {
	return &Server{
		kubeconfig: kubeconfig,
		discoverer: cluster.NewDiscoverer(kubeconfig),
		reader:     bufio.NewReader(os.Stdin),
		writer:     os.Stdout,
	}
}

// Run starts the MCP server
func (s *Server) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read request: %w", err)
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", nil)
			continue
		}

		s.handleRequest(ctx, &req)
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

	handler := findToolHandler(params.Name)
	if handler == nil {
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name), nil)
		return
	}

	start := time.Now()
	result, isError := handler(ctx, s, params.Arguments)
	metrics.RecordToolCall(params.Name, s.clusterMetricLabel(params.Arguments), time.Since(start), isError, "")

	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: result}},
		IsError: isError,
	})
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

// clusterLabelCacheTTL bounds how often clusterMetricLabel re-reads cluster
// names from the discoverer, so validating the "cluster" metric label
// doesn't add discovery overhead to every tool call.
const clusterLabelCacheTTL = 30 * time.Second

// unrecognizedClusterLabel is the bounded "cluster" label value recorded
// when a tool call's "cluster" argument does not match any cluster known to
// the discoverer.
const unrecognizedClusterLabel = "unrecognized"

// clusterMetricLabel returns a bounded "cluster" label value for tool-call
// metrics. The "cluster" tool argument is supplied by the calling client
// (an LLM, in practice) and is not otherwise validated before this point;
// forwarding it straight into a Prometheus label would let an arbitrary,
// unbounded string flow into metric label values, risking cardinality
// explosion. This caps the label to the set of clusters the discoverer
// currently knows about (refreshed at most every clusterLabelCacheTTL) plus
// two closed sentinels ("" -> normalized to "none" by RecordToolCall, and
// unrecognizedClusterLabel for anything else), matching the bounded-label
// invariant documented in pkg/metrics.
func (s *Server) clusterMetricLabel(args map[string]interface{}) string {
	raw := clusterArg(args)
	if raw == "" {
		return ""
	}
	if known := s.knownClusterNames(); known != nil && known[raw] {
		return raw
	}
	return unrecognizedClusterLabel
}

// knownClusterNames returns the set of cluster names and contexts most
// recently reported by the discoverer, caching the result for
// clusterLabelCacheTTL. It returns nil if no discoverer is configured or no
// successful discovery has completed yet.
func (s *Server) knownClusterNames() map[string]bool {
	s.clusterLabelMu.Lock()
	defer s.clusterLabelMu.Unlock()

	if s.discoverer == nil {
		return nil
	}
	if s.clusterLabelSet != nil && time.Since(s.clusterLabelCachedAt) < clusterLabelCacheTTL {
		return s.clusterLabelSet
	}

	clusters, err := s.discoverer.DiscoverClusters("all")
	if err != nil {
		// Keep serving the previous cache (if any) rather than treating a
		// transient discovery failure as "no clusters known", which would
		// relabel every in-flight call as unrecognized.
		return s.clusterLabelSet
	}

	set := make(map[string]bool, len(clusters)*2)
	for _, c := range clusters {
		if c.Name != "" {
			set[c.Name] = true
		}
		if c.Context != "" {
			set[c.Context] = true
		}
	}
	s.clusterLabelSet = set
	s.clusterLabelCachedAt = time.Now()
	return set
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	s.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (s *Server) send(resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		klog.Errorf("Failed to marshal MCP response: %v", err)
		return
	}
	_, _ = fmt.Fprintf(s.writer, "%s\n", data)
}
