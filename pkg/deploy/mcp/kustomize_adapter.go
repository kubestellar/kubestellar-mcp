package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
)

// kustomizeToolDefs adapts the pkg/deploy/mcp/kustomize sub-package
// (kustomize_build, kustomize_apply, kustomize_delete) into the root Server's
// toolDef shape. Cluster-name validation is shared with the helm domain
// (#289) and manifest validation stays in the root package
// (manifest_util.go). Order is preserved so tools/list output stays
// byte-identical (see registry.go).
func (s *Server) kustomizeToolDefs() []toolDef {
	deps := kustomize.Deps{
		DiscoverClusters: s.manager.DiscoverClusters,
		ValidateClusters: helm.ValidateClusters,
		ValidateManifest: validateManifestDocs,
	}
	return adaptToolDefs(deps.Tools())
}
