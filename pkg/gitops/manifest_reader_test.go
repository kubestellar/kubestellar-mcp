package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManifestReaderWithSchemes(t *testing.T) {
	tests := []struct {
		name    string
		schemes map[string]bool
	}{
		{
			name:    "nil schemes",
			schemes: nil,
		},
		{
			name:    "empty schemes",
			schemes: map[string]bool{},
		},
		{
			name:    "custom schemes with file",
			schemes: map[string]bool{"file": true, "https": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewManifestReaderWithSchemes(tt.schemes)
			if reader == nil {
				t.Fatal("NewManifestReaderWithSchemes() = nil, want non-nil reader")
			}
			if len(reader.AllowedSchemes) != len(tt.schemes) {
				t.Fatalf("AllowedSchemes len = %d, want %d", len(reader.AllowedSchemes), len(tt.schemes))
			}
			for k, v := range tt.schemes {
				if reader.AllowedSchemes[k] != v {
					t.Fatalf("AllowedSchemes[%q] = %v, want %v", k, reader.AllowedSchemes[k], v)
				}
			}
			if reader.tempDir != "" {
				t.Fatalf("tempDir = %q, want empty for new reader", reader.tempDir)
			}
		})
	}
}

func TestNewManifestReaderDefaultsToNilSchemes(t *testing.T) {
	reader := NewManifestReader()
	if reader == nil {
		t.Fatal("NewManifestReader() = nil, want non-nil reader")
	}
	if reader.AllowedSchemes != nil {
		t.Fatalf("AllowedSchemes = %#v, want nil so defaults apply", reader.AllowedSchemes)
	}
}

func TestReadFromFile(t *testing.T) {
	dir := t.TempDir()

	validPath := filepath.Join(dir, "valid.yaml")
	writeFile(t, validPath, `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: apps
data:
  mode: test
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
spec:
  replicas: 3
`)

	emptyPath := filepath.Join(dir, "empty.yaml")
	writeFile(t, emptyPath, "")

	invalidPath := filepath.Join(dir, "invalid.yaml")
	writeFile(t, invalidPath, "apiVersion: v1\nkind: ConfigMap\n  bad-indent: [unclosed\n")

	tests := []struct {
		name         string
		path         string
		wantErr      bool
		wantCount    int
		wantFirstKey ResourceKey
	}{
		{
			name:         "valid multi-document YAML",
			path:         validPath,
			wantCount:    2,
			wantFirstKey: ResourceKey{APIVersion: "v1", Kind: "ConfigMap", Namespace: "apps", Name: "demo-config"},
		},
		{
			name:      "empty file yields no manifests",
			path:      emptyPath,
			wantCount: 0,
		},
		{
			name:    "invalid YAML returns error",
			path:    invalidPath,
			wantErr: true,
		},
		{
			name:    "non-existent path returns error",
			path:    filepath.Join(dir, "does-not-exist.yaml"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewManifestReader()
			manifests, err := reader.ReadFromFile(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadFromFile(%q) error = nil, want error", tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFromFile(%q) unexpected error: %v", tt.path, err)
			}
			if len(manifests) != tt.wantCount {
				t.Fatalf("ReadFromFile(%q) len = %d, want %d", tt.path, len(manifests), tt.wantCount)
			}
			if tt.wantCount > 0 && manifests[0].GetKey() != tt.wantFirstKey {
				t.Fatalf("ReadFromFile(%q) first key = %#v, want %#v", tt.path, manifests[0].GetKey(), tt.wantFirstKey)
			}
		})
	}
}

func TestReadFromPath(t *testing.T) {
	t.Run("directory with manifests", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), `apiVersion: v1
kind: ConfigMap
metadata:
  name: a
`)
		writeFile(t, filepath.Join(dir, "b.yml"), `apiVersion: v1
kind: Secret
metadata:
  name: b
`)
		writeFile(t, filepath.Join(dir, "README.md"), "# ignored")

		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		writeFile(t, filepath.Join(sub, "c.yaml"), `apiVersion: apps/v1
kind: Deployment
metadata:
  name: c
`)

		reader := NewManifestReader()
		manifests, err := reader.ReadFromPath(dir)
		if err != nil {
			t.Fatalf("ReadFromPath() unexpected error: %v", err)
		}
		if len(manifests) != 3 {
			t.Fatalf("ReadFromPath() len = %d, want 3", len(manifests))
		}

		gotKinds := map[string]bool{}
		for _, m := range manifests {
			gotKinds[m.Kind] = true
		}
		for _, want := range []string{"ConfigMap", "Secret", "Deployment"} {
			if !gotKinds[want] {
				t.Fatalf("ReadFromPath() missing kind %q; got kinds %v", want, gotKinds)
			}
		}
	})

	t.Run("directory with no manifests", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "notes.txt"), "hello")
		writeFile(t, filepath.Join(dir, "chart.tgz"), "binary")

		reader := NewManifestReader()
		manifests, err := reader.ReadFromPath(dir)
		if err != nil {
			t.Fatalf("ReadFromPath() unexpected error: %v", err)
		}
		if len(manifests) != 0 {
			t.Fatalf("ReadFromPath() len = %d, want 0", len(manifests))
		}
	})

	t.Run("non-existent path returns error", func(t *testing.T) {
		dir := t.TempDir()
		reader := NewManifestReader()
		_, err := reader.ReadFromPath(filepath.Join(dir, "missing"))
		if err == nil {
			t.Fatal("ReadFromPath() error = nil, want error for non-existent path")
		}
	})

	t.Run("propagates invalid YAML error with file path", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "bad.yaml")
		writeFile(t, bad, "apiVersion: v1\nkind: ConfigMap\n  bad-indent: [unclosed\n")

		reader := NewManifestReader()
		_, err := reader.ReadFromPath(dir)
		if err == nil {
			t.Fatal("ReadFromPath() error = nil, want error for invalid YAML")
		}
		if !strings.Contains(err.Error(), "bad.yaml") {
			t.Fatalf("ReadFromPath() error = %v, want error to reference offending file", err)
		}
	})
}

func TestCleanupRemovesTempDirAndIsIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp(t.TempDir(), "cleanup-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}

	reader := NewManifestReader()
	reader.tempDir = dir

	reader.Cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(%q) error = %v, want not exists", dir, err)
	}
	if reader.tempDir != "" {
		t.Fatalf("tempDir after Cleanup = %q, want empty", reader.tempDir)
	}

	reader.Cleanup()
	if reader.tempDir != "" {
		t.Fatalf("tempDir after second Cleanup = %q, want empty", reader.tempDir)
	}
}

func TestCleanupNoTempDirIsNoop(t *testing.T) {
	reader := NewManifestReader()
	reader.Cleanup()
	if reader.tempDir != "" {
		t.Fatalf("tempDir = %q, want empty", reader.tempDir)
	}
}

func TestCleanupWhenDirAlreadyRemoved(t *testing.T) {
	dir, err := os.MkdirTemp(t.TempDir(), "cleanup-preremoved-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}

	reader := NewManifestReader()
	reader.tempDir = dir
	reader.Cleanup()
	if reader.tempDir != "" {
		t.Fatalf("tempDir = %q, want empty after Cleanup of already-removed dir", reader.tempDir)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
