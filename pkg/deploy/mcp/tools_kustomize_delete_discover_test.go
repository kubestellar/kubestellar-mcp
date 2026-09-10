package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKustomizeDeleteDiscoversClustersWhenNoneProvided covers the
// pkg/deploy/mcp/tools_kustomize.go handleKustomizeDelete discovery-fallback
// branch (lines 287-294): when the caller omits `clusters`, the handler calls
// s.manager.DiscoverClusters() and appends every discovered context to
// targetClusters. Existing delete-branch tests either pass an explicit
// `clusters` list (TestHandleKustomizeDeleteDryRunAcrossExplicitClusters)
// or use an empty kubeconfig so the append loop body never runs
// (TestHandleKustomizeDeleteReturnsNoClustersAvailable). This test populates
// two contexts in the kubeconfig and omits `clusters`, forcing the discover
// path AND executing the `for _, c := range clusters { ... append(...) }`
// loop body — previously the two uncovered blocks at tools_kustomize.go:292.4
// and 293.4 for this handler.
func TestHandleKustomizeDeleteDiscoversClustersWhenNoneProvided(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n")

	got, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":    dir,
		// no `clusters` → discovery fallback populates targetClusters
		"dry_run": true,
	}))
	require.NoError(t, err)

	result, ok := got.(map[string]interface{})
	require.True(t, ok)

	targets, ok := result["targetClusters"].([]string)
	require.True(t, ok, "expected []string targetClusters")
	assert.ElementsMatch(t, []string{"alpha", "beta"}, targets)
	assert.Equal(t, 2, result["totalClusters"])
	assert.True(t, result["dryRun"].(bool))
}
