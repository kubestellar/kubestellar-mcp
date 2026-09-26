// Package tooldef holds the single ToolDef type shared by the root
// pkg/deploy/mcp package and every domain sub-package beneath it (app,
// deploy, gitops, helm, kubectl, kustomize, labels).
//
// It is a leaf package with no dependencies beyond the standard library so
// that both the root package and its sub-packages can import it without
// forming an import cycle. Before this package existed each sub-package
// re-declared a structurally identical ToolDef and the root adapters copied
// them field-by-field into a private twin; that duplication let the shapes
// drift silently (see kubestellar-mcp#1001).
package tooldef

import (
	"context"
	"encoding/json"
)

// Handler executes a tool call. Every dependency a handler needs must be
// bound at construction time (see the sub-packages' Tools functions); the
// signature deliberately carries only the request context and raw JSON
// arguments so the root dispatcher stays domain-agnostic.
type Handler func(ctx context.Context, args json.RawMessage) (interface{}, error)

// ToolDef pairs a tool's MCP schema (name, description, inputSchema) with
// the handler that executes it. Co-locating the schema and handler keeps
// them from drifting apart the way they could when the schema lived in one
// big tools/list literal and the dispatch lived in a separate switch.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     Handler
}
