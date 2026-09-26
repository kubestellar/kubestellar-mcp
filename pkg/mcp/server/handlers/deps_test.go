package handlers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

const testKubeconfig = `apiVersion: v1
kind: Config
current-context: ctx1
clusters:
- name: cluster1
  cluster:
    server: https://127.0.0.1:65535
contexts:
- name: ctx1
  context:
    cluster: cluster1
    user: user1
- name: ctx2
  context:
    cluster: cluster1
    user: user1
users:
- name: user1
  user:
    token: abc
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestGetClientForCluster_WithFactory(t *testing.T) {
	expected := k8sfake.NewSimpleClientset()
	var seen string
	d := &Deps{ClientFactory: func(name string) (kubernetes.Interface, error) {
		seen = name
		return expected, nil
	}}

	got, err := d.GetClientForCluster("prod-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Fatal("returned client does not match expected")
	}
	if seen != "prod-cluster" {
		t.Fatalf("clusterName passed to factory = %q, want %q", seen, "prod-cluster")
	}
}

func TestGetClientForCluster_FactoryError(t *testing.T) {
	sentinel := errors.New("factory boom")
	d := &Deps{ClientFactory: func(string) (kubernetes.Interface, error) { return nil, sentinel }}

	got, err := d.GetClientForCluster("any")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil client on error, got %T", got)
	}
}

func TestGetClientForCluster_InvalidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: "/nonexistent/kubeconfig-should-not-exist"}

	if _, err := d.GetClientForCluster(""); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig (empty clusterName)")
	}
	// Non-empty clusterName exercises the CurrentContext override branch.
	if _, err := d.GetClientForCluster("some-cluster"); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig (non-empty clusterName)")
	}
}

func TestGetClientForCluster_ValidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: writeKubeconfig(t)}

	client, err := d.GetClientForCluster("")
	if err != nil {
		t.Fatalf("unexpected error with valid kubeconfig / empty cluster: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	client2, err := d.GetClientForCluster("ctx2")
	if err != nil {
		t.Fatalf("unexpected error with context override: %v", err)
	}
	if client2 == nil {
		t.Fatal("expected non-nil client with override")
	}
}

func TestGetDynamicClientForCluster_WithFactory(t *testing.T) {
	expected := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	d := &Deps{DynamicClientFactory: func(name string) (dynamic.Interface, error) {
		if name != "test-cluster" {
			t.Errorf("expected cluster 'test-cluster', got %q", name)
		}
		return expected, nil
	}}

	client, err := d.GetDynamicClientForCluster("test-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client != expected {
		t.Error("returned client does not match expected")
	}
}

func TestGetDynamicClientForCluster_InvalidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: "/nonexistent/kubeconfig"}
	if _, err := d.GetDynamicClientForCluster("test-cluster"); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig")
	}
}

func TestGetDynamicClientForCluster_ValidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: writeKubeconfig(t)}
	client, err := d.GetDynamicClientForCluster("ctx2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil dynamic client")
	}
}

func TestGetRESTConfigForCluster_WithFactory(t *testing.T) {
	expected := &rest.Config{Host: "https://test-server:6443"}
	d := &Deps{RESTConfigFactory: func(name string) (*rest.Config, error) {
		if name != "my-cluster" {
			t.Errorf("expected cluster 'my-cluster', got %q", name)
		}
		return expected, nil
	}}

	config, err := d.GetRESTConfigForCluster("my-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Host != expected.Host {
		t.Errorf("expected host %q, got %q", expected.Host, config.Host)
	}
}

func TestGetRESTConfigForCluster_InvalidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: "/nonexistent/kubeconfig"}
	if _, err := d.GetRESTConfigForCluster("test-cluster"); err == nil {
		t.Fatal("expected error with nonexistent kubeconfig")
	}
}

func TestGetRESTConfigForCluster_ValidKubeconfig(t *testing.T) {
	d := &Deps{Kubeconfig: writeKubeconfig(t)}
	config, err := d.GetRESTConfigForCluster("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Host != "https://127.0.0.1:65535" {
		t.Fatalf("unexpected host %q", config.Host)
	}
}

type fakeReader struct{ ManifestReader }

type fakeDetector struct{ DriftDetector }

func TestNewManifestReader_WithFactory(t *testing.T) {
	want := &fakeReader{}
	d := &Deps{ManifestReaderFactory: func() ManifestReader { return want }}
	if got := d.NewManifestReader(); got != want {
		t.Fatalf("NewManifestReader() = %v, want factory result", got)
	}
}

func TestNewDriftDetector_WithFactory(t *testing.T) {
	want := &fakeDetector{}
	var seen *rest.Config
	d := &Deps{DriftDetectorFactory: func(c *rest.Config) (DriftDetector, error) {
		seen = c
		return want, nil
	}}
	cfg := &rest.Config{Host: "https://x"}
	got, err := d.NewDriftDetector(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want || seen != cfg {
		t.Fatal("NewDriftDetector did not delegate to the factory with the given config")
	}
}

func TestNewFactories_FallbackWhenUnset(t *testing.T) {
	// Without factories, the real gitops implementations are used.
	d := &Deps{}

	r := d.NewManifestReader()
	if r == nil {
		t.Fatal("NewManifestReader fallback returned nil")
	}
	// Cleanup must be safe to call on the real reader.
	r.Cleanup()

	// Either outcome (detector or error) proves the non-factory branch ran;
	// a Host-only rest.Config is enough to construct clients without
	// contacting an API server.
	det, err := d.NewDriftDetector(&rest.Config{Host: "https://api.example.local"})
	if err == nil && det == nil {
		t.Fatal("NewDriftDetector fallback returned (nil, nil)")
	}
}
