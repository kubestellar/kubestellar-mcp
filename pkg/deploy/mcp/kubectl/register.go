package kubectl

import (
	"context"
	"encoding/json"
)

// ToolDef pairs a tool's MCP schema (name, description, inputSchema) with the
// handler that executes it, bound to the caller-supplied Deps rather than a
// concrete *mcp.Server. The root package adapts these into its own toolDef
// type (see kubectl_adapter.go) so tools/list output stays byte-identical to
// the pre-refactor server.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error)
}

// Tools returns the tool definitions for the kubectl domain, in the same
// order they were registered in the pre-refactor tools_kubectl.go.
func Tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "delete_resource",
			Description: "Delete a Kubernetes resource from clusters. Supports all common resource types.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Resource kind (e.g., Deployment, Service, Pod, ConfigMap, Secret, StatefulSet, DaemonSet, Job, CronJob, Ingress, PVC, Namespace, ServiceAccount, Role, RoleBinding, ClusterRole, ClusterRoleBinding)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (default: default, ignored for cluster-scoped resources)",
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
				"required": []string{"kind", "name"},
			},
			Handler: HandleDeleteResource,
		},
		{
			Name:        "kubectl_apply",
			Description: "Apply any Kubernetes manifest to clusters. Supports all resource types using dynamic client.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"manifest": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes manifest (YAML or JSON)",
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
				"required": []string{"manifest"},
			},
			Handler: HandleKubectlApply,
		},
	}
}
