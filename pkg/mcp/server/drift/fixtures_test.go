package drift

import (
	"context"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// newTestRegistry returns a fresh registry populated only by this package's
// Register, so tests never depend on pkg/mcp/server's global registry.
func newTestRegistry() *handlers.Registry {
	reg := handlers.NewRegistry()
	Register(reg)
	return reg
}

// callTool dispatches tool through a Register-populated registry against d
// and wraps the handler's (text, isError) return in the same
// protocol.CallToolResult shape pkg/mcp/server sends, so the tests moved
// here from pkg/mcp/server keep asserting on result.Content/result.IsError.
// An unregistered tool name is reported as a JSON-RPC -32602 error, matching
// pkg/mcp/server's handleToolsCall.
func callTool(t *testing.T, d *handlers.Deps, tool string, args map[string]interface{}) (protocol.CallToolResult, *protocol.Error) {
	t.Helper()

	handler := newTestRegistry().Find(tool)
	if handler == nil {
		return protocol.CallToolResult{}, &protocol.Error{Code: -32602, Message: "Unknown tool: " + tool}
	}

	text, isError := handler(context.Background(), d, args)
	return protocol.CallToolResult{
		Content: []protocol.ContentBlock{{Type: "text", Text: text}},
		IsError: isError,
	}, nil
}
