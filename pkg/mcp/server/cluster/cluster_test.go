package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

type stubDiscoverer struct {
	discoverClusters   func(source string) ([]cluster.ClusterInfo, error)
	checkHealthByCtxFn func(contextName string) (*cluster.HealthInfo, error)
}

func (s stubDiscoverer) DiscoverClusters(source string) ([]cluster.ClusterInfo, error) {
	if s.discoverClusters != nil {
		return s.discoverClusters(source)
	}
	return nil, nil
}

func (s stubDiscoverer) CheckHealthByContext(contextName string) (*cluster.HealthInfo, error) {
	if s.checkHealthByCtxFn != nil {
		return s.checkHealthByCtxFn(contextName)
	}
	return nil, nil
}

func newDeps(d stubDiscoverer) *handlers.Deps {
	return &handlers.Deps{Discoverer: d}
}

func TestListClusters(t *testing.T) {
	t.Run("zero clusters", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				if source != "all" {
					t.Fatalf("DiscoverClusters source = %q, want all", source)
				}
				return []cluster.ClusterInfo{}, nil
			},
		})
		text, isErr := ListClusters(context.Background(), d, map[string]interface{}{})
		if isErr {
			t.Fatalf("expected success, got error: %s", text)
		}
		if !strings.Contains(text, "No clusters found") {
			t.Fatalf("unexpected output: %s", text)
		}
	})

	t.Run("respects explicit source", func(t *testing.T) {
		var seen string
		d := newDeps(stubDiscoverer{
			discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				seen = source
				return nil, nil
			},
		})
		_, _ = ListClusters(context.Background(), d, map[string]interface{}{"source": "kubeconfig"})
		if seen != "kubeconfig" {
			t.Fatalf("source = %q, want kubeconfig", seen)
		}
	})

	t.Run("multiple clusters", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(source string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{
					{Name: "alpha", Context: "alpha", Source: "kubeconfig", Server: "https://alpha", Current: true, Status: "Healthy"},
					{Name: "beta", Context: "beta", Source: "kubestellar", Server: "https://beta"},
				}, nil
			},
		})
		text, isErr := ListClusters(context.Background(), d, map[string]interface{}{})
		if isErr {
			t.Fatalf("expected success, got error: %s", text)
		}
		for _, want := range []string{"Discovered clusters:", "alpha (current)", "Source: kubeconfig", "Status: Healthy", "beta", "Source: kubestellar"} {
			if !strings.Contains(text, want) {
				t.Fatalf("result text %q missing %q", text, want)
			}
		}
	})

	t.Run("discovery error", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return nil, errors.New("boom")
			},
		})
		text, isErr := ListClusters(context.Background(), d, map[string]interface{}{})
		if !isErr {
			t.Fatalf("expected error, got: %s", text)
		}
		if !strings.Contains(text, "Failed to discover clusters") || !strings.Contains(text, "boom") {
			t.Fatalf("unexpected error text: %s", text)
		}
	})
}

func TestGetClusterHealth(t *testing.T) {
	t.Run("current context", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
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
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{})
		if isErr {
			t.Fatalf("expected success, got: %s", text)
		}
		for _, want := range []string{"Cluster: alpha", "Status: Healthy", "API Server: OK", "Nodes Ready: 3/3"} {
			if !strings.Contains(text, want) {
				t.Fatalf("result text %q missing %q", text, want)
			}
		}
	})

	t.Run("named cluster match by context", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx"}}, nil
			},
			checkHealthByCtxFn: func(string) (*cluster.HealthInfo, error) {
				return &cluster.HealthInfo{Status: "Degraded", APIServerStatus: "Slow", NodesReady: "2/3", Error: "warn"}, nil
			},
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{"cluster": "alpha-ctx"})
		if isErr {
			t.Fatalf("expected success, got: %s", text)
		}
		for _, want := range []string{"Cluster: alpha", "Status: Degraded", "Error: warn"} {
			if !strings.Contains(text, want) {
				t.Fatalf("result text %q missing %q", text, want)
			}
		}
	})

	t.Run("no current context", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx"}}, nil
			},
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{})
		if !isErr {
			t.Fatalf("expected error, got: %s", text)
		}
		if !strings.Contains(text, "No current cluster context set") {
			t.Fatalf("unexpected text: %s", text)
		}
	})

	t.Run("missing cluster", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx", Current: true}}, nil
			},
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{"cluster": "missing"})
		if !isErr {
			t.Fatal("expected error for missing cluster")
		}
		if !strings.Contains(text, "Cluster \"missing\" not found") {
			t.Fatalf("unexpected text: %s", text)
		}
	})

	t.Run("discovery error", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return nil, errors.New("boom")
			},
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{})
		if !isErr {
			t.Fatalf("expected error, got: %s", text)
		}
		if !strings.Contains(text, "Failed to discover clusters") {
			t.Fatalf("unexpected text: %s", text)
		}
	})

	t.Run("health check error", func(t *testing.T) {
		d := newDeps(stubDiscoverer{
			discoverClusters: func(string) ([]cluster.ClusterInfo, error) {
				return []cluster.ClusterInfo{{Name: "alpha", Context: "alpha-ctx", Current: true}}, nil
			},
			checkHealthByCtxFn: func(string) (*cluster.HealthInfo, error) {
				return nil, errors.New("api down")
			},
		})
		text, isErr := GetClusterHealth(context.Background(), d, map[string]interface{}{})
		if !isErr {
			t.Fatalf("expected error, got: %s", text)
		}
		if !strings.Contains(text, "Failed to check health") || !strings.Contains(text, "api down") {
			t.Fatalf("unexpected text: %s", text)
		}
	})
}
