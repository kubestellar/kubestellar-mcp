package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/policy"

// The ownership-policy domain lives in pkg/mcp/server/policy
// (kubestellar-mcp#1027); registering it from this file's init() keeps
// check_gatekeeper, get_ownership_policy_status, list_ownership_violations,
// install_ownership_policy, set_ownership_policy_mode and
// uninstall_ownership_policy at the same positions in tools/list as before
// the extraction.
func init() {
	policy.Register(toolRegistry)
}
