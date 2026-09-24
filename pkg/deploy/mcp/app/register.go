package app

import (
	"context"
	"encoding/json"
)

// ToolDef mirrors the shape of the root package's (unexported) toolDef so
// the root adapter (pkg/deploy/mcp/app_adapter.go) can convert this slice
// into its own toolDef type without any loss of information.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(ctx context.Context, args json.RawMessage) (interface{}, error)
}

// Tools returns the app-domain tool definitions, bound to the given
// Executor. The order matches the pre-refactor appToolDefs() in
// pkg/deploy/mcp/tools_app.go exactly, since tools/list order must stay
// byte-identical (see registry.go).
func Tools(executor Executor) []ToolDef {
	return []ToolDef{
		{
			Name:        "get_app_instances",
			Description: "Find all instances of an app across all clusters. Returns where the app is running, replica counts, and health status.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name to search for (matches label app=<name> or name contains <name>)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace to search in (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return GetAppInstances(ctx, executor, args)
			},
		},
		{
			Name:        "get_app_status",
			Description: "Get unified status of an app across all clusters. Shows health (healthy/degraded/failed), replica counts, and any issues.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
				},
				"required": []string{"app"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return GetAppStatus(ctx, executor, args)
			},
		},
		{
			Name:        "get_app_logs",
			Description: "Get aggregated logs from an app across all clusters. Logs are labeled with cluster name for easy identification.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (all namespaces if not specified)",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines from end (default 100)",
					},
					"since": map[string]interface{}{
						"type":        "string",
						"description": "Only return logs newer than duration (e.g., 1h, 30m)",
					},
				},
				"required": []string{"app"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
				return GetAppLogs(ctx, executor, args)
			},
		},
	}
}
