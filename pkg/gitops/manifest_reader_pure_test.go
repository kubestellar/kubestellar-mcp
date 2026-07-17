package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file cover pure (no-network, no-cluster) code paths of
// ManifestReader that previously had 0% coverage per issue #518:
// NewManifestReaderWithSchemes, ReadFromFile, ReadFromPath, and Cleanup.

func TestNewManifestReaderWithSchemesStoresAllowlist(t *testing.T) {
	t.Run("nil allowlist yields default behavior", func(t *testing.T) {
		r := NewManifestReaderWithSchemes(nil)
		if r == nil {
			t.Fatal("NewManifestReaderWithSchemes(nil) returned nil")
		}
		if r.AllowedSchemes != nil {
			t.Fatalf("AllowedSchemes = %v, want nil", r.AllowedSchemes)
		}
	})

	t.Run("empty allowlist rejects every scheme", func(t *testing.T) {
		r := NewManifestReaderWithSchemes(map[string]bool{})
		if r.AllowedSchemes == nil {
			t.Fatal("AllowedSchemes should be non-nil for empty map")
		}
		if len(r.AllowedSchemes) != 0 {
			t.Fatalf("AllowedSchemes len = %d, want 0", len(r.AllowedSchemes))
		}
	})

	t.Run("custom allowlist is preserved verbatim", func(t *testing.T) {
		schemes := map[string]bool{"file": true, "https": true}
		r := NewManifestReaderWithSchemes(schemes)
		if !r.AllowedSchemes["file"] || !r.AllowedSchemes["https"] {
			t.Fatalf("AllowedSchemes = %v, want file+https enabled", r.AllowedSchemes)
		}
		if r.AllowedSchemes["ssh"] {
			t.Fatalf("AllowedSchemes should not contain ssh: %v", r.AllowedSchemes)
		}
	})
}

func TestReadFromFileParsesValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cm.yaml")
	body := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
data:
  key: value
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reader := NewManifestReader()
	got, err := reader.ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadFromFile() len = %d, want 1", len(got))
	}
	if got[0].Kind != "ConfigMap" || got[0].Metadata.Name != "demo" {
		t.Fatalf("unexpected manifest: %+v", got[0])
	}
}

func TestReadFromFileReturnsErrorForMissingFile(t *testing.T) {
	reader := NewManifestReader()
	_, err := reader.ReadFromFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("ReadFromFile() error = nil, want non-nil for missing file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("ReadFromFile() error = %v, want an os.IsNotExist error", err)
	}
}

func TestReadFromFileReturnsErrorForMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	// Unterminated flow mapping — decoder must report an error.
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: {broken\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reader := NewManifestReader()
	_, err := reader.ReadFromFile(path)
	if err == nil {
		t.Fatal("ReadFromFile() error = nil for malformed YAML, want non-nil")
	}
}

func TestReadFromFileEmptyFileYieldsNoManifests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reader := NewManifestReader()
	got, err := reader.ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadFromFile(empty) len = %d, want 0", len(got))
	}
}

func TestReadFromPathWalksYAMLFilesAndIgnoresOthers(t *testing.T) {
	dir := t.TempDir()
	// yaml file
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"), 0o600); err != nil {
		t.Fatalf("WriteFile cm.yaml: %v", err)
	}
	// yml file in a subdirectory to exercise the recursive walk
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "dep.yml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: b\n"), 0o600); err != nil {
		t.Fatalf("WriteFile dep.yml: %v", err)
	}
	// non-yaml file must be ignored
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# ignore me\n"), 0o600); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	// yaml with no Kind — must be skipped by parseManifest
	if err := os.WriteFile(filepath.Join(dir, "nokind.yaml"), []byte("apiVersion: v1\nmetadata:\n  name: c\n"), 0o600); err != nil {
		t.Fatalf("WriteFile nokind.yaml: %v", err)
	}

	reader := NewManifestReader()
	got, err := reader.ReadFromPath(dir)
	if err != nil {
		t.Fatalf("ReadFromPath() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadFromPath() len = %d, want 2 (README.md and nokind.yaml ignored); got=%+v", len(got), got)
	}
	kinds := map[string]bool{}
	for _, m := range got {
		kinds[m.Kind] = true
	}
	if !kinds["ConfigMap"] || !kinds["Deployment"] {
		t.Fatalf("kinds = %v, want ConfigMap and Deployment", kinds)
	}
}

func TestReadFromPathEmptyDirectoryReturnsNoManifests(t *testing.T) {
	reader := NewManifestReader()
	got, err := reader.ReadFromPath(t.TempDir())
	if err != nil {
		t.Fatalf("ReadFromPath(empty) error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadFromPath(empty) len = %d, want 0", len(got))
	}
}

func TestReadFromPathMissingDirectoryReturnsError(t *testing.T) {
	reader := NewManifestReader()
	_, err := reader.ReadFromPath(filepath.Join(t.TempDir(), "nope", "still-nope"))
	if err == nil {
		t.Fatal("ReadFromPath(missing) error = nil, want non-nil")
	}
}

func TestReadFromPathPropagatesReadFromFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: {broken\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reader := NewManifestReader()
	_, err := reader.ReadFromPath(dir)
	if err == nil {
		t.Fatal("ReadFromPath() with malformed YAML error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("ReadFromPath() error = %v, want error to mention broken.yaml", err)
	}
}

func TestCleanupRemovesTempDirAndIsIdempotent_Pure(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kubestellar-cleanup-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	// Sanity: the dir exists.
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("Stat before cleanup: %v", err)
	}

	r := &ManifestReader{tempDir: tempDir}
	r.Cleanup()
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("Cleanup() left temp dir behind: stat err = %v", err)
	}
	if r.tempDir != "" {
		t.Fatalf("Cleanup() did not reset tempDir, got %q", r.tempDir)
	}

	// Second call must be a no-op and must not panic.
	r.Cleanup()
	if r.tempDir != "" {
		t.Fatalf("second Cleanup() populated tempDir: %q", r.tempDir)
	}
}

func TestCleanupNoOpWhenTempDirUnset(t *testing.T) {
	r := NewManifestReader()
	// Must not panic and must not touch the filesystem in any user-visible way.
	r.Cleanup()
	if r.tempDir != "" {
		t.Fatalf("Cleanup() populated tempDir on empty reader: %q", r.tempDir)
	}
}
