package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
)

// writeMinimalKubeconfig writes a minimal kubeconfig into t.TempDir() and
// returns its path. It's the smallest shape NewClientManager will accept via
// KUBECONFIG. Named to avoid colliding with the existing writeKubeconfig
// helper in tools_app_status_test.go which has a different signature.
func writeMinimalKubeconfig(t *testing.T) string {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["test"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	cfg.AuthInfos["test"] = &clientcmdapi.AuthInfo{}
	cfg.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "test"}
	cfg.CurrentContext = "test"

	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// TestNewServer_Success covers the happy path of NewServer when a valid
// kubeconfig is discoverable through the standard loading rules. Previously
// no test hit NewServer at all (0% coverage).
func TestNewServer_Success(t *testing.T) {
	kubeconfig := writeMinimalKubeconfig(t)
	t.Setenv("KUBECONFIG", kubeconfig)
	// Prevent default loader from falling back to a real ~/.kube/config.
	t.Setenv("HOME", t.TempDir())

	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer() returned nil server")
	}
	if srv.manager == nil || srv.executor == nil || srv.selector == nil {
		t.Errorf("NewServer() left required deps unset: %+v", srv)
	}
	// The two manifest factories should be wired to real gitops constructors,
	// exercising the non-nil branches in getManifestReader/getManifestSyncer.
	if srv.newManifestReader == nil {
		t.Error("newManifestReader factory was not wired")
	}
	if srv.newManifestSyncer == nil {
		t.Error("newManifestSyncer factory was not wired")
	}

	reader := srv.getManifestReader()
	if reader == nil {
		t.Error("getManifestReader() = nil, want non-nil")
	}
}

// TestNewServer_KubeconfigLoadFailure covers the error branch of NewServer
// when the multicluster client manager cannot load a kubeconfig. Setting
// KUBECONFIG to an intentionally malformed file makes RawConfig() fail.
func TestNewServer_KubeconfigLoadFailure(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("::: not yaml :::"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("KUBECONFIG", bad)
	t.Setenv("HOME", t.TempDir())

	srv, err := NewServer()
	if err == nil {
		t.Fatalf("NewServer() = %+v, want error", srv)
	}
}

// TestRunMCPServer_PropagatesConstructionError covers the early-exit branch
// of RunMCPServer when NewServer fails. RunMCPServer is otherwise 0% because
// its success path reads from os.Stdin, which is unsafe to hijack in a unit
// test. This test exercises the failure branch by pointing KUBECONFIG at an
// unparseable file.
func TestRunMCPServer_PropagatesConstructionError(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("::: not yaml :::"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("KUBECONFIG", bad)
	t.Setenv("HOME", t.TempDir())

	if err := RunMCPServer(); err == nil {
		t.Fatal("RunMCPServer() returned nil, want error from failed NewServer")
	}
}

// TestGetManifestReader_NilFactoryFallthrough covers the else branch of
// getManifestReader when no factory is wired — a code path that previously
// showed up as an uncovered "return gitops.NewManifestReader()" line.
func TestGetManifestReader_NilFactoryFallthrough(t *testing.T) {
	srv := &Server{}
	if reader := srv.getManifestReader(); reader == nil {
		t.Fatal("getManifestReader() with nil factory returned nil")
	}
}

// TestGetManifestSyncer_NilFactoryFallthrough covers the else branch of
// getManifestSyncer. gitops.NewSyncer expects a non-nil *rest.Config, so we
// pass a minimal valid one; a bogus host is fine because dynamic.NewForConfig
// only builds a client and defers network I/O.
func TestGetManifestSyncer_NilFactoryFallthrough(t *testing.T) {
	srv := &Server{}
	cfg := &rest.Config{Host: "https://127.0.0.1:1"}
	syncer, err := srv.getManifestSyncer(cfg)
	if err != nil {
		t.Fatalf("getManifestSyncer(): %v", err)
	}
	if syncer == nil {
		t.Fatal("getManifestSyncer() returned nil syncer")
	}
	_ = gitops.NewManifestReader
}
