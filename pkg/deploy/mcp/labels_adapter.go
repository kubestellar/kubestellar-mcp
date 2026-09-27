package mcp

import (
	"context"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/labels"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// serverLabelsDeps adapts *Server's multicluster manager/executor and the
// root package's sensitive-kind guards to the labels.Deps interface expected
// by the extracted sub-package.
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

// labelToolDefs returns the pkg/deploy/mcp/labels sub-package's tools
// (add_labels, remove_labels) with each handler bound to this *Server via
// serverLabelsDeps. Order is preserved so tools/list output stays
// byte-identical (see registry.go).
func (s *Server) labelToolDefs() []tooldef.ToolDef {
	return labels.Tools(&serverLabelsDeps{s: s})
}
