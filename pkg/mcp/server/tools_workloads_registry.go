package server

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/diagnostics"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/workloads"
)

// The workload domain lives in pkg/mcp/server/workloads and the
// cluster-diagnostics domain lives in pkg/mcp/server/diagnostics
// (kubestellar-mcp#1027). Registering them in this order from this file's
// init() keeps get_pods, get_deployments, get_services, get_nodes,
// get_events, describe_pod, get_pod_logs, find_pod_issues,
// find_deployment_issues, check_resource_limits, check_security_issues,
// analyze_namespace and get_warning_events at the same positions in
// tools/list as before the extraction. Registering diagnostics separately
// (rather than from inside workloads) also closes the hidden cross-domain
// edge #1027 called out: workloads no longer owns diagnostics tools.
//
// audit_kubeconfig and find_resource_owners are registered directly below
// because their handlers (tools_rbac_audit.go, tools_rbac_owners.go) are
// still top-level in pkg/mcp/server; extracting them is left for a
// follow-up #1027 slice.
func init() {
	workloads.Register(toolRegistry)
	diagnostics.Register(toolRegistry)
	RegisterTool(Tool{
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
		toolAuditKubeconfig,
	)
	RegisterTool(Tool{
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
		toolFindResourceOwners,
	)
}
