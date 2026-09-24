package labels

import (
	"context"
	"encoding/json"
)

// ToolDef pairs a tool's MCP schema (name, description, inputSchema) with the
// handler that executes it, bound to the caller-supplied Deps rather than a
// concrete *mcp.Server. The root package adapts these into its own toolDef
// type (see labels_adapter.go) so tools/list output stays byte-identical to
// the pre-refactor server.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error)
}

// Tools returns the tool definitions for the labels domain, in the same
// order they were registered in the pre-refactor tools_labels.go.
func Tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "add_labels",
			Description: "Add labels to a Kubernetes resource across clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Resource kind (e.g., Deployment, Service, Pod, Node)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (default: default, ignored for cluster-scoped)",
					},
					"labels": map[string]interface{}{
						"type":        "object",
						"description": "Labels to add (key-value pairs)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"kind", "name", "labels"},
			},
			Handler: HandleAddLabels,
		},
		{
			Name:        "remove_labels",
			Description: "Remove labels from a Kubernetes resource across clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Resource kind (e.g., Deployment, Service, Pod, Node)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (default: default, ignored for cluster-scoped)",
					},
					"labels": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Label keys to remove",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"kind", "name", "labels"},
			},
			Handler: HandleRemoveLabels,
		},
	}
}
