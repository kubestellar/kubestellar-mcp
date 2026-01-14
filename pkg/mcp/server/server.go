package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/kubestellar/kubectl-claude/pkg/cluster"
)

const (
	MCPVersion = "2024-11-05"
	ServerName = "kubectl-claude"
	ServerVersion = "0.1.0"
)

// Server implements an MCP server over stdio
type Server struct {
	kubeconfig string
	discoverer *cluster.Discoverer
	reader     *bufio.Reader
	writer     io.Writer
	mu         sync.Mutex
}

// JSON-RPC types
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP types
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Items       *Items   `json:"items,omitempty"`
}

type Items struct {
	Type string `json:"type"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
	case "initialized":
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
		ProtocolVersion: MCPVersion,
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
	tools := []Tool{
		{
			Name:        "list_clusters",
			Description: "List all discovered Kubernetes clusters from kubeconfig and KubeStellar",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"source": {
						Type:        "string",
						Description: "Discovery source: all, kubeconfig, or kubestellar",
						Enum:        []string{"all", "kubeconfig", "kubestellar"},
					},
				},
			},
		},
		{
			Name:        "get_cluster_health",
			Description: "Check the health status of a Kubernetes cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Name of the cluster to check (uses current context if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_pods",
			Description: "List pods in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list pods from (all namespaces if not specified)",
					},
					"label_selector": {
						Type:        "string",
						Description: "Label selector to filter pods (e.g., app=nginx)",
					},
				},
			},
		},
		{
			Name:        "get_deployments",
			Description: "List deployments in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list deployments from (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_services",
			Description: "List services in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list services from (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_nodes",
			Description: "List nodes in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_events",
			Description: "Get recent events from a cluster, useful for troubleshooting",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to get events from (all namespaces if not specified)",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of events to return (default 50)",
					},
				},
			},
		},
		{
			Name:        "describe_pod",
			Description: "Get detailed information about a specific pod",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace of the pod",
					},
					"name": {
						Type:        "string",
						Description: "Name of the pod",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "get_pod_logs",
			Description: "Get logs from a pod",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace of the pod",
					},
					"name": {
						Type:        "string",
						Description: "Name of the pod",
					},
					"container": {
						Type:        "string",
						Description: "Container name (required if pod has multiple containers)",
					},
					"tail_lines": {
						Type:        "integer",
						Description: "Number of lines from the end to return (default 100)",
					},
				},
				Required: []string{"name"},
			},
		},
	}

	s.sendResult(req.ID, ToolsListResult{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, req *Request) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	var result string
	var isError bool

	switch params.Name {
	case "list_clusters":
		result, isError = s.toolListClusters(params.Arguments)
	case "get_cluster_health":
		result, isError = s.toolGetClusterHealth(params.Arguments)
	case "get_pods":
		result, isError = s.toolGetPods(ctx, params.Arguments)
	case "get_deployments":
		result, isError = s.toolGetDeployments(ctx, params.Arguments)
	case "get_services":
		result, isError = s.toolGetServices(ctx, params.Arguments)
	case "get_nodes":
		result, isError = s.toolGetNodes(ctx, params.Arguments)
	case "get_events":
		result, isError = s.toolGetEvents(ctx, params.Arguments)
	case "describe_pod":
		result, isError = s.toolDescribePod(ctx, params.Arguments)
	case "get_pod_logs":
		result, isError = s.toolGetPodLogs(ctx, params.Arguments)
	default:
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name), nil)
		return
	}

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

	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}
