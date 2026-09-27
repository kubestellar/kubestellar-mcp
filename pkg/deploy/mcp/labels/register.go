package labels

import (
	"context"
	"encoding/json"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"
)

// Tools returns the tool definitions for the labels domain, bound to the
// given Deps at construction time (matching app.Tools(executor) and
// deploy.Tools(d)), in the same order they were registered in the
// pre-refactor tools_labels.go.
func Tools(d Deps) []tooldef.ToolDef {
	return []tooldef.ToolDef{
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
			Handler: bind(d, HandleAddLabels),
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
			Handler: bind(d, HandleRemoveLabels),
		},
	}
}

// bind closes a Deps-taking handler over d so it satisfies tooldef.Handler.
func bind(d Deps, h func(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error)) tooldef.Handler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		return h(ctx, d, args)
	}
}
