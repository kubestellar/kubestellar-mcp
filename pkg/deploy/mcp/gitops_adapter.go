package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/rest"
)

// serverGitOpsAccess adapts *Server's multicluster manager to the
// gitops.ClusterAccess interface expected by the extracted sub-package.
type serverGitOpsAccess struct {
	s *Server
}

func (a *serverGitOpsAccess) DiscoverClusters() ([]multicluster.ClusterInfo, error) {
	return a.s.manager.DiscoverClusters()
}

func (a *serverGitOpsAccess) GetConfig(clusterName string) (*rest.Config, error) {
	return a.s.manager.GetConfig(clusterName)
}

// Compile-time interface compliance check.
var _ gitops.ClusterAccess = (*serverGitOpsAccess)(nil)

// gitopsServer builds a gitops.Server wired to s's manager and the shared
// manifest reader/syncer/drift-detector factories.
func (s *Server) gitopsServer() *gitops.Server {
	return &gitops.Server{
		Access:            &serverGitOpsAccess{s: s},
		NewManifestReader: s.getManifestReader,
		NewManifestSyncer: func(config *rest.Config) (gitops.ManifestSyncer, error) {
			return s.getManifestSyncer(config)
		},
		NewDriftDetector: func(config *rest.Config) (gitops.DriftDetector, error) {
			return s.getDriftDetector(config)
		},
	}
}

// gitopsToolDefs adapts the pkg/deploy/mcp/gitops sub-package (detect_drift,
// sync_from_git, reconcile, preview_changes) into the root Server's toolDef
// shape. Order is preserved so tools/list output stays byte-identical (see
// registry.go).
func (s *Server) gitopsToolDefs() []toolDef {
	return adaptToolDefs(s.gitopsServer().Tools())
}
