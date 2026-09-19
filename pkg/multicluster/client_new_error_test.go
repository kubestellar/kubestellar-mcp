package multicluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewClientManagerRawConfigError covers the previously untested branch in
// NewClientManager where clientcmd.RawConfig() fails on an unparseable
// kubeconfig file (client.go:43-44). The fast-fail path returns a wrapped
// "failed to load kubeconfig" error instead of a half-initialised manager.
func TestNewClientManagerRawConfigError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(bad, []byte("not: [valid: yaml: at all"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mgr, err := NewClientManager(bad)
	if err == nil {
		t.Fatalf("NewClientManager(malformed) error = nil, want error; manager = %#v", mgr)
	}
	if mgr != nil {
		t.Fatalf("NewClientManager(malformed) manager = %#v, want nil", mgr)
	}
	if !strings.Contains(err.Error(), "failed to load kubeconfig") {
		t.Fatalf("NewClientManager(malformed) error = %v, want wrapped 'failed to load kubeconfig'", err)
	}
}
