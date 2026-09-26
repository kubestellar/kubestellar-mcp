package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
)

// appToolDefs adapts the pkg/deploy/mcp/app sub-package (get_app_instances,
// get_app_status, get_app_logs) as handler-bound tooldef.ToolDefs. The
// position of these tools within Server.toolDefs() is preserved so
// tools/list output stays byte-identical (see registry.go).
func (s *Server) appToolDefs() []tooldef.ToolDef {
	return app.Tools(s.executor)
}
