package mcp

import (
	"context"
	"encoding/json"
)

// toolDef pairs a tool's MCP schema (name, description, inputSchema) with the
// handler that executes it. Co-locating the schema and handler in the same
// tools_*.go file (see appToolDefs, deployToolDefs, etc.) keeps them from
// drifting apart the way they could when the schema lived in one big literal
// in handleListTools and the dispatch lived separately in handleToolCall.
type toolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, args json.RawMessage) (interface{}, error)
}

// toolDefs returns every registered tool definition in the exact order
// clients should see them via tools/list. The order mirrors the domain
// grouping already used across tools_*.go (app, deploy, gitops, helm,
// kubectl, kustomize, labels) so tools/list output stays byte-identical to
// the pre-refactor server.go. Use this slice (not map iteration) whenever
// tool order matters.
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
