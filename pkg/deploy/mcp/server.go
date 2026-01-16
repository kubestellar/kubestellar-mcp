package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kubestellar/klaude/pkg/multicluster"
)

const (
	ServerName    = "klaude-deploy"
	ServerVersion = "0.8.0"
)

// Server implements the MCP server for klaude-deploy
type Server struct {
	manager  *multicluster.ClientManager
	executor *multicluster.Executor
	selector *multicluster.Selector
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
		manager:  manager,
		executor: executor,
		selector: selector,
	}, nil
}

// MCPRequest represents an incoming MCP request
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse represents an outgoing MCP response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

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

// handleListTools returns the list of available tools
func (s *Server) handleListTools(req *MCPRequest) *MCPResponse {
	tools := []map[string]interface{}{
		{
			"name":        "get_app_instances",
			"description": "Find all instances of an app across all clusters. Returns where the app is running, replica counts, and health status.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name to search for (matches label app=<name> or name contains <name>)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace to search in (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
		},
		{
			"name":        "get_app_status",
			"description": "Get unified status of an app across all clusters. Shows health (healthy/degraded/failed), replica counts, and any issues.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
		},
		{
			"name":        "get_app_logs",
			"description": "Get aggregated logs from an app across all clusters. Logs are labeled with cluster name for easy identification.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines from end (default 100)",
					},
					"since": map[string]interface{}{
						"type":        "string",
						"description": "Only return logs newer than duration (e.g., 1h, 30m)",
					},
				},
				"required": []string{"app"},
			},
		},
		{
			"name":        "list_cluster_capabilities",
			"description": "List what each cluster can run: GPU availability, CPU/memory capacity, node labels. Use this to understand cluster resources.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Specific cluster (all clusters if not specified)",
					},
				},
			},
		},
		{
			"name":        "find_clusters_for_workload",
			"description": "Find clusters that can run a workload with specific requirements (GPU, memory, CPU, labels).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "GPU type required (e.g., nvidia.com/gpu)",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Minimum number of GPUs required",
					},
					"min_memory": map[string]interface{}{
						"type":        "string",
						"description": "Minimum memory required (e.g., 16Gi)",
					},
					"min_cpu": map[string]interface{}{
						"type":        "string",
						"description": "Minimum CPU required (e.g., 4)",
					},
					"labels": map[string]interface{}{
						"type":        "object",
						"description": "Required node labels",
					},
				},
			},
		},
		{
			"name":        "deploy_app",
			"description": "Deploy an app to clusters. Can specify clusters explicitly or let klaude find matching clusters based on requirements.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"manifest": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes manifest (YAML)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all matching clusters if not specified)",
					},
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "Deploy to clusters with this GPU type",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Deploy to clusters with at least this many GPUs",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
				},
				"required": []string{"manifest"},
			},
		},
		{
			"name":        "scale_app",
			"description": "Scale an app across clusters. Can target specific clusters or all clusters where app runs.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Target replica count",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters where app runs if not specified)",
					},
				},
				"required": []string{"app", "replicas"},
			},
		},
		{
			"name":        "patch_app",
			"description": "Apply a patch to an app across clusters.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"patch": map[string]interface{}{
						"type":        "string",
						"description": "JSON or strategic merge patch",
					},
					"patch_type": map[string]interface{}{
						"type":        "string",
						"description": "Patch type: strategic, merge, or json (default: strategic)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters",
					},
				},
				"required": []string{"app", "patch"},
			},
		},
		// GitOps Tools
		{
			"name":        "detect_drift",
			"description": "Detect drift between git manifests and cluster state. Shows which resources differ between git and what's deployed.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL (e.g., https://github.com/org/manifests)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests (e.g., production/)",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
		},
		{
			"name":        "sync_from_git",
			"description": "Sync manifests from a git repository to clusters. Applies all manifests found in the specified path.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Override namespace for all resources",
					},
				},
				"required": []string{"repo"},
			},
		},
		{
			"name":        "reconcile",
			"description": "Bring clusters back in sync with git. Same as sync_from_git but always applies changes.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
		},
		{
			"name":        "preview_changes",
			"description": "Preview what would change if manifests were synced from git. Dry-run mode.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
		},
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

	switch params.Name {
	case "get_app_instances":
		result, err = s.handleGetAppInstances(ctx, params.Arguments)
	case "get_app_status":
		result, err = s.handleGetAppStatus(ctx, params.Arguments)
	case "get_app_logs":
		result, err = s.handleGetAppLogs(ctx, params.Arguments)
	case "list_cluster_capabilities":
		result, err = s.handleListClusterCapabilities(ctx, params.Arguments)
	case "find_clusters_for_workload":
		result, err = s.handleFindClustersForWorkload(ctx, params.Arguments)
	case "deploy_app":
		result, err = s.handleDeployApp(ctx, params.Arguments)
	case "scale_app":
		result, err = s.handleScaleApp(ctx, params.Arguments)
	case "patch_app":
		result, err = s.handlePatchApp(ctx, params.Arguments)
	// GitOps tools
	case "detect_drift":
		result, err = s.handleDetectDrift(ctx, params.Arguments)
	case "sync_from_git":
		result, err = s.handleSyncFromGit(ctx, params.Arguments)
	case "reconcile":
		result, err = s.handleReconcile(ctx, params.Arguments)
	case "preview_changes":
		result, err = s.handlePreviewChanges(ctx, params.Arguments)
	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: fmt.Sprintf("Unknown tool: %s", params.Name)},
		}
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
