package handlers

import (
	"context"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// ToolHandler executes a tool against the injected Deps and returns
// (result, isError).
type ToolHandler func(ctx context.Context, deps *Deps, args map[string]interface{}) (string, bool)

// ToolDef co-locates a tool's schema with its handler implementation.
type ToolDef struct {
	Schema  protocol.Tool
	Handler ToolHandler
}

// Registry is an ordered collection of tool definitions. Domain packages
// register into it; the protocol server lists and dispatches from it.
type Registry struct {
	defs []ToolDef
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register appends a tool definition in registration order.
func (r *Registry) Register(schema protocol.Tool, handler ToolHandler) {
	r.defs = append(r.defs, ToolDef{Schema: schema, Handler: handler})
}

// Defs returns a copy of all registered tool definitions.
func (r *Registry) Defs() []ToolDef {
	out := make([]ToolDef, len(r.defs))
	copy(out, r.defs)
	return out
}

// Tools returns all registered tool schemas.
func (r *Registry) Tools() []protocol.Tool {
	tools := make([]protocol.Tool, len(r.defs))
	for i, td := range r.defs {
		tools[i] = td.Schema
	}
	return tools
}

// Find looks up a handler by tool name. It returns nil if not found.
func (r *Registry) Find(name string) ToolHandler {
	for _, td := range r.defs {
		if td.Schema.Name == name {
			return td.Handler
		}
	}
	return nil
}
