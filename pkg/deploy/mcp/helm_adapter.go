package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// This file adapts the pkg/deploy/mcp/helm sub-package (the "helm" domain:
// helm_install, helm_uninstall, helm_list, helm_rollback) into the root
// Server, per epic #983 (decompose pkg/deploy/mcp into per-domain
// sub-packages). It replaces the former tools_helm.go, tools_helm_install.go,
// tools_helm_list.go, tools_helm_rollback.go, tools_helm_uninstall.go, and
// tools_helm_validate.go, whose logic was moved verbatim into
// pkg/deploy/mcp/helm. Behavior, including tools/list registration order
// (registry.go:19-24), is unchanged. This is the last of the seven domains
// tracked by epic #983; only root-scaffolding cleanup remains (#997).

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

// helmToolDefs returns the tool definitions handled by the helm sub-package.
// Order matches the pre-refactor helmToolDefs exactly.
func (s *Server) helmToolDefs() []tooldef.ToolDef {
	return s.helmServer().Tools()
}
