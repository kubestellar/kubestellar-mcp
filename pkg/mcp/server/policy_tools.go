package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/policy"

// The policy domain (check_gatekeeper, get_ownership_policy_status,
// list_ownership_violations, install_ownership_policy,
// set_ownership_policy_mode, uninstall_ownership_policy) lives in
// pkg/mcp/server/policy (kubestellar-mcp#1027); registering it from this
// file's init() keeps those tools at the same positions in tools/list as
// before the extraction.
func init() {
	policy.Register(toolRegistry)
}
