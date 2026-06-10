package server

import "context"

func init() {
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCheckGatekeeper(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetOwnershipPolicyStatus(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolListOwnershipViolations(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolInstallOwnershipPolicy(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolSetOwnershipPolicyMode(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolUninstallOwnershipPolicy(ctx, args)
		},
	)
}
