package server

import (
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

func TestToolListClusters(t *testing.T) {
	t.Run("zero clusters", func(t *testing.T) {
		server := &Server{
			discoverer: stubDiscoverer{
				discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
					if source != "all" {
						t.Fatalf("DiscoverClusters source = %q, want all", source)
					}
					return []cluster.ClusterInfo{}, nil
				},
			},
		}

		result, rpcErr := callTool(t, server, "list_clusters", map[string]interface{}{})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if result.IsError {
			t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
		}
		if !strings.Contains(result.Content[0].Text, "No clusters found") {
			t.Fatalf("unexpected output: %s", result.Content[0].Text)
		}
	})

	t.Run("multiple clusters", func(t *testing.T) {
		server := &Server{
			discoverer: stubDiscoverer{
				discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
					if source != "all" {
						t.Fatalf("DiscoverClusters source = %q, want all", source)
					}
					return []cluster.ClusterInfo{
						{Name: "alpha", Context: "alpha", Source: "kubeconfig", Server: "https://alpha", Current: true, Status: "Healthy"},
						{Name: "beta", Context: "beta", Source: "kubestellar", Server: "https://beta"},
					}, nil
				},
			},
		}

		result, rpcErr := callTool(t, server, "list_clusters", map[string]interface{}{})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if result.IsError {
			t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
		}
		for _, want := range []string{"Discovered clusters:", "alpha (current)", "Source: kubeconfig", "Status: Healthy", "beta", "Source: kubestellar"} {
			if !strings.Contains(result.Content[0].Text, want) {
				t.Fatalf("result text %q missing %q", result.Content[0].Text, want)
			}
		}
	})
}

func TestToolGetClusterHealth(t *testing.T) {
	t.Run("current context", func(t *testing.T) {
		server := &Server{
			discoverer: stubDiscoverer{
				discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
					if source != "all" {
						t.Fatalf("DiscoverClusters source = %q, want all", source)
					}
					return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx", Current: true, Server: "https://alpha"}}, nil
				},
				checkHealthByCtxFn: func(contextName string) (*cluster.HealthInfo, error) {
					if contextName != "alpha-ctx" {
						t.Fatalf("CheckHealthByContext context = %q, want alpha-ctx", contextName)
					}
					return &cluster.HealthInfo{Status: "Healthy", APIServerStatus: "OK", NodesReady: "3/3"}, nil
				},
			},
		}

		result, rpcErr := callTool(t, server, "get_cluster_health", map[string]interface{}{})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if result.IsError {
			t.Fatalf("expected success result, got error: %s", result.Content[0].Text)
		}
		for _, want := range []string{"Cluster: alpha", "Status: Healthy", "API Server: OK", "Nodes Ready: 3/3"} {
			if !strings.Contains(result.Content[0].Text, want) {
				t.Fatalf("result text %q missing %q", result.Content[0].Text, want)
			}
		}
	})

	t.Run("missing cluster", func(t *testing.T) {
		server := &Server{
			discoverer: stubDiscoverer{
				discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
					return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx", Current: true}}, nil
				},
			},
		}

		result, rpcErr := callTool(t, server, "get_cluster_health", map[string]interface{}{"cluster": "missing"})
		if rpcErr != nil {
			t.Fatalf("unexpected RPC error: %v", rpcErr)
		}
		if !result.IsError {
			t.Fatal("expected tool error for missing cluster")
		}
		if !strings.Contains(result.Content[0].Text, "Cluster \"missing\" not found") {
			t.Fatalf("unexpected error text: %s", result.Content[0].Text)
		}
	})
}
