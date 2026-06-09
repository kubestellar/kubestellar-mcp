package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
)

// mockDiscoverer implements the discoverer interface for testing.
type mockDiscoverer struct {
	clusters []cluster.ClusterInfo
	health   *cluster.HealthInfo
	err      error
	healthBy map[string]*cluster.HealthInfo
}

func (m *mockDiscoverer) DiscoverClusters(source string) ([]cluster.ClusterInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if source == "all" || source == "" {
		return m.clusters, nil
	}
	var filtered []cluster.ClusterInfo
	for _, c := range m.clusters {
		if c.Source == source {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

func (m *mockDiscoverer) CheckHealthByContext(contextName string) (*cluster.HealthInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.healthBy != nil {
		if h, ok := m.healthBy[contextName]; ok {
			return h, nil
		}
	}
	return m.health, nil
}

func TestToolListClusters_Empty(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{clusters: nil}}

	result, isErr := s.toolListClusters(map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if result != "No clusters found" {
		t.Fatalf("expected 'No clusters found', got %q", result)
	}
}

func TestToolListClusters_DiscoveryError(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{err: errors.New("kubeconfig not found")}}

	result, isErr := s.toolListClusters(map[string]interface{}{})
	if !isErr {
		t.Fatal("expected isErr=true for discovery failure")
	}
	if !strings.Contains(result, "kubeconfig not found") {
		t.Fatalf("expected error message in result, got %q", result)
	}
}

func TestToolListClusters_MultipleClusters(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Source: "kubeconfig", Server: "https://prod:6443", Current: true, Status: "Ready"},
			{Name: "staging", Source: "kubestellar", Server: "https://staging:6443", Current: false},
		},
	}}

	result, isErr := s.toolListClusters(map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	if !strings.Contains(result, "prod (current)") {
		t.Errorf("expected current marker on prod, got:\n%s", result)
	}
	if !strings.Contains(result, "staging") {
		t.Errorf("expected staging in result, got:\n%s", result)
	}
	if !strings.Contains(result, "Source: kubeconfig") {
		t.Errorf("expected source info, got:\n%s", result)
	}
	if !strings.Contains(result, "Status: Ready") {
		t.Errorf("expected status for prod, got:\n%s", result)
	}
}

func TestToolListClusters_SourceFilter(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Source: "kubeconfig", Server: "https://prod:6443"},
			{Name: "edge", Source: "kubestellar", Server: "https://edge:6443"},
		},
	}}

	result, isErr := s.toolListClusters(map[string]interface{}{"source": "kubestellar"})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	if !strings.Contains(result, "edge") {
		t.Errorf("expected edge in filtered result, got:\n%s", result)
	}
	// Source filtering is done by discoverer; both may appear if mock returns all
}

func TestToolGetClusterHealth_CurrentCluster(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Context: "prod-ctx", Current: true, Server: "https://prod:6443"},
		},
		health: &cluster.HealthInfo{
			Status:          "Healthy",
			APIServerStatus: "Responding",
			NodesReady:      "3/3",
		},
	}}

	result, isErr := s.toolGetClusterHealth(map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	if !strings.Contains(result, "Cluster: prod") {
		t.Errorf("expected cluster name, got:\n%s", result)
	}
	if !strings.Contains(result, "Status: Healthy") {
		t.Errorf("expected healthy status, got:\n%s", result)
	}
	if !strings.Contains(result, "Nodes Ready: 3/3") {
		t.Errorf("expected nodes ready, got:\n%s", result)
	}
}

func TestToolGetClusterHealth_ByName(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Context: "prod-ctx", Current: true, Server: "https://prod:6443"},
			{Name: "staging", Context: "staging-ctx", Current: false, Server: "https://staging:6443"},
		},
		healthBy: map[string]*cluster.HealthInfo{
			"staging-ctx": {Status: "Degraded", APIServerStatus: "Responding", NodesReady: "2/3", Error: "node-3 NotReady"},
		},
	}}

	result, isErr := s.toolGetClusterHealth(map[string]interface{}{"cluster": "staging"})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	if !strings.Contains(result, "Cluster: staging") {
		t.Errorf("expected staging cluster name, got:\n%s", result)
	}
	if !strings.Contains(result, "Status: Degraded") {
		t.Errorf("expected degraded status, got:\n%s", result)
	}
	if !strings.Contains(result, "Error: node-3 NotReady") {
		t.Errorf("expected error message, got:\n%s", result)
	}
}

func TestToolGetClusterHealth_ClusterNotFound(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Context: "prod-ctx", Current: true},
		},
	}}

	result, isErr := s.toolGetClusterHealth(map[string]interface{}{"cluster": "nonexistent"})
	if !isErr {
		t.Fatal("expected isErr=true for missing cluster")
	}
	if !strings.Contains(result, "not found") {
		t.Fatalf("expected 'not found' error, got: %s", result)
	}
}

func TestToolGetClusterHealth_NoCurrentContext(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Context: "prod-ctx", Current: false},
		},
	}}

	result, isErr := s.toolGetClusterHealth(map[string]interface{}{})
	if !isErr {
		t.Fatal("expected isErr=true when no current context")
	}
	if !strings.Contains(result, "No current cluster context") {
		t.Fatalf("expected no context error, got: %s", result)
	}
}

func TestToolGetClusterHealth_DiscoveryError(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{err: errors.New("connection refused")}}

	result, isErr := s.toolGetClusterHealth(map[string]interface{}{})
	if !isErr {
		t.Fatal("expected isErr=true for discovery error")
	}
	if !strings.Contains(result, "connection refused") {
		t.Fatalf("expected error message, got: %s", result)
	}
}

func TestToolGetClusterHealth_ByContext(t *testing.T) {
	s := &Server{discoverer: &mockDiscoverer{
		clusters: []cluster.ClusterInfo{
			{Name: "prod", Context: "prod-ctx", Current: false},
		},
		health: &cluster.HealthInfo{
			Status:          "Healthy",
			APIServerStatus: "OK",
			NodesReady:      "5/5",
		},
	}}

	// toolGetClusterHealth matches by Name OR Context
	result, isErr := s.toolGetClusterHealth(map[string]interface{}{"cluster": "prod-ctx"})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, "Cluster: prod") {
		t.Errorf("expected cluster name, got:\n%s", result)
	}
}
