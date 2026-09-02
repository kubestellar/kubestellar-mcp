package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestGetCurrentContextLoadError covers the previously-uncovered error
// arm of Discoverer.GetCurrentContext (discovery.go:200). Existing tests
// only exercise the happy path, so a regression that swallowed a broken
// kubeconfig and returned "" with a nil error would go undetected — an
// operator would silently target the wrong cluster.
func TestGetCurrentContextLoadError(t *testing.T) {
	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "broken-kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("clusters: ["), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := NewDiscoverer(kubeconfig).GetCurrentContext()
	if err == nil {
		t.Fatalf("GetCurrentContext() error = nil, want load failure")
	}
	if got != "" {
		t.Fatalf("GetCurrentContext() = %q, want empty string on error", got)
	}
}

// TestGetCurrentContextDefaultLoadingRules covers the branch where
// d.kubeconfig is empty and NewDefaultClientConfigLoadingRules() is used
// unchanged. Point KUBECONFIG at a valid file so the load succeeds
// deterministically without depending on the developer's ~/.kube/config.
func TestGetCurrentContextDefaultLoadingRules(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t, map[string]string{
		"solo": "https://solo.example.com",
	}, "solo")

	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("HOME", newTestDir(t))

	got, err := NewDiscoverer("").GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error = %v", err)
	}
	if got != "solo" {
		t.Fatalf("GetCurrentContext() = %q, want %q", got, "solo")
	}
}

// TestDiscoverFromKubeconfigSkipsContextsMissingCluster covers the
// `if !ok { continue }` skip arm inside discoverFromKubeconfig
// (discovery.go:92). A context that references a Cluster which is not
// defined in the kubeconfig file must be silently skipped, not surfaced
// as a ClusterInfo with an empty Server. Without this coverage, a
// refactor that instead emitted the dangling context would produce
// health-check failures against "" endpoints in production.
func TestDiscoverFromKubeconfigSkipsContextsMissingCluster(t *testing.T) {
	config := clientcmdapi.NewConfig()
	config.CurrentContext = "good"

	// Valid context: cluster defined.
	config.Contexts["good"] = &clientcmdapi.Context{Cluster: "good", AuthInfo: "good"}
	config.Clusters["good"] = &clientcmdapi.Cluster{Server: "https://good.example.com"}
	config.AuthInfos["good"] = &clientcmdapi.AuthInfo{}

	// Dangling context: references a cluster name that does not exist.
	config.Contexts["orphan"] = &clientcmdapi.Context{Cluster: "does-not-exist", AuthInfo: "good"}

	dir := newTestDir(t)
	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	clusters, err := NewDiscoverer(kubeconfig).DiscoverClusters("all")
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("cluster count = %d, want 1 (orphan context must be skipped); got %#v", len(clusters), clusters)
	}
	if clusters[0].Name != "good" {
		t.Fatalf("cluster name = %q, want %q", clusters[0].Name, "good")
	}
	if clusters[0].Server != "https://good.example.com" {
		t.Fatalf("cluster server = %q, want the defined endpoint", clusters[0].Server)
	}
}

// TestDiscoverClustersUnsupportedSourceMessage locks down the exact
// error text produced by the default arm of DiscoverClusters
// (discovery.go:65). The existing test only asserts a substring match;
// asserting the source value is echoed back guards against a refactor
// that drops the offending value from the message and makes
// misconfiguration harder to diagnose.
func TestDiscoverClustersUnsupportedSourceMessage(t *testing.T) {
	_, err := NewDiscoverer("").DiscoverClusters("nope-not-real")
	if err == nil {
		t.Fatalf("DiscoverClusters() error = nil, want unsupported source failure")
	}
	if !strings.Contains(err.Error(), `"nope-not-real"`) {
		t.Fatalf("error = %q, want to include quoted source name", err.Error())
	}
	if !strings.Contains(err.Error(), "unsupported discovery source") {
		t.Fatalf("error = %q, want to include 'unsupported discovery source'", err.Error())
	}
}
