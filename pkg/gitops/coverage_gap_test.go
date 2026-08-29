package gitops

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSyncerGetGVRPropagatesResolveError covers the error branch of
// (*Syncer).getGVR, exercised when resolveManifestResource fails —
// which happens for a manifest whose APIVersion is not parseable by
// schema.ParseGroupVersion (e.g. multi-slash strings). Previously only
// the happy path was covered.
func TestSyncerGetGVRPropagatesResolveError(t *testing.T) {
	s := &Syncer{}
	// "a/b/c" has two slashes — schema.ParseGroupVersion rejects it.
	_, err := s.getGVR(Manifest{APIVersion: "a/b/c", Kind: "Widget"})
	if err == nil {
		t.Fatal("getGVR() with unparseable APIVersion should return error, got nil")
	}
}

// TestDriftDetectorIsManifestClusterScopedFallsBackOnInvalidAPIVersion
// covers the error branch of IsManifestClusterScoped: when
// resolveManifestResource errors (invalid APIVersion), the function must
// fall back to the static IsClusterScoped(Kind) list. The existing
// "FallsBackToStaticList" test exercises the nil-mapper path but not
// the parse-error path, leaving the `return IsClusterScoped(...)`
// branch uncovered.
func TestDriftDetectorIsManifestClusterScopedFallsBackOnInvalidAPIVersion(t *testing.T) {
	d := &DriftDetector{}
	// Kind is cluster-scoped in the static list, APIVersion is invalid.
	if !d.IsManifestClusterScoped(Manifest{APIVersion: "a/b/c", Kind: "ClusterRole"}) {
		t.Fatal("ClusterRole should be classified cluster-scoped via static fallback")
	}
	if d.IsManifestClusterScoped(Manifest{APIVersion: "a/b/c", Kind: "ConfigMap"}) {
		t.Fatal("ConfigMap should be classified namespaced via static fallback")
	}
}

// TestManifestReaderCleanupSwallowsRemoveAllError covers the error
// branch of resetTempDir, reached from Cleanup when os.RemoveAll fails.
// We build a subdirectory under a read-only parent so RemoveAll cannot
// unlink it, then verify Cleanup returns quietly (per its documented
// contract) and does NOT clear tempDir — the pre-existing "happy path"
// tests never exercise this branch, leaving Cleanup at 50%.
//
// Skipped on Windows (permission model differs) and when running as
// root (RemoveAll can bypass read-only parents).
func TestManifestReaderCleanupSwallowsRemoveAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission blocking is unreliable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses read-only parent permissions")
	}

	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll(child): %v", err)
	}
	// Populate child so RemoveAll must call unlinkat inside it.
	if err := os.WriteFile(filepath.Join(child, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write child/f: %v", err)
	}
	// Freeze the parent so the child directory cannot be removed.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("Chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	r := &ManifestReader{tempDir: child}
	// Cleanup must not panic and must not clear tempDir on error, so
	// callers can retry once the read-only parent is restored.
	r.Cleanup()
	if r.tempDir != child {
		t.Fatalf("Cleanup() on RemoveAll-error path should not clear tempDir; got %q", r.tempDir)
	}
}
