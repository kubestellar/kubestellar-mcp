package mcp

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kubectl"
)

// This file adapts the pkg/deploy/mcp/kubectl sub-package (the "kubectl"
// domain: delete_resource, kubectl_apply) into the root Server, per epic
// #983 (decompose pkg/deploy/mcp into per-domain sub-packages). It replaces
// the former tools_kubectl.go, kubectl_cluster_ops.go, and kubectl_gvr.go,
// whose logic was moved verbatim into pkg/deploy/mcp/kubectl. Behavior,
// including tools/list registration order (registry.go:19-24), is
// unchanged.
//
// Re-exports below exist solely so in-package tests (tools_kubectl*_test.go)
// keep compiling against *Server without modification; they are thin
// delegations to the kubectl sub-package, mirroring the pattern used in
// app_adapter.go, kustomize_adapter.go, and labels_adapter.go.

// DeleteResult is re-exported from the kubectl sub-package.
type DeleteResult = kubectl.DeleteResult

// ApplyResult is re-exported from the kubectl sub-package.
type ApplyResult = kubectl.ApplyResult

// kubectlDeps builds a kubectl.Deps bound to this *Server, wiring the
// shared manifest_util helpers and the multicluster manager/executor.
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

// kubectlToolDefs returns the tool definitions handled by the kubectl
// sub-package. Order is preserved from the pre-refactor tools_kubectl.go so
// tools/list output stays byte-identical (see registry.go:19-24).
func (s *Server) kubectlToolDefs() []toolDef {
	subDefs := s.kubectlDeps().Tools()
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

// handleDeleteResource delegates to kubectl.HandleDeleteResource. Retained
// on *Server for test compatibility (tools_kubectl*_test.go).
func (s *Server) handleDeleteResource(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return kubectl.HandleDeleteResource(ctx, s.kubectlDeps(), args)
}

// handleKubectlApply delegates to kubectl.HandleKubectlApply. Retained on
// *Server for test compatibility.
func (s *Server) handleKubectlApply(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return kubectl.HandleKubectlApply(ctx, s.kubectlDeps(), args)
}

// deleteResourceInCluster delegates to kubectl.DeleteResourceInCluster.
// Retained on *Server for test compatibility.
func (s *Server) deleteResourceInCluster(ctx context.Context, client kubernetes.Interface, clusterName, kind, name, namespace string, dryRun bool) (DeleteResult, error) {
	return kubectl.DeleteResourceInCluster(ctx, client, clusterName, kind, name, namespace, dryRun)
}

// applyManifestDynamic delegates to kubectl.ApplyManifestDynamic. Retained
// on *Server for test compatibility.
func (s *Server) applyManifestDynamic(ctx context.Context, clusterName, manifest string, dryRun bool) ([]ApplyResult, error) {
	return kubectl.ApplyManifestDynamic(ctx, s.kubectlDeps(), clusterName, manifest, dryRun)
}

// getGVR delegates to kubectl.GetGVR. Retained as a package-level function
// for test compatibility (tools_kubectl_test.go).
func getGVR(kind string) (schema.GroupVersionResource, bool) {
	return kubectl.GetGVR(kind)
}

