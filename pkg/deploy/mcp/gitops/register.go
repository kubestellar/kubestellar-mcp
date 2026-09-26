package gitops

import "github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/tooldef"

// Tools returns the gitops tool definitions bound to s, in the same order as
// the pre-refactor gitopsToolDefs so tools/list output stays byte-identical.
func (s *Server) Tools() []tooldef.ToolDef {
	return []tooldef.ToolDef{
		{
			Name:        "detect_drift",
			Description: "Detect drift between git manifests and cluster state. Shows which resources differ between git and what's deployed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL (e.g., https://github.com/org/manifests)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests (e.g., production/)",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
			Handler: s.HandleDetectDrift,
		},
		{
			Name:        "sync_from_git",
			Description: "Sync manifests from a git repository to clusters. Applies all manifests found in the specified path.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Override namespace for all resources",
					},
				},
				"required": []string{"repo"},
			},
			Handler: s.HandleSyncFromGit,
		},
		{
			Name:        "reconcile",
			Description: "Bring clusters back in sync with git. Same as sync_from_git but always applies changes.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
			Handler: s.HandleReconcile,
		},
		{
			Name:        "preview_changes",
			Description: "Preview what would change if manifests were synced from git. Dry-run mode.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{
						"type":        "string",
						"description": "Git repository URL",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path within repo to manifests",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Git branch (default: main)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"repo"},
			},
			Handler: s.HandlePreviewChanges,
		},
	}
}
