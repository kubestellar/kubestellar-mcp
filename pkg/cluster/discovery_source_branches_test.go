package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverClustersKubeconfigSourceHappyPath covers the "kubeconfig"
// switch-case in DiscoverClusters. The sibling
// TestDiscoverClustersAndCurrentContext exercises the "all" case's happy
// path, but the "kubeconfig" case's `clusters = append(clusters,
// kubeconfigClusters...)` line stays uncovered when only the invalid-config
// arm is tested against it. This test drives the happy path directly.
func TestDiscoverClustersKubeconfigSourceHappyPath(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
	}, "alpha")

	clusters, err := NewDiscoverer(kubeconfig).DiscoverClusters("kubeconfig")
	if err != nil {
		t.Fatalf("DiscoverClusters(%q) error = %v", "kubeconfig", err)
	}
	if len(clusters) != 1 || clusters[0].Name != "alpha" {
		t.Fatalf("clusters = %#v, want single alpha entry", clusters)
	}
	if clusters[0].Source != "kubeconfig" {
		t.Fatalf("Source = %q, want kubeconfig", clusters[0].Source)
	}
}

// TestDiscoverClustersAllSourceErrorBranch is the twin of
// TestDiscoverClustersInvalidKubeconfig for the "all" case. Without this,
// only the "kubeconfig" case's kubeconfig-load error branch is exercised;
// the "all" case has its own separate `return nil, fmt.Errorf("kubeconfig
// discovery failed: %w", err)` line at DiscoverClusters that would silently
// go dead if the switch ever re-shuffled.
func TestDiscoverClustersAllSourceErrorBranch(t *testing.T) {
	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "broken-all-kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("clusters: ["), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewDiscoverer(kubeconfig).DiscoverClusters("all")
	if err == nil || !strings.Contains(err.Error(), "kubeconfig discovery failed") {
		t.Fatalf("DiscoverClusters(%q) error = %v, want kubeconfig discovery failed", "all", err)
	}
}

// TestCheckHealthBuildClientFailure covers the `failed to build client`
// error arm of CheckHealth. A malformed kubeconfig makes buildClient's
// clientConfig.ClientConfig() call return an error before any remote
// request is attempted, so this is fully hermetic.
func TestCheckHealthBuildClientFailure(t *testing.T) {
	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "malformed-kubeconfig")
	// A syntactically-broken YAML makes loadingRules.Load() error, which
	// propagates out of clientConfig.ClientConfig() inside buildClient.
	if err := os.WriteFile(kubeconfig, []byte(":not-yaml:"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewDiscoverer(kubeconfig).CheckHealth(ClusterInfo{Context: "any"})
	if err == nil || !strings.Contains(err.Error(), "failed to build client") {
		t.Fatalf("CheckHealth() error = %v, want failed to build client", err)
	}
}
