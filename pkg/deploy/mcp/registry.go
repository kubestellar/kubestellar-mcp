package mcp

import "github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"

// toolDefs returns every registered tool definition in the exact order
// clients should see them via tools/list. The order mirrors the domain
// grouping of the sub-packages (app, deploy, gitops, helm, kubectl,
// kustomize, labels) so tools/list output stays byte-identical to the
// pre-refactor server.go. Use this slice (not map iteration) whenever tool
// order matters.
//
// Every sub-package returns []tooldef.ToolDef directly, so no per-domain
// conversion happens here; the *ToolDefs methods below are pure wiring that
// bind each domain's dependencies to this *Server.
func (s *Server) toolDefs() []tooldef.ToolDef {
	var defs []tooldef.ToolDef
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
func (s *Server) findToolDef(name string) (tooldef.ToolDef, bool) {
	for _, d := range s.toolDefs() {
		if d.Name == name {
			return d, true
		}
	}
	return tooldef.ToolDef{}, false
}
