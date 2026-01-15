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
		// RBAC Tools
		{
			Name:        "get_roles",
			Description: "List Roles in a namespace",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list roles from (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_cluster_roles",
			Description: "List ClusterRoles in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"include_system": {
						Type:        "string",
						Description: "Include system ClusterRoles (true/false, default false)",
					},
				},
			},
		},
		{
			Name:        "get_role_bindings",
			Description: "List RoleBindings in a namespace",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to list role bindings from (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_cluster_role_bindings",
			Description: "List ClusterRoleBindings in a cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"include_system": {
						Type:        "string",
						Description: "Include system ClusterRoleBindings (true/false, default false)",
					},
				},
			},
		},
		{
			Name:        "can_i",
			Description: "Check if a subject can perform an action on a resource (similar to kubectl auth can-i)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"verb": {
						Type:        "string",
						Description: "The action verb (get, list, create, update, delete, watch, etc.)",
					},
					"resource": {
						Type:        "string",
						Description: "The resource type (pods, deployments, secrets, etc.)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for the check (empty for cluster-scoped)",
					},
					"subresource": {
						Type:        "string",
						Description: "Subresource (e.g., logs, status)",
					},
					"name": {
						Type:        "string",
						Description: "Specific resource name to check",
					},
				},
				Required: []string{"verb", "resource"},
			},
		},
		{
			Name:        "analyze_subject_permissions",
			Description: "Analyze all RBAC permissions for a specific subject (user, group, or service account)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"subject_kind": {
						Type:        "string",
						Description: "Kind of subject: User, Group, or ServiceAccount",
						Enum:        []string{"User", "Group", "ServiceAccount"},
					},
					"subject_name": {
						Type:        "string",
						Description: "Name of the subject",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for ServiceAccount subjects",
					},
				},
				Required: []string{"subject_kind", "subject_name"},
			},
		},
		{
			Name:        "describe_role",
			Description: "Get detailed information about a Role or ClusterRole including all rules",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"name": {
						Type:        "string",
						Description: "Name of the Role or ClusterRole",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace for Role (omit for ClusterRole)",
					},
				},
				Required: []string{"name"},
			},
		},
		// Diagnostic Tools
		{
			Name:        "find_pod_issues",
			Description: "Find pods with issues like CrashLoopBackOff, ImagePullBackOff, Pending, OOMKilled, or restarts",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
					"include_completed": {
						Type:        "string",
						Description: "Include completed/succeeded pods (true/false, default false)",
					},
				},
			},
		},
		{
			Name:        "find_deployment_issues",
			Description: "Find deployments with issues like unavailable replicas, stuck rollouts, or misconfigurations",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "check_resource_limits",
			Description: "Find pods/containers without CPU or memory limits/requests configured",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "check_security_issues",
			Description: "Find security misconfigurations: privileged containers, running as root, host network/PID, missing security context",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "analyze_namespace",
			Description: "Comprehensive namespace analysis: resource quotas, limit ranges, pod count, issues summary",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to analyze",
					},
				},
				Required: []string{"namespace"},
			},
		},
		{
			Name:        "get_warning_events",
			Description: "Get only Warning events, filtered by namespace or resource",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
					"involved_object": {
						Type:        "string",
						Description: "Filter by involved object name",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of events (default 50)",
					},
				},
			},
		},
		{
			Name:        "audit_kubeconfig",
			Description: "Audit all clusters in kubeconfig: check connectivity, identify stale/inaccessible clusters, and recommend cleanup",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"timeout_seconds": {
						Type:        "integer",
						Description: "Connection timeout in seconds per cluster (default 5)",
					},
				},
			},
		},
		{
			Name:        "find_resource_owners",
			Description: "Find who owns/manages resources by checking managedFields, ownership labels, and annotations",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (required)",
					},
					"resource_type": {
						Type:        "string",
						Description: "Resource type to check: pods, deployments, services, all (default: all)",
					},
				},
				Required: []string{"namespace"},
			},
		},
		// OPA Gatekeeper Tools
		{
			Name:        "check_gatekeeper",
			Description: "Check if OPA Gatekeeper is installed and running in the cluster",
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
			Name:        "get_ownership_policy_status",
			Description: "Get the status of the ownership labels policy including violation count",
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
			Name:        "list_ownership_violations",
			Description: "List resources that violate the ownership labels policy (missing owner/team labels)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Filter violations by namespace",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of violations to return (default 50)",
					},
				},
			},
		},
		{
			Name:        "install_ownership_policy",
			Description: "Install the ownership labels policy (ConstraintTemplate and Constraint) for OPA Gatekeeper",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"labels": {
						Type:        "array",
						Description: "Required labels (default: [\"owner\", \"team\"])",
						Items:       &Items{Type: "string"},
					},
					"target_namespaces": {
						Type:        "array",
						Description: "Namespaces to enforce (empty means all non-system namespaces)",
						Items:       &Items{Type: "string"},
					},
					"exclude_namespaces": {
						Type:        "array",
						Description: "Namespaces to exclude (default: kube-*, openshift-*, gatekeeper-system)",
						Items:       &Items{Type: "string"},
					},
					"mode": {
						Type:        "string",
						Description: "Enforcement mode: dryrun, warn, or enforce (default: dryrun)",
						Enum:        []string{"dryrun", "warn", "enforce"},
					},
				},
			},
		},
		{
			Name:        "set_ownership_policy_mode",
			Description: "Change the enforcement mode of the ownership labels policy",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"mode": {
						Type:        "string",
						Description: "Enforcement mode: dryrun, warn, or enforce",
						Enum:        []string{"dryrun", "warn", "enforce"},
					},
				},
				Required: []string{"mode"},
			},
		},
		{
			Name:        "uninstall_ownership_policy",
			Description: "Remove the ownership labels policy from the cluster",
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
		// Upgrade Tools
		{
			Name:        "detect_cluster_type",
			Description: "Detect the Kubernetes distribution type (OpenShift, EKS, GKE, AKS, kubeadm, k3s, kind, etc.)",
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
			Name:        "get_cluster_version_info",
			Description: "Get current Kubernetes/OpenShift version and check for available upgrades",
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
			Name:        "check_olm_operator_upgrades",
			Description: "Check OLM-managed operators for available upgrades (requires OLM installed)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "check_helm_release_upgrades",
			Description: "Check Helm releases for available chart version upgrades",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to check (all namespaces if not specified)",
					},
				},
			},
		},
		{
			Name:        "get_upgrade_prerequisites",
			Description: "Check upgrade prerequisites: node health, pod issues, ClusterOperators (OpenShift), MachineConfigPools",
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
			Name:        "trigger_openshift_upgrade",
			Description: "Trigger an OpenShift cluster upgrade to a specific version (REQUIRES CONFIRMATION: pass confirm='yes-upgrade-now')",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Cluster name (uses current context if not specified)",
					},
					"target_version": {
						Type:        "string",
						Description: "Target OpenShift version to upgrade to (e.g., 4.14.5)",
					},
					"confirm": {
						Type:        "string",
						Description: "Must be 'yes-upgrade-now' to proceed with the upgrade",
					},
				},
				Required: []string{"target_version", "confirm"},
			},
		},
		{
			Name:        "get_upgrade_status",
			Description: "Get the current upgrade status for a cluster (progress, ClusterOperators, MachineConfigPools)",
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
	case "get_roles":
		result, isError = s.toolGetRoles(ctx, params.Arguments)
	case "get_cluster_roles":
		result, isError = s.toolGetClusterRoles(ctx, params.Arguments)
	case "get_role_bindings":
		result, isError = s.toolGetRoleBindings(ctx, params.Arguments)
	case "get_cluster_role_bindings":
		result, isError = s.toolGetClusterRoleBindings(ctx, params.Arguments)
	case "can_i":
		result, isError = s.toolCanI(ctx, params.Arguments)
	case "analyze_subject_permissions":
		result, isError = s.toolAnalyzeSubjectPermissions(ctx, params.Arguments)
	case "describe_role":
		result, isError = s.toolDescribeRole(ctx, params.Arguments)
	case "find_pod_issues":
		result, isError = s.toolFindPodIssues(ctx, params.Arguments)
	case "find_deployment_issues":
		result, isError = s.toolFindDeploymentIssues(ctx, params.Arguments)
	case "check_resource_limits":
		result, isError = s.toolCheckResourceLimits(ctx, params.Arguments)
	case "check_security_issues":
		result, isError = s.toolCheckSecurityIssues(ctx, params.Arguments)
	case "analyze_namespace":
		result, isError = s.toolAnalyzeNamespace(ctx, params.Arguments)
	case "get_warning_events":
		result, isError = s.toolGetWarningEvents(ctx, params.Arguments)
	case "audit_kubeconfig":
		result, isError = s.toolAuditKubeconfig(ctx, params.Arguments)
	case "find_resource_owners":
		result, isError = s.toolFindResourceOwners(ctx, params.Arguments)
	case "check_gatekeeper":
		result, isError = s.toolCheckGatekeeper(ctx, params.Arguments)
	case "get_ownership_policy_status":
		result, isError = s.toolGetOwnershipPolicyStatus(ctx, params.Arguments)
	case "list_ownership_violations":
		result, isError = s.toolListOwnershipViolations(ctx, params.Arguments)
	case "install_ownership_policy":
		result, isError = s.toolInstallOwnershipPolicy(ctx, params.Arguments)
	case "set_ownership_policy_mode":
		result, isError = s.toolSetOwnershipPolicyMode(ctx, params.Arguments)
	case "uninstall_ownership_policy":
		result, isError = s.toolUninstallOwnershipPolicy(ctx, params.Arguments)
	// Upgrade Tools
	case "detect_cluster_type":
		result, isError = s.toolDetectClusterType(ctx, params.Arguments)
	case "get_cluster_version_info":
		result, isError = s.toolGetClusterVersionInfo(ctx, params.Arguments)
	case "check_olm_operator_upgrades":
		result, isError = s.toolCheckOLMOperatorUpgrades(ctx, params.Arguments)
	case "check_helm_release_upgrades":
		result, isError = s.toolCheckHelmReleaseUpgrades(ctx, params.Arguments)
	case "get_upgrade_prerequisites":
		result, isError = s.toolGetUpgradePrerequisites(ctx, params.Arguments)
	case "trigger_openshift_upgrade":
		result, isError = s.toolTriggerOpenShiftUpgrade(ctx, params.Arguments)
	case "get_upgrade_status":
		result, isError = s.toolGetUpgradeStatus(ctx, params.Arguments)
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
