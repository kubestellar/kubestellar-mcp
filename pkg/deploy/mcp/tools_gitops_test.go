package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDetectDriftValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	tests := []struct {
		name    string
		args    []byte
		wantErr string
	}{
		{name: "invalid json", args: []byte(`{invalid`), wantErr: "invalid arguments"},
		{name: "missing repo", args: []byte(`{}`), wantErr: "repo is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.handleDetectDrift(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHandleDetectDriftReturnsNoManifestsMessage(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"README.md": "# demo\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{"repo": repo, "path": "."}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, "No manifests found in repository", result["message"])
	assert.Equal(t, gitops.ManifestSource{Repo: repo, Path: "."}, result["source"])
}

func TestHandleDetectDriftReturnsFailureForMissingClusterConfig(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"missing"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	assert.Equal(t, 1, result.ClusterCount)
	assert.Equal(t, 1, result.TotalDrifts)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "missing", result.Drifts[0].Cluster)
	assert.Equal(t, gitops.DriftTypeMissing, result.Drifts[0].DriftType)
	assert.Contains(t, result.Drifts[0].Differences[0], "Failed to get config")
}

func TestHandleSyncFromGitValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	tests := []struct {
		name    string
		args    []byte
		wantErr string
	}{
		{name: "invalid json", args: []byte(`{invalid`), wantErr: "invalid arguments"},
		{name: "missing repo", args: []byte(`{}`), wantErr: "repo is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.handleSyncFromGit(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHandleSyncFromGitReturnsNoManifestsMessage(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"notes.txt": "no yaml here\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{"repo": repo, "path": "."}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, "No manifests found in repository", result["message"])
	assert.Equal(t, gitops.ManifestSource{Repo: repo, Path: "."}, result["source"])
}

func TestHandleSyncFromGitReturnsFailedSummaryForMissingClusterConfig(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"dry_run":   true,
		"namespace": "apps",
		"include":   []string{"ConfigMap"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.True(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, "missing", result.Summaries[0].Cluster)
	assert.Equal(t, 1, result.Summaries[0].Failed)
	require.Len(t, result.Summaries[0].Results, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
	assert.Contains(t, result.Summaries[0].Results[0].Message, "Failed to get config")
}

func TestHandleReconcileDelegatesToSyncWithoutDryRun(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleReconcile(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"namespace": "apps",
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.False(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
}

func TestHandlePreviewChangesDelegatesToSyncWithDryRun(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handlePreviewChanges(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"namespace": "apps",
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.True(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
}

func setGitOpsTempDir(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp(".", "gitops-tmp-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	t.Setenv("TMPDIR", absDir)
}

func createGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "gitops-repo-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)

	for name, content := range files {
		path := filepath.Join(absDir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = absDir
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v failed: %s", args, string(output))
	}

	runGit("init", "-b", "main")
	runGit("config", "user.name", "Copilot Test")
	runGit("config", "user.email", "copilot@example.com")
	runGit("add", ".")
	if len(files) == 0 {
		runGit("commit", "--allow-empty", "-m", "test repo")
	} else {
		runGit("commit", "-m", "test repo")
	}

	return "file://" + absDir
}
