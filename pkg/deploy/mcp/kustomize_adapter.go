package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
)

// kustomizeToolDefs returns the tool definitions handled by the kustomize
// sub-package (pkg/deploy/mcp/kustomize), bound to this server. The position
// of these tools within Server.toolDefs() is preserved, so tools/list output
// stays byte-identical.
func (s *Server) kustomizeToolDefs() []tooldef.ToolDef {
	deps := kustomize.Deps{
		DiscoverClusters: s.manager.DiscoverClusters,
		ValidateClusters: helm.ValidateClusters,
		ValidateManifest: validateManifestDocs,
	}
	return deps.Tools()
}
