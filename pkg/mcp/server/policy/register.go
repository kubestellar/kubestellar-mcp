// Package policy provides the OPA Gatekeeper ownership-policy MCP tool
// handlers (check_gatekeeper, get_ownership_policy_status,
// list_ownership_violations, install_ownership_policy,
// set_ownership_policy_mode, uninstall_ownership_policy). It was extracted
// from the flat pkg/mcp/server package as part of kubestellar-mcp#1027
// (per-domain sub-package decomposition), mirroring the pattern established
// for pkg/deploy/mcp in #983. It depends only on the leaf
// pkg/mcp/server/handlers package, never on pkg/mcp/server itself.
package policy

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// Register adds the policy-domain tools to reg. pkg/mcp/server calls it from
// policy_tools.go's init(), so the tools' positions in tools/list are
// unchanged from when they were registered directly in
// tools_policy_registry.go.
func Register(reg *handlers.Registry) {
	reg.Register(protocol.Tool{
		Name:        "check_gatekeeper",
		Description: "Check if OPA Gatekeeper is installed and running in the cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
			},
		},
	},
		toolCheckGatekeeper,
	)
	reg.Register(protocol.Tool{
		Name:        "get_ownership_policy_status",
		Description: "Get the status of the ownership labels policy including violation count",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
			},
		},
	},
		toolGetOwnershipPolicyStatus,
	)
	reg.Register(protocol.Tool{
		Name:        "list_ownership_violations",
		Description: "List resources that violate the ownership labels policy (missing owner/team labels)",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
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
		toolListOwnershipViolations,
	)
	reg.Register(protocol.Tool{
		Name:        "install_ownership_policy",
		Description: "Install the ownership labels policy (ConstraintTemplate and Constraint) for OPA Gatekeeper",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
				"labels": {
					Type:        "array",
					Description: "Required labels (default: [\"owner\", \"team\"])",
					Items:       &protocol.Items{Type: "string"},
				},
				"target_namespaces": {
					Type:        "array",
					Description: "Namespaces to enforce (empty means all non-system namespaces)",
					Items:       &protocol.Items{Type: "string"},
				},
				"exclude_namespaces": {
					Type:        "array",
					Description: "Namespaces to exclude (default: kube-*, openshift-*, gatekeeper-system)",
					Items:       &protocol.Items{Type: "string"},
				},
				"mode": {
					Type:        "string",
					Description: "Enforcement mode: dryrun, warn, or enforce (default: dryrun)",
					Enum:        []string{"dryrun", "warn", "enforce"},
				},
			},
		},
	},
		toolInstallOwnershipPolicy,
	)
	reg.Register(protocol.Tool{
		Name:        "set_ownership_policy_mode",
		Description: "Change the enforcement mode of the ownership labels policy",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
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
		toolSetOwnershipPolicyMode,
	)
	reg.Register(protocol.Tool{
		Name:        "uninstall_ownership_policy",
		Description: "Remove the ownership labels policy from the cluster",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
				"cluster": {
					Type:        "string",
					Description: "Cluster name (uses current context if not specified)",
				},
			},
		},
	},
		toolUninstallOwnershipPolicy,
	)
}
