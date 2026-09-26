package helm

import "github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"

// Tools returns the tool definitions for the helm domain, bound to s, in the
// same order they were registered in the pre-refactor tools_helm.go so
// tools/list output stays byte-identical (see registry.go:19-24).
func (s *Server) Tools() []tooldef.ToolDef {
	return []tooldef.ToolDef{
		{
			Name:        "helm_install",
			Description: "Install or upgrade a Helm chart to clusters. Supports values overrides and targeting specific clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release_name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the Helm release",
					},
					"chart": map[string]interface{}{
						"type":        "string",
						"description": "Chart name or path (e.g., nginx, ./mychart, oci://registry/chart)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Target namespace (default: default)",
					},
					"values": map[string]interface{}{
						"type":        "object",
						"description": "Values to set (key-value pairs for --set)",
					},
					"values_yaml": map[string]interface{}{
						"type":        "string",
						"description": "Values in YAML format (equivalent to -f values.yaml)",
					},
					"version": map[string]interface{}{
						"type":        "string",
						"description": "Chart version to install",
					},
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Chart repository URL",
					},
					"wait": map[string]interface{}{
						"type":        "boolean",
						"description": "Wait for resources to be ready",
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Timeout for wait (e.g., 5m, 300s)",
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
				"required": []string{"release_name", "chart"},
			},
			Handler: s.handleHelmInstall,
		},
		{
			Name:        "helm_uninstall",
			Description: "Uninstall a Helm release from clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the Helm release to uninstall",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace of the release (default: default)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (clusters where release exists if not specified)",
					},
				},
				"required": []string{"release_name"},
			},
			Handler: s.handleHelmUninstall,
		},
		{
			Name:        "helm_list",
			Description: "List Helm releases across clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Filter by namespace",
					},
					"all_namespaces": map[string]interface{}{
						"type":        "boolean",
						"description": "List releases in all namespaces",
					},
					"filter": map[string]interface{}{
						"type":        "string",
						"description": "Filter releases by name regex",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
			},
			Handler: s.handleHelmList,
		},
		{
			Name:        "helm_rollback",
			Description: "Rollback a Helm release to a previous revision.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the Helm release",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace of the release (default: default)",
					},
					"revision": map[string]interface{}{
						"type":        "integer",
						"description": "Revision to rollback to (previous if not specified)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (clusters where release exists if not specified)",
					},
				},
				"required": []string{"release_name"},
			},
			Handler: s.handleHelmRollback,
		},
	}
}
