package mcp

import (
	"context"
	"encoding/json"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/deploy"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/helm"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kubectl"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/kustomize"
)

// toolDef pairs a tool's MCP schema (name, description, inputSchema) with the
// handler that executes it. Each domain sub-package (app, deploy, gitops,
// helm, kubectl, kustomize, labels) co-locates its schemas and handlers and
// exposes them via Tools(); the *_adapter.go files in this package convert
// them into this shape.
type toolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, args json.RawMessage) (interface{}, error)
}

// domainToolDef is the set of per-domain tool definition types whose shape is
// identical to toolDef (name, description, inputSchema, self-contained
// handler). labels.ToolDef is excluded because its Handler takes an explicit
// labels.Deps argument; see labelToolDefs.
type domainToolDef interface {
	app.ToolDef | deploy.ToolDef | gitops.ToolDef | helm.ToolDef | kubectl.ToolDef | kustomize.ToolDef
}

// adaptToolDefs converts a sub-package's tool definitions into the root
// toolDef shape, preserving order.
func adaptToolDefs[T domainToolDef](subDefs []T) []toolDef {
	defs := make([]toolDef, 0, len(subDefs))
	for _, d := range subDefs {
		defs = append(defs, toolDef(d))
	}
	return defs
}

// toolDefs returns every registered tool definition in the exact order
// clients should see them via tools/list. The order follows the domain
// grouping (app, deploy, gitops, helm, kubectl, kustomize, labels) so
// tools/list output stays byte-identical to the pre-refactor flat package.
// Use this slice (not map iteration) whenever tool order matters.
func (s *Server) toolDefs() []toolDef {
	var defs []toolDef
	defs = append(defs, s.appToolDefs()...)
	defs = append(defs, s.deployToolDefs()...)
	defs = append(defs, s.gitopsToolDefs()...)
	defs = append(defs, s.helmToolDefs()...)
	defs = append(defs, s.kubectlToolDefs()...)
	defs = append(defs, s.kustomizeToolDefs()...)
	defs = append(defs, s.labelToolDefs()...)
	return defs
}

// findToolDef looks up a tool definition by name for dispatch in
// handleToolCall.
func (s *Server) findToolDef(name string) (toolDef, bool) {
	for _, d := range s.toolDefs() {
		if d.Name == name {
			return d, true
		}
	}
	return toolDef{}, false
}
