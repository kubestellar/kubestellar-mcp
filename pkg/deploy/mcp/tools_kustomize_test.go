package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleKustomizeBuildValidatesPath(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{name: "missing path", args: map[string]interface{}{}, wantErr: "path is required"},
		{name: "missing kustomization file", args: map[string]interface{}{"path": t.TempDir()}, wantErr: "no kustomization.yaml or kustomization.yml found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.handleKustomizeBuild(context.Background(), mustMarshalJSON(t, tt.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHandleKustomizeBuildRejectsResolvedPathOutsideAllowedDirectories(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	outsideDir, err := os.MkdirTemp(filepath.Dir(workingDir), "outside-kustomization-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outsideDir) })

	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "kustomization.yaml"), []byte("resources: []\n"), 0o644))

	linkPath := filepath.Join(workingDir, "outside-kustomization-link")
	require.NoError(t, os.Symlink(outsideDir, linkPath))
	t.Cleanup(func() { _ = os.Remove(linkPath) })

	_, err = server.handleKustomizeBuild(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": linkPath}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside allowed directories")
}

func TestParseKustomizeBuildResultValidatesTypes(t *testing.T) {
	_, _, err := parseKustomizeBuildResult("bad-result")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected kustomize build result type")

	_, _, err = parseKustomizeBuildResult(map[string]interface{}{"output": 1, "resources": 2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected kustomize build output type")
}

func TestHandleKustomizeBuildCountsResources(t *testing.T) {
	logFile := setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n")

	got, err := server.handleKustomizeBuild(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, dir, result["path"])
	assert.Equal(t, 2, result["resources"])
	assert.Contains(t, result["output"].(string), "Deployment")
	assert.Contains(t, readLogFile(t, logFile), "args=build "+dir)
}

func TestHandleKustomizeBuildFallsBackToKubectlKustomize(t *testing.T) {
	logFile := setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_FAIL", "1")
	t.Setenv("FAKE_KUBECTL_KUSTOMIZE_STDOUT", "apiVersion: v1\nkind: Service\nmetadata:\n  name: demo\n")

	got, err := server.handleKustomizeBuild(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, 1, result["resources"])
	assert.Contains(t, result["output"].(string), "Service")
	logData := readLogFile(t, logFile)
	assert.Contains(t, logData, "bin=kustomize")
	assert.Contains(t, logData, "bin=kubectl")
	assert.Contains(t, logData, "args=kustomize "+dir)
}

func TestHandleKustomizeApplyRejectsInvalidClusterNames(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n")

	for _, badCluster := range []string{"--server=http://evil.example.com", "-x", "--token=leaked"} {
		_, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
			"path":     dir,
			"clusters": []string{badCluster},
			"dry_run":  true,
		}))
		require.Error(t, err, "expected rejection of cluster %q", badCluster)
		assert.Contains(t, err.Error(), "flag injection", "cluster %q should be rejected as flag injection", badCluster)
	}
}

func TestHandleKustomizeDeleteRejectsInvalidClusterNames(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: Deployment\n")

	for _, badCluster := range []string{"--server=http://evil.example.com", "-x", "--kubeconfig=/etc/passwd"} {
		_, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
			"path":     dir,
			"clusters": []string{badCluster},
			"dry_run":  true,
		}))
		require.Error(t, err, "expected rejection of cluster %q", badCluster)
		assert.Contains(t, err.Error(), "flag injection", "cluster %q should be rejected as flag injection", badCluster)
	}
}

func TestApplyKustomizeDryRunReturnsWouldApply(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	result := server.applyKustomize(context.Background(), "alpha", "/workdir/demo", "kind: ConfigMap\n", 3, true)

	assert.Equal(t, "alpha", result.Cluster)
	assert.Equal(t, "would-apply", result.Status)
	assert.Equal(t, 3, result.Resources)
	assert.Equal(t, "Would apply 3 resources from /workdir/demo", result.Message)
}

func TestHandleKustomizeApplyDryRunAcrossExplicitClusters(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com", "beta": "https://beta.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n---\nkind: Service\n")

	got, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"beta", "alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, []string{"beta", "alpha"}, result["targetClusters"])
	assert.Equal(t, 2, result["successCount"])
	assert.Equal(t, 2, result["totalClusters"])
	assert.True(t, result["dryRun"].(bool))

	results := result["results"].([]KustomizeResult)
	require.Len(t, results, 2)
	for _, item := range results {
		assert.Equal(t, "would-apply", item.Status)
		assert.Equal(t, 2, item.Resources)
	}
}

func TestHandleKustomizeApplyReturnsNoClustersAvailable(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n")

	_, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir, "dry_run": true}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters available")
}

func TestHandleKustomizeApplyRunsKubectlApply(t *testing.T) {
	logFile := setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")
	t.Setenv("FAKE_KUBECTL_APPLY_STDOUT", "configmap/demo created")

	got, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir, "clusters": []string{"alpha"}}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	results := result["results"].([]KustomizeResult)
	require.Len(t, results, 1)
	assert.Equal(t, "applied", results[0].Status)
	assert.Contains(t, results[0].Message, "configmap/demo created")

	logData := readLogFile(t, logFile)
	assert.Contains(t, logData, "bin=kubectl")
	assert.Contains(t, logData, "cluster=alpha")
	assert.Contains(t, logData, "args=apply -f - --context alpha")
	assert.Contains(t, logData, "stdin=kind: ConfigMap")
}

func TestDeleteKustomizeDryRunReturnsWouldDelete(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	result := server.deleteKustomize(context.Background(), "beta", "/workdir/demo", "kind: Service\n", 2, true)

	assert.Equal(t, "beta", result.Cluster)
	assert.Equal(t, "would-delete", result.Status)
	assert.Equal(t, 2, result.Resources)
	assert.Equal(t, "Would delete 2 resources from /workdir/demo", result.Message)
}

func TestHandleKustomizeDeleteRunsKubectlDelete(t *testing.T) {
	logFile := setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: Deployment\nmetadata:\n  name: demo\n")
	t.Setenv("FAKE_KUBECTL_DELETE_STDOUT", "deployment.apps/demo deleted")

	got, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir, "clusters": []string{"alpha"}}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	results := result["results"].([]KustomizeResult)
	require.Len(t, results, 1)
	assert.Equal(t, "deleted", results[0].Status)
	assert.Contains(t, results[0].Message, "deployment.apps/demo deleted")

	logData := readLogFile(t, logFile)
	assert.Contains(t, logData, "args=delete -f - --context alpha --ignore-not-found")
	assert.Contains(t, logData, "stdin=kind: Deployment")
}

func setupFakeKustomize(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "fake-kustomize-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)

	logFile := filepath.Join(absDir, "kustomize.log")
	t.Setenv("FAKE_KUSTOMIZE_LOG", logFile)
	t.Setenv("PATH", absDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	kustomizeScript := `#!/bin/sh
cmd="$1"
shift
if [ -n "$FAKE_KUSTOMIZE_LOG" ]; then
  {
    echo "---"
    echo "bin=kustomize"
    echo "args=$cmd $*"
  } >> "$FAKE_KUSTOMIZE_LOG"
fi
case "$cmd" in
  build)
    if [ "$FAKE_KUSTOMIZE_BUILD_FAIL" = "1" ]; then
      echo "${FAKE_KUSTOMIZE_BUILD_STDERR:-kustomize build failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUSTOMIZE_BUILD_STDOUT:-kind: ConfigMap\n}"
    ;;
  *)
    echo "unsupported kustomize command: $cmd" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(absDir, "kustomize"), []byte(kustomizeScript), 0o755))

	kubectlScript := `#!/bin/sh
cmd="$1"
shift
stdin="$(cat)"
cluster=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--context" ] || [ "$prev" = "--kube-context" ]; then
    cluster="$arg"
  fi
  prev="$arg"
done
if [ -n "$FAKE_KUSTOMIZE_LOG" ]; then
  {
    echo "---"
    echo "bin=kubectl"
    echo "cmd=$cmd"
    echo "cluster=$cluster"
    echo "args=$cmd $*"
    printf 'stdin=%s\n' "$stdin"
  } >> "$FAKE_KUSTOMIZE_LOG"
fi
case "$cmd" in
  kustomize)
    printf '%s' "${FAKE_KUBECTL_KUSTOMIZE_STDOUT:-kind: ConfigMap\n}"
    ;;
  apply)
    if [ "$FAKE_KUBECTL_APPLY_FAIL" = "1" ]; then
      echo "${FAKE_KUBECTL_APPLY_STDERR:-kubectl apply failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUBECTL_APPLY_STDOUT:-applied}"
    ;;
  delete)
    if [ "$FAKE_KUBECTL_DELETE_FAIL" = "1" ]; then
      echo "${FAKE_KUBECTL_DELETE_STDERR:-kubectl delete failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUBECTL_DELETE_STDOUT:-deleted}"
    ;;
  *)
    echo "unsupported kubectl command: $cmd" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(absDir, "kubectl"), []byte(kubectlScript), 0o755))

	return logFile
}

func createTestKustomization(t *testing.T, filename string) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "kustomization-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	content := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	return absDir
}
