package server

import "context"

func init() {
	RegisterTool(Tool{
			Name:        "detect_drift",
			Description: "Detect configuration drift between Git repository manifests and cluster state. Shows which resources differ.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"repo_url": {
						Type:        "string",
						Description: "Git repository URL (e.g., https://github.com/org/manifests)",
					},
					"path": {
						Type:        "string",
						Description: "Path within repository to YAML manifests (e.g., production/)",
					},
					"branch": {
						Type:        "string",
						Description: "Git branch to use (default: main)",
					},
					"cluster": {
						Type:        "string",
						Description: "Target cluster to check (uses current context if not specified)",
					},
					"namespace": {
						Type:        "string",
						Description: "Override namespace for all resources",
					},
				},
				Required: []string{"repo_url"},
			},
		},
		func(ctx context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolDetectDrift(ctx, args)
		},
	)
}
