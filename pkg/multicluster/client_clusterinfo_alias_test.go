package multicluster

import (
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestClusterInfoIsSharedClusterType pins multicluster.ClusterInfo as an
// alias of cluster.ClusterInfo so both binaries keep a single cluster type,
// and that DiscoverClusters still populates the same fields it always has.
func TestClusterInfoIsSharedClusterType(t *testing.T) {
	shared := append([]cluster.ClusterInfo(nil), ClusterInfo{Name: "a"})
	if len(shared) != 1 || shared[0].Name != "a" {
		t.Fatalf("unexpected shared slice: %+v", shared)
	}

	m := &ClientManager{rawConfig: clientcmdapi.Config{
		CurrentContext: "ctx",
		Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c1"}},
		Clusters:       map[string]*clientcmdapi.Cluster{"c1": {Server: "https://example.invalid"}},
	}}
	clusters, err := m.DiscoverClusters()
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("DiscoverClusters() = %d clusters, want 1", len(clusters))
	}
	c := clusters[0]
	if c.Name != "ctx" || c.Server != "https://example.invalid" || !c.Current {
		t.Fatalf("unexpected cluster: %+v", c)
	}
	if c.Labels == nil {
		t.Fatalf("Labels = nil, want non-nil empty map")
	}
	if c.Source != "" || c.Context != "" || c.Status != "" {
		t.Fatalf("ops-only fields unexpectedly populated: %+v", c)
	}
}
