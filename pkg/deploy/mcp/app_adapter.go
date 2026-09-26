package mcp

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
)

// appToolDefs adapts the pkg/deploy/mcp/app sub-package (get_app_instances,
// get_app_status, get_app_logs) into the root Server's toolDef shape. The
// position of these tools within Server.toolDefs() is preserved so
// tools/list output stays byte-identical (see registry.go).
func (s *Server) appToolDefs() []toolDef {
	return adaptToolDefs(app.Tools(s.executor))
}
