package rpcloop

import (
	"context"
	"fmt"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// Methods holds a server's Handlers for the MCP methods whose replies are
// server-specific. Dispatch owns every other arm of the JSON-RPC method
// table, so the two MCP servers can no longer drift on lifecycle
// notifications, ping, or unknown-method handling (see kubestellar-mcp#1017;
// pkg/deploy/mcp previously answered "ping" with -32601 and omitted the
// method name from its "Method not found" error). All three fields must be
// non-nil.
type Methods struct {
	Initialize Handler
	ToolsList  Handler
	ToolsCall  Handler
}

// Dispatch routes req to the matching Methods handler and returns the
// response Loop.Run should write, or nil when no reply is expected. It
// answers the "initialized"/"notifications/initialized" lifecycle
// notifications with nil, "ping" with an empty result, and any other
// unrecognized method with a JSON-RPC -32601 "Method not found: <method>"
// error.
func Dispatch(ctx context.Context, req *protocol.Request, m Methods) *protocol.Response {
	switch req.Method {
	case "initialize":
		return m.Initialize(ctx, req)
	case "initialized", "notifications/initialized":
		return nil
	case "tools/list":
		return m.ToolsList(ctx, req)
	case "tools/call":
		return m.ToolsCall(ctx, req)
	case "ping":
		return protocol.NewResult(req.ID, map[string]interface{}{})
	default:
		return protocol.NewError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}
