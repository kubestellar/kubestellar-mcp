package mcp

import (
	"context"
	"encoding/json"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
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

// gitopsServer builds a gitops.Server wired to s's manager and factories.
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

// gitopsToolDefs returns the tool definitions handled by the gitops
// sub-package. Order matches the pre-refactor gitopsToolDefs exactly.
func (s *Server) gitopsToolDefs() []tooldef.ToolDef {
	return s.gitopsServer().Tools()
}

// handleSyncFromGit is a thin re-export retained because
// tools_namespace_validation_rest_test.go (not part of the gitops slice)
// still calls it directly on *Server.
func (s *Server) handleSyncFromGit(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return s.gitopsServer().HandleSyncFromGit(ctx, args)
}
