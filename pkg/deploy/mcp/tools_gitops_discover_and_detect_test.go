package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleDetectDriftDiscoversClustersWhenNoneProvided exercises the
// `if len(targetClusters) == 0` DiscoverClusters branch on handleDetectDrift.
// When callers omit `clusters`, the handler falls back to whatever the
// manager reports; with a manager built from an empty kubeconfig, that fallback
// yields an empty target set and the result reflects ClusterCount == 0.
func TestHandleDetectDriftDiscoversClustersWhenNoneProvided(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo": repo,
		"path": "manifests",
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	assert.Equal(t, 0, result.ClusterCount)
	assert.Equal(t, 0, result.TotalDrifts)
	assert.Empty(t, result.Drifts)
}

// TestHandleSyncFromGitDiscoversClustersWhenNoneProvided is the sync-side twin
// of the drift discovery test — covers the same DiscoverClusters fallback on
// handleSyncFromGit.
func TestHandleSyncFromGitDiscoversClustersWhenNoneProvided(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":    repo,
		"path":    "manifests",
		"dry_run": true,
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.True(t, result.DryRun)
	assert.Empty(t, result.Summaries)
}

// TestHandleDetectDriftReportsErrorsForUnreachableCluster covers the drift
// detector's per-manifest error path: when GetConfig and NewDriftDetector both
// succeed but the API server is unreachable, DetectDrift returns synthetic
// error DriftResults rather than an outer error. The handler propagates those
// entries verbatim into the result.
func TestHandleDetectDriftReportsErrorsForUnreachableCluster(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	assert.Equal(t, 1, result.ClusterCount)
	require.GreaterOrEqual(t, result.TotalDrifts, 1)
	require.NotEmpty(t, result.Drifts)
	assert.Equal(t, "alpha", result.Drifts[0].Cluster)
}

