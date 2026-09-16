package gitops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover ReadFromGit error branches that were previously
// unreachable from the existing clone-oriented tests:
//
//   - manifest.go:246 (validateBranchName error propagation)
//   - manifest.go:224 (resetTempDir error propagation on second call)
//   - manifest.go:271 (ReadFromPath error propagation after successful clone)
//
// Together with the pre-existing happy-path / clone-failure / path-escape
// tests, these lift handleDetectDrift's sibling helper ReadFromGit from
// 85.3% to ~100% statement coverage.

// TestReadFromGit_InvalidBranchNameRejected: validateBranchName rejects
// names starting with '-' (which would otherwise be interpreted as a git
// flag). Exercises the `if err := validateBranchName(branch); err != nil`
// error branch at manifest.go:245-247.
func TestReadFromGit_InvalidBranchNameRejected(t *testing.T) {
	// The repo URL just has to pass validateRepoURLWithSchemes; it never
	// gets cloned because validateBranchName fails first.
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "-evil-flag",
	})
	if err == nil {
		t.Fatal("expected invalid-branch-name error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid git branch name") {
		t.Errorf("expected invalid-branch-name error, got: %v", err)
	}
}

// TestReadFromGit_ResetTempDirFailurePropagates seeds r.tempDir with a
// path under a read-only parent so that os.RemoveAll fails inside
// resetTempDir, then calls ReadFromGit. Exercises the
// `if err := r.resetTempDir(); err != nil` branch at manifest.go:223-225,
// which is not reachable from any test that only calls ReadFromGit on a
// fresh reader (r.tempDir == "" makes resetTempDir a no-op).
//
// Skipped on Windows (chmod-based permission blocking is unreliable) and
// when running as root (RemoveAll bypasses read-only parent bits).
func TestReadFromGit_ResetTempDirFailurePropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission blocking is unreliable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses read-only parent permissions")
	}

	// Build a repo just so ReadFromGit's URL validation succeeds. It
	// never gets that far because resetTempDir fails first.
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n",
	})

	// Populate a "previous" tempDir under a read-only parent so
	// os.RemoveAll cannot unlink it.
	parent := t.TempDir()
	child := filepath.Join(parent, "prev")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll(child): %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write child/f: %v", err)
	}
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("Chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	r.tempDir = child

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "main",
	})
	if err == nil {
		t.Fatal("expected resetTempDir failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to remove previous temp dir") {
		t.Errorf("expected resetTempDir wrap, got: %v", err)
	}
	// resetTempDir must NOT have cleared tempDir on error, so the field
	// still points at the un-removed directory.
	if r.tempDir != child {
		t.Errorf("resetTempDir should not clear tempDir on error; got %q", r.tempDir)
	}
}

// TestReadFromGit_ReadFromPathReturnsError forces ReadFromPath to fail
// after a successful clone by seeding the repo with a syntactically
// broken YAML file. Exercises the `if err != nil` branch at
// manifest.go:270-272 (ReadFromPath error propagation), which the
// happy-path clone tests bypass.
func TestReadFromGit_ReadFromPathReturnsError(t *testing.T) {
	// An unterminated flow sequence is a YAML parse error that
	// yaml.NewYAMLOrJSONDecoder surfaces from Decode.
	repoURL := makeLocalRepo(t, map[string]string{
		"manifests/broken.yaml": "key: [unterminated\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "main",
		Path:   "manifests",
	})
	if err == nil {
		t.Fatal("expected ReadFromPath error, got nil")
	}
	// ReadFromPath wraps the file-level error with "failed to read <path>".
	if !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("expected failed-to-read error from ReadFromPath, got: %v", err)
	}
}
