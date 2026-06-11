package upgrades

import (
	"context"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// ToolDef pairs a tool schema with its handler bound to a ClusterAccess.
type ToolDef struct {
	Schema  protocol.Tool
	Handler func(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool)
}

// Tools returns all upgrade tool definitions. The caller is responsible for
// registering them with the MCP server's tool dispatch table.
func Tools() []ToolDef {
	return []ToolDef{
		{
			Schema: protocol.Tool{
				Name:        "detect_cluster_type",
				Description: "Detect the Kubernetes distribution type (OpenShift, EKS, GKE, AKS, kubeadm, k3s, kind, etc.)",
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
			Handler: DetectClusterType,
		},
		{
			Schema: protocol.Tool{
				Name:        "get_cluster_version_info",
				Description: "Get current Kubernetes/OpenShift version and check for available upgrades",
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
			Handler: GetClusterVersionInfo,
		},
		{
			Schema: protocol.Tool{
				Name:        "check_olm_operator_upgrades",
				Description: "Check OLM-managed operators for available upgrades (requires OLM installed)",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
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
			Handler: CheckOLMOperatorUpgrades,
		},
		{
			Schema: protocol.Tool{
				Name:        "check_helm_release_upgrades",
				Description: "Check Helm releases for available chart version upgrades",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
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
			Handler: CheckHelmReleaseUpgrades,
		},
		{
			Schema: protocol.Tool{
				Name:        "get_upgrade_prerequisites",
				Description: "Check upgrade prerequisites: node health, pod issues, ClusterOperators (OpenShift), MachineConfigPools",
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
			Handler: GetUpgradePrerequisites,
		},
		{
			Schema: protocol.Tool{
				Name:        "trigger_openshift_upgrade",
				Description: "Trigger an OpenShift cluster upgrade to a specific version (REQUIRES CONFIRMATION: pass confirm='yes-upgrade-now')",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
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
			Handler: TriggerOpenShiftUpgrade,
		},
		{
			Schema: protocol.Tool{
				Name:        "get_upgrade_status",
				Description: "Get the current upgrade status for a cluster (progress, ClusterOperators, MachineConfigPools)",
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
			Handler: GetUpgradeStatus,
		},
	}
}
