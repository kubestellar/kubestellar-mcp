package mcp

import (
	"io"

	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/deploy"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
)

// manifestReaderAdapter adapts *gitops.ManifestReader to deploy.ManifestReader.
type manifestReaderAdapter struct {
	reader *gitops.ManifestReader
}

func (a manifestReaderAdapter) ReadFromReader(r io.Reader) ([]gitops.Manifest, error) {
	return a.reader.ReadFromReader(r)
}

// deployDeps builds a deploy.Deps bound to this *Server, wiring the shared
// multicluster manager/executor/selector and manifest reader/syncer
// factories, plus the manifest-doc validator that stays in the root package
// (manifest_util.go).
func (s *Server) deployDeps() deploy.Deps {
	return deploy.Deps{
		Execute:                   s.executor.Execute,
		ExecuteOnSelected:         s.executor.ExecuteOnSelected,
		DiscoverClusters:          s.manager.DiscoverClusters,
		GetConfig:                 s.manager.GetConfig,
		GetCapabilitiesForCluster: s.selector.GetCapabilitiesForCluster,
		GetClusterCapabilities:    s.selector.GetClusterCapabilities,
		FindClustersForWorkload:   s.selector.FindClustersForWorkload,
		GetManifestReader: func() deploy.ManifestReader {
			return manifestReaderAdapter{reader: s.getManifestReader()}
		},
		GetManifestSyncer: func(config *rest.Config) (deploy.ManifestSyncer, error) {
			return s.getManifestSyncer(config)
		},
		ValidateManifestDocs: validateManifestDocs,
	}
}

// deployToolDefs adapts the pkg/deploy/mcp/deploy sub-package
// (list_cluster_capabilities, find_clusters_for_workload, deploy_app,
// scale_app, patch_app) into the root Server's toolDef shape. Order is
// preserved so tools/list output stays byte-identical (see registry.go).
func (s *Server) deployToolDefs() []toolDef {
	return adaptToolDefs(deploy.Tools(s.deployDeps()))
}
