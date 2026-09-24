package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
)

// kustomizeToolDefs returns the tool definitions handled by the kustomize
// sub-package (pkg/deploy/mcp/kustomize), adapted to the root package's
// toolDef shape. This is a thin adapter: the schemas and handler behavior are
// unchanged from before the pkg/deploy/mcp/kustomize extraction (epic #983
// phase 1) and the position of these tools within Server.toolDefs() is
// preserved, so tools/list output stays byte-identical.
func (s *Server) kustomizeToolDefs() []toolDef {
	deps := kustomize.Deps{
		DiscoverClusters: s.manager.DiscoverClusters,
		ValidateClusters: helm.ValidateClusters,
		ValidateManifest: validateManifestDocs,
	}

	subDefs := deps.Tools()
	defs := make([]toolDef, 0, len(subDefs))
	for _, d := range subDefs {
		defs = append(defs, toolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Handler:     d.Handler,
		})
	}
	return defs
}
