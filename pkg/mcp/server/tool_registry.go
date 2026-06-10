package server

import "context"

// ToolHandler is a function that executes a tool and returns (result, isError).
type ToolHandler func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool)

// ToolDef co-locates a tool's schema with its handler implementation.
type ToolDef struct {
	Schema  Tool
	Handler ToolHandler
}

// toolRegistry holds all registered tool definitions. Domain files append to
// this slice via init() or explicit registration functions.
var toolRegistry []ToolDef

// RegisterTool adds a tool definition to the global registry. Called from
// domain-specific files (tools_cluster.go, tools_workloads.go, etc.) during
// package initialization.
func RegisterTool(schema Tool, handler ToolHandler) {
	toolRegistry = append(toolRegistry, ToolDef{Schema: schema, Handler: handler})
}

// registeredTools returns all registered tool schemas.
func registeredTools() []Tool {
	tools := make([]Tool, len(toolRegistry))
	for i, td := range toolRegistry {
		tools[i] = td.Schema
	}
	return tools
}

// findToolHandler looks up a handler by tool name. Returns nil if not found.
func findToolHandler(name string) ToolHandler {
	for _, td := range toolRegistry {
		if td.Schema.Name == name {
			return td.Handler
		}
	}
	return nil
}
