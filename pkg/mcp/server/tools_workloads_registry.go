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
// audit_kubeconfig and find_resource_owners were previously registered
// directly here because their handlers (tools_rbac_audit.go,
// tools_rbac_owners.go) were still top-level in pkg/mcp/server; they have
// since moved into pkg/mcp/server/rbac and are registered by
// tools_rbac_registry.go's init() instead, closing that cross-domain edge
// too.
func init() {
	workloads.Register(toolRegistry)
	diagnostics.Register(toolRegistry)
}
