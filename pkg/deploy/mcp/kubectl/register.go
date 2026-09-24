package kubectl

import (
	"context"
	"encoding/json"
)

// ToolDef pairs a tool's MCP schema with its dispatch handler. It mirrors
// the shape of the root package's private toolDef type so the root adapter
// can convert 1:1 without changing tools/list output.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, args json.RawMessage) (interface{}, error)
}

// Tools returns the kubectl-domain tool definitions, bound to the given
// Deps. The order matches the pre-refactor kubectlToolDefs() in
// pkg/deploy/mcp/tools_kubectl.go exactly, since tools/list order must stay
// byte-identical (see registry.go).
func (d Deps) Tools() []ToolDef {
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
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleDeleteResource(ctx, d, args)
			},
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
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return HandleKubectlApply(ctx, d, args)
			},
		},
	}
}
