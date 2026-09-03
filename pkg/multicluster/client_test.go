package multicluster

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestNewClientManagerDiscoverClustersAndCurrentContext(t *testing.T) {
	manager := newClientManagerFromKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	}, "beta")

	clusters, err := manager.DiscoverClusters()
	if err != nil {
		t.Fatalf("DiscoverClusters() error = %v", err)
	}

	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Name < clusters[j].Name })
	if len(clusters) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(clusters))
	}
	if got := clusters[0]; got.Name != "alpha" || got.Server != "https://alpha.example.com" || got.Current {
		t.Fatalf("unexpected alpha cluster: %#v", got)
	}
	if got := clusters[1]; got.Name != "beta" || got.Server != "https://beta.example.com" || !got.Current {
		t.Fatalf("unexpected beta cluster: %#v", got)
	}
	if got := manager.CurrentContext(); got != "beta" {
		t.Fatalf("CurrentContext() = %q, want beta", got)
	}
}

func TestGetClientAndConfigCacheByCluster(t *testing.T) {
	manager := newClientManagerFromKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
	}, "alpha")

	client1, err := manager.GetClient("alpha")
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	client2, err := manager.GetClient("alpha")
	if err != nil {
		t.Fatalf("GetClient() second error = %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected GetClient() to return cached client")
	}

	config1, err := manager.GetConfig("alpha")
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	config2, err := manager.GetConfig("alpha")
	if err != nil {
		t.Fatalf("GetConfig() second error = %v", err)
	}
	if config1 != config2 {
		t.Fatal("expected GetConfig() to return cached config")
	}
	if config1.Host != "https://alpha.example.com" {
		t.Fatalf("config host = %q, want https://alpha.example.com", config1.Host)
	}
}

func TestGetConfigCacheMissPopulatesFromGetClient(t *testing.T) {
	manager := newClientManagerFromKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
	}, "alpha")

	// Call GetConfig before GetClient: exercises the cache-miss branch
	// (exists=false) that delegates to GetClient to build+cache the config.
	config, err := manager.GetConfig("alpha")
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if config == nil {
		t.Fatal("GetConfig() returned nil config")
	}
	if config.Host != "https://alpha.example.com" {
		t.Fatalf("config host = %q, want https://alpha.example.com", config.Host)
	}

	// A second call should now hit the cache and return the same pointer.
	cached, err := manager.GetConfig("alpha")
	if err != nil {
		t.Fatalf("GetConfig() second call error = %v", err)
	}
	if cached != config {
		t.Fatal("expected second GetConfig() to return cached pointer")
	}
}

func TestGetConfigUnknownContextReturnsError(t *testing.T) {
	manager := newClientManagerFromKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
	}, "alpha")

	// Exercises the GetClient error passthrough in GetConfig.
	cfg, err := manager.GetConfig("missing")
	if err == nil {
		t.Fatalf("GetConfig(missing) error = nil, want error; got config = %#v", cfg)
	}
	if cfg != nil {
		t.Fatalf("GetConfig(missing) config = %#v, want nil", cfg)
	}
	if !strings.Contains(err.Error(), "failed to get config for context missing") {
		t.Fatalf("GetConfig(missing) error = %v, want missing-context failure", err)
	}
}

func TestGetClientUnknownContext(t *testing.T) {
	manager := newClientManagerFromKubeconfig(t, map[string]string{
		"alpha": "https://alpha.example.com",
	}, "alpha")

	_, err := manager.GetClient("missing")
	if err == nil || !strings.Contains(err.Error(), "failed to get config for context missing") {
		t.Fatalf("GetClient() error = %v, want missing context failure", err)
	}
}

func newClientManagerFromKubeconfig(t *testing.T, contexts map[string]string, currentContext string) *ClientManager {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.CurrentContext = currentContext
	for name, server := range contexts {
		config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
		config.Clusters[name] = &clientcmdapi.Cluster{Server: server}
		config.AuthInfos[name] = &clientcmdapi.AuthInfo{}
	}

	dir := newClientManagerTestDir(t)
	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}
	return manager
}

func newClientManagerTestDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	return dir
}
