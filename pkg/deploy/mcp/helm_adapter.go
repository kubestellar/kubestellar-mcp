package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// serverHelmAccess adapts *Server's multicluster manager to the
// helm.ClusterAccess interface expected by the extracted sub-package.
type serverHelmAccess struct {
	s *Server
}

func (a *serverHelmAccess) DiscoverClusters() ([]multicluster.ClusterInfo, error) {
	return a.s.manager.DiscoverClusters()
}

// Compile-time interface compliance check.
var _ helm.ClusterAccess = (*serverHelmAccess)(nil)

// helmServer builds a helm.Server wired to s's manager.
func (s *Server) helmServer() *helm.Server {
	return &helm.Server{Access: &serverHelmAccess{s: s}}
}

// helmToolDefs adapts the pkg/deploy/mcp/helm sub-package (helm_install,
// helm_uninstall, helm_list, helm_rollback) into the root Server's toolDef
// shape. Order is preserved so tools/list output stays byte-identical (see
// registry.go).
func (s *Server) helmToolDefs() []toolDef {
	return adaptToolDefs(s.helmServer().Tools())
}
