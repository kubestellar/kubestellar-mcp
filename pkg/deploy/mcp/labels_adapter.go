package mcp

import (
	"context"
	"encoding/json"

	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/labels"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// serverLabelsDeps adapts *Server to the labels.Deps interface expected by
// pkg/deploy/mcp/labels.
type serverLabelsDeps struct {
	s *Server
}

func (d *serverLabelsDeps) DiscoverClusterNames() ([]string, error) {
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

func (d *serverLabelsDeps) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	return d.s.executor.ExecuteOnSelected(ctx, clusterNames, fn)
}

func (d *serverLabelsDeps) IsSensitiveKind(kind string) bool {
	return isSensitiveKind(kind)
}

func (d *serverLabelsDeps) SensitiveKindError(kind string) error {
	return sensitiveKindError(kind)
}

// Compile-time interface compliance check.
var _ labels.Deps = (*serverLabelsDeps)(nil)

// labelToolDefs adapts labels.Tools() into this package's toolDef, binding
// each handler to this *Server via serverLabelsDeps. Order is preserved
// from the pre-refactor tools_labels.go so tools/list output stays
// byte-identical (see registry.go:19-24).
func (s *Server) labelToolDefs() []toolDef {
	deps := &serverLabelsDeps{s: s}
	labelTools := labels.Tools()
	defs := make([]toolDef, 0, len(labelTools))
	for _, td := range labelTools {
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

// LabelResult is re-exported from the labels sub-package so existing
// in-package tests (tools_labels_*_test.go) continue to compile unchanged.
type LabelResult = labels.LabelResult

// buildLabelPatch delegates to the labels sub-package. Retained for test
// compatibility (tools_labels_test.go).
func buildLabelPatch(labelMap map[string]string, remove bool) []byte {
	return labels.BuildLabelPatch(labelMap, remove)
}

// handleAddLabels delegates to the labels sub-package. Retained as a Server
// method for test compatibility (tools_labels_test.go and friends).
func (s *Server) handleAddLabels(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return labels.HandleAddLabels(ctx, &serverLabelsDeps{s: s}, args)
}

// handleRemoveLabels delegates to the labels sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) handleRemoveLabels(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return labels.HandleRemoveLabels(ctx, &serverLabelsDeps{s: s}, args)
}

// addLabelsInCluster delegates to the labels sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) addLabelsInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labelMap map[string]string, dryRun bool) (LabelResult, error) {
	return labels.AddLabelsInCluster(ctx, &serverLabelsDeps{s: s}, client, clusterName, kind, name, namespace, labelMap, dryRun)
}

// removeLabelsInCluster delegates to the labels sub-package. Retained as a
// Server method for test compatibility.
func (s *Server) removeLabelsInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labelKeys []string, dryRun bool) (LabelResult, error) {
	return labels.RemoveLabelsInCluster(ctx, &serverLabelsDeps{s: s}, client, clusterName, kind, name, namespace, labelKeys, dryRun)
}
