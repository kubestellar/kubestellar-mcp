package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleDeployAppSummariesFailedClusterResult covers the "failed"
// summary arm of handleDeployApp at tools_deploy.go:157-162 —
// `if result.Error != "" { deployResults = append(..., Status: "failed", Message: result.Error) }`.
//
// Existing coverage exercises the success arm (TestHandleDeployAppDryRunAcrossClusters)
// and the pre-executor rejection arms (sensitive kinds, system namespaces,
// no-matching-GPU), but never the post-executor summarization branch that
// converts a ClusterResult.Error into a failed DeployResult.
//
// Reachable purely from the public handler: when targetClusters contains a
// cluster name that is not present in the kubeconfig, executor.executeAcrossClusters
// records ClusterResult{Cluster: name, Error: <GetClient error>}, and the
// summary loop must translate that into DeployResult{Status: "failed"}.
// One known cluster ("alpha") is included so the success append (line 163-166)
// stays covered by the same test — mixed success/failure is the realistic shape
// of a partial-multi-cluster deploy and pins the whole loop.
func TestHandleDeployAppSummariesFailedClusterResult(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
`

	got, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		// "ghost" is not in the kubeconfig; executor will surface
		// GetClient's "cluster not found" error via ClusterResult.Error,
		// which is the exact condition the failed-summary arm exists for.
		"clusters": []string{"alpha", "ghost"},
		"dry_run":  true,
	}))
	require.NoError(t, err, "handler must summarize per-cluster failures, not fail overall")

	result, ok := got.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, []string{"alpha", "ghost"}, result["targetClusters"])
	assert.Equal(t, 2, result["totalClusters"])
	// Only "alpha" succeeded; "ghost" surfaced as ClusterResult.Error and
	// went through the failed-summary arm (not the success arm), so
	// successCount stays at 1.
	assert.Equal(t, 1, result["successCount"])

	deployResults, ok := result["results"].([]DeployResult)
	require.True(t, ok)
	require.NotEmpty(t, deployResults)

	// Exactly one entry should carry Status == "failed" for ghost, with a
	// non-empty Message copied from ClusterResult.Error.
	var failed []DeployResult
	var succeeded []DeployResult
	for _, dr := range deployResults {
		switch dr.Status {
		case "failed":
			failed = append(failed, dr)
		case "would-apply":
			succeeded = append(succeeded, dr)
		}
	}

	require.Len(t, failed, 1, "expected exactly one failed DeployResult for the unknown cluster")
	assert.Equal(t, "ghost", failed[0].Cluster)
	assert.NotEmpty(t, failed[0].Message, "failed DeployResult must propagate ClusterResult.Error into Message")

	require.NotEmpty(t, succeeded, "expected the known cluster to still be summarized as success")
	for _, dr := range succeeded {
		assert.Equal(t, "alpha", dr.Cluster)
	}
}
