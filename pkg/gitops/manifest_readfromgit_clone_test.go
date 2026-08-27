package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ReadFromGit was previously only exercised for pre-clone URL / branch
// validation errors, leaving the happy path (git clone, manifest read,
// resolveManifestPath, resetTempDir on repeat calls) at ~15% coverage.
//
// These tests build a real local git repository in a temp dir, then
// allow the "file" scheme so ReadFromGit can clone it end-to-end. That
// exercises:
//
//   - the full happy path (clone succeeds, manifests parsed)
//   - resolveManifestPath when Path is a subdirectory
//   - resolveManifestPath's ".." and absolute-path guards inside the
//     post-clone code (previously only exercised via other callers)
//   - the git clone failure branch (URL points at a non-repo directory)
//   - the second-call resetTempDir cleanup arm (r.tempDir != "")
//
// All fixtures live under t.TempDir() and are cleaned up automatically.

// makeLocalRepo initialises a git repository at a fresh temp dir with
// the given files, then returns the file:// URL that can be passed to
// ReadFromGit. Skips the test if git is unavailable.
func makeLocalRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "safe.directory", dir)

	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")

	return "file://" + dir
}

// TestReadFromGit_HappyPath_LocalFileScheme clones a real local repo via
// the file:// scheme and asserts manifests are returned. This covers the
// full ReadFromGit success path (validate → resetTempDir(no-op) →
// MkdirTemp → git clone → resolveManifestPath → ReadFromPath → keep
// tempDir).
func TestReadFromGit_HappyPath_LocalFileScheme(t *testing.T) {
	repoURL := makeLocalRepo(t, map[string]string{
		"manifests/cm.yaml": strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
`) + "\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	got, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "main",
		Path:   "manifests",
	})
	if err != nil {
		t.Fatalf("ReadFromGit: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(got))
	}
	if got[0].Kind != "ConfigMap" || got[0].Metadata.Name != "test-cm" {
		t.Errorf("unexpected manifest: %+v", got[0])
	}
	if r.tempDir == "" {
		t.Error("expected tempDir to be retained after successful ReadFromGit")
	}
}

// TestReadFromGit_DefaultBranchIsMain confirms that an empty
// source.Branch falls through the `if branch == "" { branch = "main" }`
// arm. Also exercises resolveManifestPath with Path="" (returns baseDir
// unchanged).
func TestReadFromGit_DefaultBranchIsMain(t *testing.T) {
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: root-cm\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	got, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo: repoURL, // Branch omitted on purpose
	})
	if err != nil {
		t.Fatalf("ReadFromGit: %v", err)
	}
	if len(got) != 1 || got[0].Metadata.Name != "root-cm" {
		t.Errorf("unexpected manifests: %+v", got)
	}
}

// TestReadFromGit_SecondCallResetsTempDir walks ReadFromGit twice on the
// same *ManifestReader to force resetTempDir's non-trivial branch
// (r.tempDir != "") — the first call leaves tempDir populated, the
// second must remove it before creating a fresh MkdirTemp.
func TestReadFromGit_SecondCallResetsTempDir(t *testing.T) {
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	if _, err := r.ReadFromGit(context.Background(), ManifestSource{Repo: repoURL}); err != nil {
		t.Fatalf("first ReadFromGit: %v", err)
	}
	first := r.tempDir
	if first == "" {
		t.Fatal("expected tempDir after first call")
	}
	if _, err := r.ReadFromGit(context.Background(), ManifestSource{Repo: repoURL}); err != nil {
		t.Fatalf("second ReadFromGit: %v", err)
	}
	if r.tempDir == first {
		t.Errorf("expected tempDir to be replaced after second call; both are %q", first)
	}
	// The previous tempDir must be gone (resetTempDir removed it).
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("previous tempDir %q should have been removed by resetTempDir (stat err: %v)", first, err)
	}
}

// TestReadFromGit_CloneFails covers the `git clone` failure branch:
// URL passes scheme + branch validation but points at a directory that
// isn't a git repo, so git exits non-zero and the error is wrapped as
// "failed to clone repo".
func TestReadFromGit_CloneFails(t *testing.T) {
	dir := t.TempDir()
	nonRepo := filepath.Join(dir, "not-a-repo")
	if err := os.MkdirAll(nonRepo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   "file://" + nonRepo,
		Branch: "main",
	})
	if err == nil {
		t.Fatal("expected clone failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to clone repo") {
		t.Errorf("expected clone-failure error, got: %v", err)
	}
	// The deferred cleanupOnError=true block must have removed the temp
	// dir on this failure path.
	if r.tempDir != "" {
		if _, err := os.Stat(r.tempDir); !os.IsNotExist(err) {
			t.Errorf("expected tempDir %q to be cleaned up after clone failure (stat err: %v)", r.tempDir, err)
		}
	}
}

// TestReadFromGit_PathEscapeRejected clones successfully but requests a
// Path that escapes the repo directory (Path=".."). Exercises
// resolveManifestPath's ".." guard from inside ReadFromGit.
func TestReadFromGit_PathEscapeRejected(t *testing.T) {
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "main",
		Path:   "..",
	})
	if err == nil {
		t.Fatal("expected path-escape error, got nil")
	}
	if !strings.Contains(err.Error(), "escapes repository") {
		t.Errorf("expected escapes-repository error, got: %v", err)
	}
}

// TestReadFromGit_AbsolutePathRejected exercises the
// `filepath.IsAbs(requestedPath)` early return inside resolveManifestPath
// from ReadFromGit's post-clone code path.
func TestReadFromGit_AbsolutePathRejected(t *testing.T) {
	repoURL := makeLocalRepo(t, map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n",
	})

	r := NewManifestReaderWithSchemes(map[string]bool{"file": true})
	t.Cleanup(r.Cleanup)

	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   repoURL,
		Branch: "main",
		Path:   "/etc",
	})
	if err == nil {
		t.Fatal("expected absolute-path error, got nil")
	}
	if !strings.Contains(err.Error(), "escapes repository") {
		t.Errorf("expected escapes-repository error, got: %v", err)
	}
}
