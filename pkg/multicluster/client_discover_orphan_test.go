package multicluster

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestDiscoverClustersSkipsContextWithMissingCluster covers the
// previously-untested `!exists` continue arm at
// pkg/multicluster/client.go:60:
//
//	cluster, exists := m.rawConfig.Clusters[context.Cluster]
//	if !exists {
//	    continue
//	}
//
// A kubeconfig can legitimately contain a Context whose `Cluster`
// field references a cluster name that isn't defined (partial edits,
// merged fragments, malformed configs). DiscoverClusters must skip
// those contexts rather than emit a ClusterInfo with a nil Server —
// which would surface to callers as an empty-server cluster entry.
//
// Existing tests only build fully-consistent kubeconfigs, so this arm
// never fires under the baseline suite.
func TestDiscoverClustersSkipsContextWithMissingCluster(t *testing.T) {
	config := clientcmdapi.NewConfig()
	config.CurrentContext = "good"

	// Valid, self-consistent context.
	config.Contexts["good"] = &clientcmdapi.Context{Cluster: "good", AuthInfo: "good"}
	config.Clusters["good"] = &clientcmdapi.Cluster{Server: "https://good.example.com"}
	config.AuthInfos["good"] = &clientcmdapi.AuthInfo{}

	// Orphan context: references a Cluster key that isn't in
	// config.Clusters. This is what triggers the `!exists` arm.
	config.Contexts["orphan"] = &clientcmdapi.Context{Cluster: "missing", AuthInfo: "good"}

	dir := newClientManagerTestDir(t)
	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}

	clusters, err := manager.DiscoverClusters()
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}

	if len(clusters) != 1 {
		t.Fatalf("cluster count = %d, want 1 (orphan context must be skipped); got %#v", len(clusters), clusters)
	}
	if got := clusters[0]; got.Name != "good" || got.Server != "https://good.example.com" {
		t.Fatalf("unexpected cluster returned: %#v", got)
	}
	for _, c := range clusters {
		if c.Name == "orphan" {
			t.Fatalf("orphan context leaked into DiscoverClusters output: %#v", c)
		}
	}
}
