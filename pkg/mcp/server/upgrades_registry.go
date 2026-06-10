package server

import "context"

func init() {
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolDetectClusterType(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetClusterVersionInfo(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCheckOLMOperatorUpgrades(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolCheckHelmReleaseUpgrades(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetUpgradePrerequisites(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolTriggerOpenShiftUpgrade(ctx, args)
		},
	)
	RegisterTool(Tool{
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
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetUpgradeStatus(ctx, args)
		},
	)
}
