package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kubectl"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
)

// kubectlDeps builds a kubectl.Deps bound to this *Server, wiring the shared
// multicluster manager/executor and the manifest utilities that stay in the
// root package (manifest_util.go).
func (s *Server) kubectlDeps() kubectl.Deps {
	return kubectl.Deps{
		DiscoverClusters:      s.manager.DiscoverClusters,
		ExecuteOnSelected:     s.executor.ExecuteOnSelected,
		GetConfig:             s.manager.GetConfig,
		IsSensitiveKind:       isSensitiveKind,
		SensitiveKindError:    sensitiveKindError,
		ManifestSensitiveKind: manifestSensitiveKind,
		IsNamespaceKind:       isNamespaceKind,
		YAMLToJSON:            yamlToJSON,
		UnstructuredFromYAML:  unstructuredFromYAML,
	}
}

// kubectlToolDefs adapts the pkg/deploy/mcp/kubectl sub-package
// (delete_resource, kubectl_apply) as handler-bound tooldef.ToolDefs.
// Order is preserved so tools/list output stays byte-identical (see
// registry.go).
func (s *Server) kubectlToolDefs() []tooldef.ToolDef {
	return s.kubectlDeps().Tools()
}
