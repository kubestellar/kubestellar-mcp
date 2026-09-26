package kubectl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleKubectlApplyFailedClusterProducesFailedResultWithRealDeps(t *testing.T) {
	deps := newRealDeps(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test-cm\ndata:\n  key: value\n",
		"clusters": []string{"ghost"},
		"dry_run":  true,
	})

	result, err := HandleKubectlApply(context.Background(), deps, args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, resultMap["successCount"])
	assert.Equal(t, []string{"ghost"}, resultMap["targetClusters"])

	results, ok := resultMap["results"].([]ApplyResult)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "ghost", results[0].Cluster)
	assert.Equal(t, "failed", results[0].Status)
	assert.NotEmpty(t, results[0].Message)
}

func TestHandleKubectlApplyMixedResultsAggregationWithRealDeps(t *testing.T) {
	deps := newRealDeps(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test-cm\n",
		"clusters": []string{"alpha", "ghost"},
		"dry_run":  true,
	})

	result, err := HandleKubectlApply(context.Background(), deps, args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	results, ok := resultMap["results"].([]ApplyResult)
	require.True(t, ok)

	byCluster := map[string]ApplyResult{}
	for _, r := range results {
		byCluster[r.Cluster] = r
	}
	require.Contains(t, byCluster, "ghost")
	assert.Equal(t, "failed", byCluster["ghost"].Status)
	assert.Contains(t, byCluster, "alpha")
}

func TestHandleDeleteResourceReportsFailedClusterResultWithRealDeps(t *testing.T) {
	deps := newRealDeps(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"kind":     "ConfigMap",
		"name":     "demo",
		"clusters": []string{"ghost"},
		"dry_run":  true,
	})

	got, err := HandleDeleteResource(context.Background(), deps, args)
	require.NoError(t, err)

	m, ok := got.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, m["successCount"])
	assert.Equal(t, 1, m["totalClusters"])
	assert.Equal(t, true, m["dryRun"])
	assert.Equal(t, []string{"ghost"}, m["targetClusters"])

	results, ok := m["results"].([]DeleteResult)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "ghost", results[0].Cluster)
	assert.Equal(t, "ConfigMap", results[0].Resource)
	assert.Equal(t, "demo", results[0].Name)
	assert.Equal(t, "failed", results[0].Status)
	assert.NotEmpty(t, results[0].Message)
}
