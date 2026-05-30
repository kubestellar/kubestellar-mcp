package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

func TestHandleKustomizeBuildValidatesPath(t *testing.T) {
	server := newKustomizeTestServer(t, nil)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing path parameter",
			args:    map[string]interface{}{},
			wantErr: "path is required",
		},
		{
			name:    "empty path",
			args:    map[string]interface{}{"path": ""},
			wantErr: "path is required",
		},
		{
			name:    "nonexistent path",
			args:    map[string]interface{}{"path": "/nonexistent/path"},
			wantErr: "no kustomization.yaml or kustomization.yml found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := mustMarshalJSON(t, tt.args)
			_, err := server.handleKustomizeBuild(context.Background(), args)
			if err == nil {
				t.Fatalf("handleKustomizeBuild() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleKustomizeBuild() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleKustomizeBuildWithFakeBinary(t *testing.T) {
	setupFakeKustomize(t)
	server := newKustomizeTestServer(t, nil)

	// Create a test kustomization directory
	dir := createTestKustomization(t, "kustomization.yaml")

	t.Setenv("FAKE_KUSTOMIZE_OUTPUT", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo1\n---\nkind: Service\n")

	args := mustMarshalJSON(t, map[string]interface{}{
		"path": dir,
	})

	got, err := server.handleKustomizeBuild(context.Background(), args)
	if err != nil {
		t.Fatalf("handleKustomizeBuild() error = %v", err)
	}

	result := got.(map[string]interface{})
	if result["path"] != dir {
		t.Fatalf("path = %v, want %s", result["path"], dir)
	}
	if result["resources"].(int) != 2 {
		t.Fatalf("resources = %v, want 2", result["resources"])
	}
	output := result["output"].(string)
	if !strings.Contains(output, "ConfigMap") {
		t.Fatalf("output missing ConfigMap: %s", output)
	}
}

func TestHandleKustomizeApplyDryRun(t *testing.T) {
	setupFakeKustomize(t)
	server := newKustomizeTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})

	dir := createTestKustomization(t, "kustomization.yml")
	t.Setenv("FAKE_KUSTOMIZE_OUTPUT", "kind: Pod\n---\nkind: Service\n---\nkind: ConfigMap\n")

	args := mustMarshalJSON(t, map[string]interface{}{
		"path":    dir,
		"dry_run": true,
	})

	got, err := server.handleKustomizeApply(context.Background(), args)
	if err != nil {
		t.Fatalf("handleKustomizeApply() error = %v", err)
	}

	result := got.(map[string]interface{})
	if !result["dryRun"].(bool) {
		t.Fatalf("dryRun = %v, want true", result["dryRun"])
	}
	if result["totalClusters"].(int) != 2 {
		t.Fatalf("totalClusters = %v, want 2", result["totalClusters"])
	}
	if result["successCount"].(int) != 2 {
		t.Fatalf("successCount = %v, want 2", result["successCount"])
	}

	results := result["results"].([]KustomizeResult)
	if len(results) != 2 {
		t.Fatalf("results count = %d, want 2", len(results))
	}

	for _, r := range results {
		if r.Status != "would-apply" {
			t.Fatalf("unexpected result status: %#v", r)
		}
		if r.Resources != 3 {
			t.Fatalf("resources = %d, want 3", r.Resources)
		}
	}
}

func TestHandleKustomizeApplyWithSpecificClusters(t *testing.T) {
	setupFakeKustomize(t)
	server := newKustomizeTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
		"gamma": "https://gamma.example.com",
	})

	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_OUTPUT", "kind: Deployment\n")

	args := mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"alpha", "gamma"},
		"dry_run":  true,
	})

	got, err := server.handleKustomizeApply(context.Background(), args)
	if err != nil {
		t.Fatalf("handleKustomizeApply() error = %v", err)
	}

	result := got.(map[string]interface{})
	targetClusters := result["targetClusters"].([]string)
	if len(targetClusters) != 2 {
		t.Fatalf("targetClusters count = %d, want 2", len(targetClusters))
	}
	if targetClusters[0] != "alpha" || targetClusters[1] != "gamma" {
		t.Fatalf("targetClusters = %v, want [alpha gamma]", targetClusters)
	}
}

func TestHandleKustomizeDeleteDryRun(t *testing.T) {
	setupFakeKustomize(t)
	server := newKustomizeTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_OUTPUT", "kind: Deployment\n---\nkind: Service\n")

	args := mustMarshalJSON(t, map[string]interface{}{
		"path":    dir,
		"dry_run": true,
	})

	got, err := server.handleKustomizeDelete(context.Background(), args)
	if err != nil {
		t.Fatalf("handleKustomizeDelete() error = %v", err)
	}

	result := got.(map[string]interface{})
	if !result["dryRun"].(bool) {
		t.Fatalf("dryRun = %v, want true", result["dryRun"])
	}

	results := result["results"].([]KustomizeResult)
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].Status != "would-delete" {
		t.Fatalf("status = %q, want would-delete", results[0].Status)
	}
	if results[0].Resources != 2 {
		t.Fatalf("resources = %d, want 2", results[0].Resources)
	}
}

func TestHandleKustomizeDeleteValidation(t *testing.T) {
	server := newKustomizeTestServer(t, nil)

	args := mustMarshalJSON(t, map[string]interface{}{
		"path": "",
	})

	_, err := server.handleKustomizeDelete(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("handleKustomizeDelete() error = %v, want 'path is required'", err)
	}
}

func setupFakeKustomize(t *testing.T) {
	t.Helper()

	dir, err := os.MkdirTemp(".", "fake-kustomize-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	t.Setenv("PATH", absDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	script := `#!/bin/sh
cmd="$1"
shift
case "$cmd" in
  build)
    if [ "$FAKE_KUSTOMIZE_FAIL" = "1" ]; then
      echo "kustomize build failed" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUSTOMIZE_OUTPUT:-apiVersion: v1\nkind: ConfigMap\n}"
    ;;
  *)
    echo "unsupported kustomize command: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(absDir, "kustomize"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func newKustomizeTestServer(t *testing.T, contexts map[string]string) *Server {
	t.Helper()

	if contexts == nil {
		// Create minimal config for tests that don't need clusters
		config := clientcmdapi.NewConfig()
		config.Contexts["default"] = &clientcmdapi.Context{Cluster: "default", AuthInfo: "default"}
		config.Clusters["default"] = &clientcmdapi.Cluster{Server: "https://localhost:6443"}
		config.AuthInfos["default"] = &clientcmdapi.AuthInfo{}
		config.CurrentContext = "default"

		dir, err := os.MkdirTemp(".", "kustomize-kubeconfig-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error = %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(dir)
		})

		kubeconfig := filepath.Join(dir, "config")
		if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
			t.Fatalf("WriteToFile() error = %v", err)
		}

		manager, err := multicluster.NewClientManager(kubeconfig)
		if err != nil {
			t.Fatalf("NewClientManager() error = %v", err)
		}
		return &Server{manager: manager}
	}

	// Use newHelmTestServer for multi-cluster tests (same pattern)
	return newHelmTestServer(t, contexts)
}

func createTestKustomization(t *testing.T, filename string) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "kustomization-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	content := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`
	kustomizationPath := filepath.Join(dir, filename)
	if err := os.WriteFile(kustomizationPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	return absDir
}
