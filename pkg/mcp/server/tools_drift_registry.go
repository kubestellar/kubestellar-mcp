package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/drift"

// The drift domain lives in pkg/mcp/server/drift (kubestellar-mcp#1027);
// registering it from this file's init() keeps detect_drift at the same
// position in tools/list as before the extraction.
func init() {
	drift.Register(toolRegistry)
}
