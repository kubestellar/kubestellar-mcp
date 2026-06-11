package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

const (
	ServerName    = "kubestellar-ops"
	ServerVersion = "0.8.0"
)

// Type aliases so tool registry files continue to compile unchanged.
type (
	Request         = protocol.Request
	Response        = protocol.Response
	Error           = protocol.Error
	ServerInfo      = protocol.ServerInfo
	InitializeResult = protocol.InitializeResult
	Capabilities    = protocol.Capabilities
	ToolsCapability = protocol.ToolsCapability
	Tool            = protocol.Tool
	InputSchema     = protocol.InputSchema
	Property        = protocol.Property
	Items           = protocol.Items
	ToolsListResult = protocol.ToolsListResult
	CallToolParams  = protocol.CallToolParams
	CallToolResult  = protocol.CallToolResult
	ContentBlock    = protocol.ContentBlock
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

	result, isError := handler(ctx, s, params.Arguments)
	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: result}},
		IsError: isError,
	})
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
		log.Printf("Failed to marshal MCP response: %v", err)
		return
	}
	_, _ = fmt.Fprintf(s.writer, "%s\n", data)
}
