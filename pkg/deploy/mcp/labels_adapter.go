package mcp

import (
	"context"
	"encoding/json"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/labels"
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

// labelToolDefs adapts the pkg/deploy/mcp/labels sub-package (add_labels,
// remove_labels) into the root Server's toolDef shape, binding each handler
// to this Server's deps. Order is preserved so tools/list output stays
// byte-identical (see registry.go).
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
