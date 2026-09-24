package gitops

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	upstreamgitops "github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDetectDriftValidatesArguments(t *testing.T) {
	server := newTestServer(t, map[string]string{})

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
			_, err := server.HandleDetectDrift(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHandleDetectDriftReturnsNoManifestsMessage(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"README.md": "# demo\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{"repo": repo, "path": "."}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, "No manifests found in repository", result["message"])
	assert.Equal(t, upstreamgitops.ManifestSource{Repo: repo, Path: "."}, result["source"])
}

func TestHandleDetectDriftReturnsFailureForMissingClusterConfig(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"missing"},
	}))
	require.NoError(t, err)

	result := got.(*DriftResult)
	assert.Equal(t, 1, result.ClusterCount)
	assert.Equal(t, 1, result.TotalDrifts)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "missing", result.Drifts[0].Cluster)
	assert.Equal(t, upstreamgitops.DriftTypeMissing, result.Drifts[0].DriftType)
	assert.Contains(t, result.Drifts[0].Differences[0], "Failed to get config")
}

func TestHandleSyncFromGitValidatesArguments(t *testing.T) {
	server := newTestServer(t, map[string]string{})

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
			_, err := server.HandleSyncFromGit(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestHandleSyncFromGitReturnsNoManifestsMessage(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"notes.txt": "no yaml here\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{"repo": repo, "path": "."}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, "No manifests found in repository", result["message"])
	assert.Equal(t, upstreamgitops.ManifestSource{Repo: repo, Path: "."}, result["source"])
}

func TestHandleSyncFromGitReturnsFailedSummaryForMissingClusterConfig(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"dry_run":   true,
		"namespace": "apps",
		"include":   []string{"ConfigMap"},
	}))
	require.NoError(t, err)

	result := got.(*SyncResult)
	assert.True(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, "missing", result.Summaries[0].Cluster)
	assert.Equal(t, 1, result.Summaries[0].Failed)
	require.Len(t, result.Summaries[0].Results, 1)
	assert.Equal(t, upstreamgitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
	assert.Contains(t, result.Summaries[0].Results[0].Message, "Failed to get config")
}

func TestHandleReconcileDelegatesToSyncWithoutDryRun(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandleReconcile(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"namespace": "apps",
	}))
	require.NoError(t, err)

	result := got.(*SyncResult)
	assert.False(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, upstreamgitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
}

func TestRunGitOpsClusterTasksBoundsConcurrency(t *testing.T) {
	clusters := make([]string, 0, maxConcurrentClusters+5)
	for i := range maxConcurrentClusters + 5 {
		clusters = append(clusters, fmt.Sprintf("cluster-%d", i))
	}

	var running atomic.Int32
	var maxRunning atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, len(clusters))
	done := make(chan struct{})

	go func() {
		defer close(done)
		runClusterTasks(clusters, func(cluster string) {
			current := running.Add(1)
			defer running.Add(-1)
			for {
				observed := maxRunning.Load()
				if current <= observed || maxRunning.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
		})
	}()

	for range maxConcurrentClusters {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for gitops workers to start")
		}
	}

	select {
	case <-started:
		t.Fatal("started more gitops workers than the concurrency limit allows")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gitops tasks to finish")
	}

	assert.LessOrEqual(t, maxRunning.Load(), int32(maxConcurrentClusters))
}

func TestHandlePreviewChangesDelegatesToSyncWithDryRun(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newTestServer(t, map[string]string{})

	got, err := server.HandlePreviewChanges(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":      repo,
		"path":      "manifests",
		"clusters":  []string{"missing"},
		"namespace": "apps",
	}))
	require.NoError(t, err)

	result := got.(*SyncResult)
	assert.True(t, result.DryRun)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, upstreamgitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
}
