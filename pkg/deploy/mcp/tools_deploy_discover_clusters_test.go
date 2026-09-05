package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleDeployAppDiscoversAllClustersWhenNoTargetSpecified covers the
// pkg/deploy/mcp/tools_deploy.go handleDeployApp branch at lines 130-137:
// when the caller supplies neither `clusters` nor a GPU requirement, the
// handler falls back to s.manager.DiscoverClusters() and appends every
// discovered cluster name to targetClusters.
//
// The existing TestHandleDeployAppDryRunAcrossClusters always passes an
// explicit `clusters` list, so the DiscoverClusters + for-loop-append arm
// (previously 0% covered blocks tools_deploy.go:132.5-133.1 and
// tools_deploy.go:135.5-136.1) was never exercised. Baseline for
// handleDeployApp was 87.2%.
func TestHandleDeployAppDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
`

	got, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		// No `clusters`, no `gpu_type`, no `min_gpu` — force the
		// DiscoverClusters fallback path.
		"dry_run": true,
	}))
	require.NoError(t, err)

	result, ok := got.(map[string]interface{})
	require.True(t, ok)

	targets, ok := result["targetClusters"].([]string)
	require.True(t, ok, "expected []string targetClusters")
	// Both kubeconfig contexts should have been discovered and appended.
	assert.ElementsMatch(t, []string{"alpha", "beta"}, targets)
	assert.Equal(t, 2, result["totalClusters"])
	assert.True(t, result["dryRun"].(bool))
}
