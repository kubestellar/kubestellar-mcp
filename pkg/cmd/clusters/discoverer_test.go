package clusters

import (
	"testing"
)

// TestNewDiscovererDefaultReturnsClusterDiscoverer exercises the real
// (non-overridden) factory body in discoverer.go, which is otherwise never
// hit because health/list/clusters tests all reassign the newDiscoverer var
// to inject fakes. Locks in that:
//
//   - the factory returns a non-nil value,
//   - the returned value satisfies the clusterDiscoverer interface
//     (i.e. cluster.NewDiscoverer's return type continues to implement
//     DiscoverClusters/CheckHealth).
//
// If cluster.NewDiscoverer's signature drifts, this test fails to compile.
func TestNewDiscovererDefaultReturnsClusterDiscoverer(t *testing.T) {
	d := newDiscoverer("/tmp/does-not-exist.kubeconfig")
	if d == nil {
		t.Fatal("newDiscoverer returned nil for a non-empty kubeconfig path")
	}

	var _ clusterDiscoverer = d
}

func TestNewDiscovererDefaultAcceptsEmptyKubeconfig(t *testing.T) {
	d := newDiscoverer("")
	if d == nil {
		t.Fatal("newDiscoverer returned nil for empty kubeconfig path")
	}
}
