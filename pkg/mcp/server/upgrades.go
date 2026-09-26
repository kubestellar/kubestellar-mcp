package server

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// *handlers.Deps satisfies upgrades.ClusterAccess directly, so no adapter
// is needed between the server and the upgrades sub-package.
var _ upgrades.ClusterAccess = (*handlers.Deps)(nil)

func init() {
	for _, td := range upgrades.Tools() {
		td := td // capture loop variable
		RegisterTool(td.Schema,
			func(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
				return td.Handler(ctx, d, args)
			},
		)
	}
}

// Re-export ClusterType constants so existing tests and consumers continue to work.
const (
	ClusterTypeOpenShift = upgrades.ClusterTypeOpenShift
	ClusterTypeEKS       = upgrades.ClusterTypeEKS
	ClusterTypeGKE       = upgrades.ClusterTypeGKE
	ClusterTypeAKS       = upgrades.ClusterTypeAKS
	ClusterTypeKubeadm   = upgrades.ClusterTypeKubeadm
	ClusterTypeK3s       = upgrades.ClusterTypeK3s
	ClusterTypeKind      = upgrades.ClusterTypeKind
	ClusterTypeMinikube  = upgrades.ClusterTypeMinikube
	ClusterTypeUnknown   = upgrades.ClusterTypeUnknown
)

// HelmRelease is re-exported from the upgrades sub-package.
type HelmRelease = upgrades.HelmRelease

// parseHelmSecret delegates to the upgrades package. Retained for test compatibility.
func parseHelmSecret(secret *corev1.Secret) *HelmRelease {
	return upgrades.ParseHelmSecret(secret)
}
