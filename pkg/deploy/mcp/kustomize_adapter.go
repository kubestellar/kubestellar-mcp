package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
)

// kustomizeToolDefs adapts the pkg/deploy/mcp/kustomize sub-package
// (kustomize_build, kustomize_apply, kustomize_delete) as handler-bound
// tooldef.ToolDefs. Cluster-name validation is shared with the helm domain
// (#289) and manifest validation stays in the root package
// (manifest_util.go). Order is preserved so tools/list output stays
// byte-identical (see registry.go).
func (s *Server) kustomizeToolDefs() []tooldef.ToolDef {
	deps := kustomize.Deps{
		DiscoverClusters: s.manager.DiscoverClusters,
		ValidateClusters: helm.ValidateClusters,
		ValidateManifest: validateManifestDocs,
	}
	return deps.Tools()
}
