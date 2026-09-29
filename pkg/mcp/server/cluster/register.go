package cluster

import (
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// Tools returns the cluster-domain tool definitions in registration order.
// The order and schema shape match the pre-refactor init() in
// pkg/mcp/server/tools_cluster_registry.go byte-for-byte, so the MCP
// tools/list response stays identical after this extraction.
func Tools() []handlers.ToolDef {
	return []handlers.ToolDef{
		{
			Schema: protocol.Tool{
				Name:        "list_clusters",
				Description: "List all discovered Kubernetes clusters from kubeconfig and KubeStellar",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"source": {
							Type:        "string",
							Description: "Discovery source: all, kubeconfig, or kubestellar (not yet implemented)",
							Enum:        []string{"all", "kubeconfig", "kubestellar"},
						},
					},
				},
			},
			Handler: ListClusters,
		},
		{
			Schema: protocol.Tool{
				Name:        "get_cluster_health",
				Description: "Check the health status of a Kubernetes cluster",
				InputSchema: protocol.InputSchema{
					Type: "object",
					Properties: map[string]protocol.Property{
						"cluster": {
							Type:        "string",
							Description: "Name of the cluster to check (uses current context if not specified)",
						},
					},
				},
			},
			Handler: GetClusterHealth,
		},
	}
}
