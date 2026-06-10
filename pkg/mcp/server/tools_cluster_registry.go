package server

import "context"

func init() {
	RegisterTool(Tool{
			Name:        "list_clusters",
			Description: "List all discovered Kubernetes clusters from kubeconfig and KubeStellar",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"source": {
						Type:        "string",
						Description: "Discovery source: all, kubeconfig, or kubestellar (not yet implemented)",
						Enum:        []string{"all", "kubeconfig", "kubestellar"},
					},
				},
			},
		},
		func(_ context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolListClusters(args)
		},
	)
	RegisterTool(Tool{
			Name:        "get_cluster_health",
			Description: "Check the health status of a Kubernetes cluster",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cluster": {
						Type:        "string",
						Description: "Name of the cluster to check (uses current context if not specified)",
					},
				},
			},
		},
		func(_ context.Context, s *Server, args map[string]interface{}) (string, bool) {
			return s.toolGetClusterHealth(args)
		},
	)
}
