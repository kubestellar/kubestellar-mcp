package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/rbac"

// The RBAC domain lives in pkg/mcp/server/rbac (kubestellar-mcp#1027);
// registering it from this file's init() keeps get_roles, get_cluster_roles,
// get_role_bindings, get_cluster_role_bindings, can_i,
// analyze_subject_permissions, describe_role, audit_kubeconfig and
// find_resource_owners registered as a single domain. The latter two moved
// here from tools_workloads_registry.go, closing the cross-domain edge
// #1027 called out (their handlers always lived in tools_rbac_audit.go /
// tools_rbac_owners.go, now pkg/mcp/server/rbac/{audit,owners}.go).
func init() {
	rbac.Register(toolRegistry)
}
