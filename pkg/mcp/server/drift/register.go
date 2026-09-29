package drift

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// Register adds the drift-domain tools to reg. pkg/mcp/server calls it from
// tools_drift_registry.go's init(), so the tool's position in tools/list is
// unchanged from when it was registered directly in that file.
func Register(reg *handlers.Registry) {
	reg.Register(protocol.Tool{
		Name:        "detect_drift",
		Description: "Detect configuration drift between Git repository manifests and cluster state. Shows which resources differ.",
		InputSchema: protocol.InputSchema{
			Type: "object",
			Properties: map[string]protocol.Property{
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
		toolDetectDrift,
	)
}
