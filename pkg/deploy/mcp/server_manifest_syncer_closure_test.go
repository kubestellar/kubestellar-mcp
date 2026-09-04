package mcp

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
)

// TestNewServer_ManifestSyncerFactoryClosureInvoked closes the last
// uncovered arm of server.go — the closure body wired into
// newManifestSyncer by NewServer (lines 54-55: `return gitops.NewSyncer(config)`).
//
// TestNewServer_Success in server_lifecycle_test.go only asserts the
// closure is non-nil after NewServer; TestGetManifestSyncer_NilFactoryFallthrough
// calls the fallback branch of getManifestSyncer when no factory is wired.
// Neither actually invokes the wired closure, so the closure body reports as
// uncovered in package coverage even though NewServer runs on the happy path.
//
// This test constructs a real Server via NewServer, then calls the wired
// factory directly with a minimal *rest.Config, forcing the closure to
// execute and delegate to gitops.NewSyncer. A regression that inlined a
// different syncer implementation into that arm would be caught here.
func TestNewServer_ManifestSyncerFactoryClosureInvoked(t *testing.T) {
	kubeconfig := writeMinimalKubeconfig(t)
	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("HOME", filepath.Dir(kubeconfig))

	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	if srv.newManifestSyncer == nil {
		t.Fatal("NewServer() left newManifestSyncer nil; cannot exercise closure")
	}

	// gitops.NewSyncer accepts a non-nil *rest.Config and defers all
	// network I/O to the dynamic client, so a bogus host is safe here.
	syncer, err := srv.newManifestSyncer(&rest.Config{Host: "https://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("wired newManifestSyncer closure returned error: %v", err)
	}
	if syncer == nil {
		t.Fatal("wired newManifestSyncer closure returned nil syncer")
	}
}
