package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers the per-cluster failure branch in handleDeleteResource
// (tools_kubectl.go:141-148 — `if result.Error != ""` arm):
// when a caller names a cluster that is not in the ClientManager,
// ExecuteOnSelected populates ClusterResult.Error via the GetClient
// failure path, and handleDeleteResource must translate that into
// a DeleteResult{Status: "failed"} entry rather than skipping it or
// counting it as a success.
//
// This mirrors TestHandleKubectlApplyReportsFailedClusterResult
// (kubestellar-mcp#814) but for the delete handler.
func TestHandleDeleteResourceReportsFailedClusterResult(t *testing.T) {
	// Kubeconfig has only "alpha"; caller asks about "ghost" → GetClient
	// fails inside executeAcrossClusters and the ClusterResult.Error path
	// is exercised.
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"kind":     "ConfigMap",
		"name":     "demo",
		"clusters": []string{"ghost"},
		"dry_run":  true,
	})

	got, err := server.handleDeleteResource(context.Background(), args)
	require.NoError(t, err)

	m, ok := got.(map[string]interface{})
	require.True(t, ok, "expected result map, got %T", got)

	assert.Equal(t, 0, m["successCount"], "ghost cluster must not count toward successes")
	assert.Equal(t, 1, m["totalClusters"])
	assert.Equal(t, true, m["dryRun"])

	targets, ok := m["targetClusters"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"ghost"}, targets)

	results, ok := m["results"].([]DeleteResult)
	require.True(t, ok, "expected []DeleteResult, got %T", m["results"])
	require.Len(t, results, 1, "one failed DeleteResult should be emitted")

	dr := results[0]
	assert.Equal(t, "ghost", dr.Cluster)
	assert.Equal(t, "ConfigMap", dr.Resource)
	assert.Equal(t, "demo", dr.Name)
	assert.Equal(t, "failed", dr.Status)
	assert.NotEmpty(t, dr.Message, "failure message must be propagated from ClusterResult.Error")
}
