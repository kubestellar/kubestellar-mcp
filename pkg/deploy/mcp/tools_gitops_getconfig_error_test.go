package mcp

import (
	"context"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleDetectDriftReportsGetConfigError covers the outer GetConfig error
// branch on handleDetectDrift (tools_gitops.go: `config, err := s.manager.GetConfig(cluster)`).
// When a caller names a cluster that is not present in the ClientManager's
// kubeconfig, GetConfig returns an error before any drift work happens, and
// the handler emits a synthetic DriftResult with DriftType=Missing citing
// "Failed to get config".
func TestHandleDetectDriftReportsGetConfigError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	// Kubeconfig has only "alpha"; caller asks about "ghost" → GetConfig fails.
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"ghost"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	assert.Equal(t, 1, result.ClusterCount)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "ghost", result.Drifts[0].Cluster)
	assert.Equal(t, gitops.DriftTypeMissing, result.Drifts[0].DriftType)
	require.NotEmpty(t, result.Drifts[0].Differences)
	assert.Contains(t, result.Drifts[0].Differences[0], "Failed to get config")
}

// TestHandleSyncFromGitReportsGetConfigError is the sync-side twin of the
// drift GetConfig-error test — verifies that handleSyncFromGit emits a
// SyncSummary with Failed=1 and a "Failed to get config" message when
// s.manager.GetConfig returns an error for the requested cluster.
func TestHandleSyncFromGitReportsGetConfigError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"ghost"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	require.Len(t, result.Summaries, 1)
	assert.Equal(t, "ghost", result.Summaries[0].Cluster)
	assert.Equal(t, 1, result.Summaries[0].Failed)
	require.Len(t, result.Summaries[0].Results, 1)
	assert.Equal(t, gitops.SyncActionFailed, result.Summaries[0].Results[0].Action)
	assert.Contains(t, result.Summaries[0].Results[0].Message, "Failed to get config")
}

// TestHandleDetectDriftDiscoveryPopulatesTargets covers the discovery-fallback
// range loop on handleDetectDrift (`for _, c := range clusters { targetClusters = append(...) }`).
// The peer discovery test uses an empty kubeconfig so the loop body never runs;
// this test populates the kubeconfig so DiscoverClusters returns real entries
// and the handler processes them as targets. The unreachable server URLs cause
// DetectDrift to emit synthetic per-cluster error DriftResults — which is fine;
// what we're locking is that the discovered cluster names actually flow into
// the drift result.
func TestHandleDetectDriftDiscoveryPopulatesTargets(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
		"beta":  "https://127.0.0.1:2",
	})

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo": repo,
		"path": "manifests",
		// no `clusters` → discovery fallback populates targetClusters
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	assert.Equal(t, 2, result.ClusterCount)
	// Both discovered clusters must appear in the drift results.
	seen := map[string]bool{}
	for _, d := range result.Drifts {
		seen[d.Cluster] = true
	}
	assert.True(t, seen["alpha"], "expected discovered cluster 'alpha' in drift results")
	assert.True(t, seen["beta"], "expected discovered cluster 'beta' in drift results")
}

// TestHandleSyncFromGitDiscoveryPopulatesTargets is the sync-side twin: covers
// the same discovery-fallback range loop on handleSyncFromGit and verifies
// discovered cluster names appear as SyncSummary entries.
func TestHandleSyncFromGitDiscoveryPopulatesTargets(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
		"beta":  "https://127.0.0.1:2",
	})

	got, err := server.handleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":    repo,
		"path":    "manifests",
		"dry_run": true,
		// no `clusters` → discovery fallback populates targetClusters
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	require.Len(t, result.Summaries, 2)
	seen := map[string]bool{}
	for _, s := range result.Summaries {
		seen[s.Cluster] = true
	}
	assert.True(t, seen["alpha"], "expected discovered cluster 'alpha' in sync summaries")
	assert.True(t, seen["beta"], "expected discovered cluster 'beta' in sync summaries")
}
