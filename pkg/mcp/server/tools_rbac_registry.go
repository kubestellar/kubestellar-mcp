package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/rbac"

// The RBAC domain lives in pkg/mcp/server/rbac (kubestellar-mcp#1027);
// registering it from this file's init() keeps get_roles, get_cluster_roles,
// get_role_bindings, get_cluster_role_bindings, can_i,
// analyze_subject_permissions and describe_role at the same positions in
// tools/list as before the extraction.
func init() {
	rbac.Register(toolRegistry)
}
