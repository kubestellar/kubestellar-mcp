package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleAddLabelsDiscoversAllClustersWhenNoTargetSpecified covers the
// pkg/deploy/mcp/tools_labels.go handleAddLabels branch at lines 60-68:
// when the caller supplies an empty `clusters` list, the handler falls
// back to s.manager.DiscoverClusters() and appends every discovered
// cluster name to targetClusters (the for-loop-append arm at
// tools_labels.go:64-66, which no other tools_labels test exercised).
//
// The pre-existing tools_labels tests all pass an explicit `clusters` list
// (see tools_labels_test.go / tools_labels_multicluster_test.go /
// tools_labels_all_kinds_test.go / tools_labels_failed_summary_test.go),
// so the DiscoverClusters + for-loop-append arm was never exercised.
// Same pattern as TestHandleDeployAppDiscoversAllClustersWhenNoTargetSpecified
// in tools_deploy_discover_clusters_test.go.
//
// dry_run=true keeps the test hermetic — no API-server calls are made,
// so both target clusters return LabelResult{Status: "would-label"}.
func TestHandleAddLabelsDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})

	got, err := server.handleAddLabels(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"kind":    "deployment",
		"name":    "demo",
		"labels":  map[string]string{"env": "prod"},
		"dry_run": true,
		// No `clusters` — force the DiscoverClusters fallback path.
	}))
	require.NoError(t, err)

	result, ok := got.(map[string]interface{})
	require.True(t, ok, "expected map[string]interface{} result")

	targets, ok := result["targetClusters"].([]string)
	require.True(t, ok, "expected []string targetClusters")
	assert.ElementsMatch(t, []string{"alpha", "beta"}, targets)
	assert.Equal(t, 2, result["totalClusters"])
	assert.Equal(t, 2, result["successCount"], "both dry-run clusters should count as success (would-label)")
	assert.True(t, result["dryRun"].(bool))
}

// TestHandleRemoveLabelsDiscoversAllClustersWhenNoTargetSpecified covers
// the symmetric branch in handleRemoveLabels at lines 211-220 (the
// DiscoverClusters + for-loop-append arm at tools_labels.go:215-217,
// which no other tools_labels test exercised).
//
// dry_run=true keeps the test hermetic — no API-server calls are made,
// so both target clusters return LabelResult{Status: "would-unlabel"}.
func TestHandleRemoveLabelsDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})

	got, err := server.handleRemoveLabels(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"kind":    "deployment",
		"name":    "demo",
		"labels":  []string{"env"},
		"dry_run": true,
		// No `clusters` — force the DiscoverClusters fallback path.
	}))
	require.NoError(t, err)

	result, ok := got.(map[string]interface{})
	require.True(t, ok, "expected map[string]interface{} result")

	targets, ok := result["targetClusters"].([]string)
	require.True(t, ok, "expected []string targetClusters")
	assert.ElementsMatch(t, []string{"alpha", "beta"}, targets)
	assert.Equal(t, 2, result["totalClusters"])
	assert.Equal(t, 2, result["successCount"], "both dry-run clusters should count as success (would-unlabel)")
	assert.True(t, result["dryRun"].(bool))
}
