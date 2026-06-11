package server

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/tools/upgrades"
)

// serverClusterAccess adapts *Server to the upgrades.ClusterAccess interface.
type serverClusterAccess struct {
	s *Server
}

func (a *serverClusterAccess) GetClientForCluster(name string) (kubernetes.Interface, error) {
	return a.s.getClientForCluster(name)
}

func (a *serverClusterAccess) GetDynamicClientForCluster(name string) (dynamic.Interface, error) {
	return a.s.getDynamicClientForCluster(name)
}

// Compile-time interface compliance check.
var _ upgrades.ClusterAccess = (*serverClusterAccess)(nil)

func init() {
	for _, td := range upgrades.Tools() {
		td := td // capture loop variable
		RegisterTool(td.Schema,
			func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
				return td.Handler(ctx, &serverClusterAccess{s: s}, args)
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
func (s *Server) parseHelmSecret(secret *corev1.Secret) *HelmRelease {
	return upgrades.ParseHelmSecret(secret)
}
