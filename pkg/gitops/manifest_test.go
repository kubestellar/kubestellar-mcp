package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceKeyString(t *testing.T) {
	if got := (ResourceKey{APIVersion: "v1", Kind: "ConfigMap", Namespace: "demo", Name: "app"}).String(); got != "v1/ConfigMap/demo/app" {
		t.Fatalf("namespaced ResourceKey.String() = %q", got)
	}
	if got := (ResourceKey{APIVersion: "v1", Kind: "Namespace", Name: "demo"}).String(); got != "v1/Namespace/demo" {
		t.Fatalf("cluster-scoped ResourceKey.String() = %q", got)
	}
}

func TestReadFromReaderParsesMultipleDocuments(t *testing.T) {
	reader := NewManifestReader()
	data := strings.NewReader(`apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  labels:
    app: demo
data:
  mode: test
---
{}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  namespace: workloads
spec:
  replicas: 2
`)

	manifests, err := reader.ReadFromReader(data)
	if err != nil {
		t.Fatalf("ReadFromReader() unexpected error: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("ReadFromReader() len = %d, want 2", len(manifests))
	}
	if manifests[0].Kind != "ConfigMap" || manifests[0].Metadata.Labels["app"] != "demo" {
		t.Fatalf("unexpected first manifest: %#v", manifests[0])
	}
	if manifests[1].GetNamespace() != "workloads" {
		t.Fatalf("second manifest namespace = %q, want workloads", manifests[1].GetNamespace())
	}
}

func TestParseManifestExtractsFields(t *testing.T) {
	manifest := parseManifest(map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      "demo-secret",
			"namespace": "apps",
			"labels": map[string]interface{}{
				"app": "demo",
			},
			"annotations": map[string]interface{}{
				"team": "platform",
			},
		},
		"data": map[string]interface{}{"token": "abc"},
	})

	if manifest.APIVersion != "v1" || manifest.Kind != "Secret" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if manifest.Metadata.Name != "demo-secret" || manifest.Metadata.Namespace != "apps" {
		t.Fatalf("unexpected metadata: %#v", manifest.Metadata)
	}
	if manifest.Metadata.Labels["app"] != "demo" || manifest.Metadata.Annotations["team"] != "platform" {
		t.Fatalf("unexpected labels or annotations: %#v", manifest.Metadata)
	}
	if manifest.Data["token"] != "abc" {
		t.Fatalf("unexpected data: %#v", manifest.Data)
	}
}

func TestManifestGetKeyAndNamespaceDefault(t *testing.T) {
	manifest := &Manifest{APIVersion: "v1", Kind: "ConfigMap", Metadata: ManifestMetadata{Name: "demo"}}
	if got := manifest.GetNamespace(); got != "default" {
		t.Fatalf("GetNamespace() = %q, want default", got)
	}
	if got := manifest.GetKey(); got.Name != "demo" || got.Namespace != "" {
		t.Fatalf("unexpected key: %#v", got)
	}
}

func TestResolveManifestPathRejectsTraversal(t *testing.T) {
	baseDir := filepath.Join(string(os.PathSeparator), "repo", "clone")

	_, err := resolveManifestPath(baseDir, "../../etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "escapes repository directory") {
		t.Fatalf("resolveManifestPath() error = %v, want traversal rejection", err)
	}

	_, err = resolveManifestPath(baseDir, "/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "escapes repository directory") {
		t.Fatalf("resolveManifestPath() absolute path error = %v, want traversal rejection", err)
	}
}

func TestResolveManifestPathAllowsRepoRelativePath(t *testing.T) {
	baseDir := filepath.Join(string(os.PathSeparator), "repo", "clone")
	got, err := resolveManifestPath(baseDir, "manifests/app")
	if err != nil {
		t.Fatalf("resolveManifestPath() unexpected error: %v", err)
	}
	want := filepath.Join(baseDir, "manifests", "app")
	if got != want {
		t.Fatalf("resolveManifestPath() = %q, want %q", got, want)
	}
}

func TestResetTempDirRemovesPreviousDirectory(t *testing.T) {
	dir, err := os.MkdirTemp(".", "manifest-reader-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	reader := NewManifestReader()
	reader.tempDir = dir
	if err := reader.resetTempDir(); err != nil {
		t.Fatalf("resetTempDir() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("os.Stat() error = %v, want not exists", err)
	}
	if reader.tempDir != "" {
		t.Fatalf("tempDir = %q, want empty", reader.tempDir)
	}
}
