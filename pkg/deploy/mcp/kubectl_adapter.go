package mcp

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kubectl"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// serverKubectlDeps adapts *Server to the kubectl.Deps interface expected by
// pkg/deploy/mcp/kubectl.
type serverKubectlDeps struct {
	s *Server
}

func (d *serverKubectlDeps) DiscoverClusterNames() ([]string, error) {
	clusters, err := d.s.manager.DiscoverClusters()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(clusters))
	for _, c := range clusters {
		names = append(names, c.Name)
	}
	return names, nil
}

func (d *serverKubectlDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	return d.s.executor.ExecuteOnSelected(ctx, clusterNames, fn)
}

func (d *serverKubectlDeps) GetConfig(clusterName string) (*rest.Config, error) {
	return d.s.manager.GetConfig(clusterName)
}

func (d *serverKubectlDeps) IsSensitiveKind(kind string) bool {
	return isSensitiveKind(kind)
}

func (d *serverKubectlDeps) SensitiveKindError(kind string) error {
	return sensitiveKindError(kind)
}

func (d *serverKubectlDeps) IsNamespaceKind(kind string) bool {
	return isNamespaceKind(kind)
}

func (d *serverKubectlDeps) ManifestSensitiveKind(doc string) (string, bool) {
	return manifestSensitiveKind(doc)
}

func (d *serverKubectlDeps) YAMLToJSON(yamlStr string) string {
	return yamlToJSON(yamlStr)
}

func (d *serverKubectlDeps) UnstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error {
	return unstructuredFromYAML(yamlStr, obj)
}

// Compile-time interface compliance check.
var _ kubectl.Deps = (*serverKubectlDeps)(nil)

// kubectlToolDefs adapts kubectl.Tools() into this package's toolDef, binding
// each handler to this *Server via serverKubectlDeps. Order is preserved
// from the pre-refactor tools_kubectl.go so tools/list output stays
// byte-identical (see registry.go:19-24).
func (s *Server) kubectlToolDefs() []toolDef {
	deps := &serverKubectlDeps{s: s}
	kubectlTools := kubectl.Tools()
	defs := make([]toolDef, 0, len(kubectlTools))
	for _, td := range kubectlTools {
		td := td // capture loop variable
		defs = append(defs, toolDef{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return td.Handler(ctx, deps, args)
			},
		})
	}
	return defs
}

// DeleteResult is re-exported from the kubectl sub-package so existing
// in-package tests (tools_kubectl_*_test.go) continue to compile unchanged.
type DeleteResult = kubectl.DeleteResult

// ApplyResult is re-exported from the kubectl sub-package so existing
// in-package tests (tools_kubectl_*_test.go) continue to compile unchanged.
type ApplyResult = kubectl.ApplyResult

// handleDeleteResource delegates to the kubectl sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) handleDeleteResource(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return kubectl.HandleDeleteResource(ctx, &serverKubectlDeps{s: s}, args)
}

// handleKubectlApply delegates to the kubectl sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) handleKubectlApply(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return kubectl.HandleKubectlApply(ctx, &serverKubectlDeps{s: s}, args)
}

// deleteResourceInCluster delegates to the kubectl sub-package. Retained as
// a Server method for test compatibility.
func (s *Server) deleteResourceInCluster(ctx context.Context, client kubernetes.Interface, clusterName, kind, name, namespace string, dryRun bool) (DeleteResult, error) {
	return kubectl.DeleteResourceInCluster(ctx, client, clusterName, kind, name, namespace, dryRun)
}

// applyManifestDynamic delegates to the kubectl sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) applyManifestDynamic(ctx context.Context, clusterName, manifest string, dryRun bool) ([]ApplyResult, error) {
	return kubectl.ApplyManifestDynamic(ctx, &serverKubectlDeps{s: s}, clusterName, manifest, dryRun)
}

// getGVR delegates to the kubectl sub-package. Retained as a free function
// for test compatibility (tools_kubectl_test.go).
func getGVR(kind string) (schema.GroupVersionResource, bool) {
	return kubectl.GetGVR(kind)
}
