package server

import "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"

// ToolHandler and ToolDef are the handler types shared with domain packages.
// They are aliases so registry files in this package keep compiling unchanged
// while domain code migrates to importing handlers directly.
type (
	ToolHandler = handlers.ToolHandler
	ToolDef     = handlers.ToolDef
)

// toolRegistry holds all registered tool definitions. Domain files register
// into it via init() or explicit registration functions.
var toolRegistry = handlers.NewRegistry()

// RegisterTool adds a tool definition to the package registry. Called from
// domain-specific files (tools_cluster_registry.go, tools_workloads_registry.go,
// etc.) during package initialization.
func RegisterTool(schema Tool, handler ToolHandler) {
	toolRegistry.Register(schema, handler)
}

// registeredTools returns all registered tool schemas.
func registeredTools() []Tool {
	return toolRegistry.Tools()
}

// findToolHandler looks up a handler by tool name. Returns nil if not found.
func findToolHandler(name string) ToolHandler {
	return toolRegistry.Find(name)
}
