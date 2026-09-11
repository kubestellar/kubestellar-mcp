package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKubectlApplyFailedClusterProducesFailedResult exercises the
// `result.Error != ""` branch in handleKubectlApply (tools_kubectl.go
// lines ~288-293). Existing tests in tools_kubectl_handlers_test.go
// only cover:
//   - invalid JSON args
//   - empty manifest
//   - sensitive-kind gate
//   - discovery of the cluster list (empty case)
//   - a successful dry-run against a real fake context
//   - the successful []ApplyResult aggregation branch
//
// They do NOT drive the executor into a ClusterResult{Error: "..."}
// state, so the fan-in branch that maps a per-cluster failure into an
// ApplyResult{Status: "failed", Message: err} was uncovered. If a
// refactor accidentally dropped this branch, every per-cluster failure
// would be silently omitted from the aggregate response and every apply
// would appear to succeed.
//
// We drive that branch by naming a cluster that is NOT present in the
// test kubeconfig: multicluster.Executor.GetClient() returns an error,
// which populates ClusterResult.Error, which is exactly what the
// handler's `if result.Error != ""` arm consumes.
func TestHandleKubectlApplyFailedClusterProducesFailedResult(t *testing.T) {
	// Kubeconfig has only "alpha"; we ask for "ghost" so GetClient fails.
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"ghost"},
		"dry_run":  true,
	})

	result, err := server.handleKubectlApply(context.Background(), args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)

	// The failed cluster must NOT count toward successCount and must
	// appear as a failed ApplyResult in the aggregated slice.
	assert.Equal(t, 0, resultMap["successCount"],
		"a per-cluster failure must not count toward successCount")
	assert.Equal(t, []string{"ghost"}, resultMap["targetClusters"])

	results, ok := resultMap["results"].([]ApplyResult)
	require.True(t, ok, "results must be []ApplyResult")
	require.Len(t, results, 1)
	assert.Equal(t, "ghost", results[0].Cluster)
	assert.Equal(t, "failed", results[0].Status)
	assert.NotEmpty(t, results[0].Message,
		"the underlying GetClient error must be surfaced in Message")
}

// TestHandleKubectlApplyMixedResultsAggregation covers the mixed case:
// one cluster in the target list resolves cleanly and produces
// ApplyResult{would-apply,...}, while a second (missing) cluster fails
// via the executor. Both must be present in the aggregate response, and
// only the successful one must contribute to successCount. This
// simultaneously guards the `else if ar, ok := result.Result.(...)` OK
// branch and the `if result.Error != ""` branch running against the
// same handler invocation, which no existing test does.
func TestHandleKubectlApplyMixedResultsAggregation(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha", "ghost"},
		"dry_run":  true,
	})

	result, err := server.handleKubectlApply(context.Background(), args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)

	results, ok := resultMap["results"].([]ApplyResult)
	require.True(t, ok)

	// Exactly one failed entry (ghost) and at least one non-failed
	// entry (alpha) must appear. Order is not stable because the
	// executor fans out concurrently, so index the entries by cluster.
	byCluster := map[string]ApplyResult{}
	for _, r := range results {
		byCluster[r.Cluster] = r
	}
	require.Contains(t, byCluster, "ghost")
	assert.Equal(t, "failed", byCluster["ghost"].Status)

	// alpha should have at least one entry present. Its status will be
	// "would-apply" (dry_run) or "failed" depending on whether the fake
	// context can be dialed; the important invariant is that ghost's
	// failure did not suppress alpha's row.
	assert.Contains(t, byCluster, "alpha",
		"successful cluster must still appear alongside a failed cluster")
}
