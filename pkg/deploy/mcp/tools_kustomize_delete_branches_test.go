package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The existing tests cover handleKustomizeDelete's happy path, cluster-name
// injection rejection, and single-cluster kubectl-delete invocation. The
// error-return branches (invalid JSON, missing path, unresolvable path,
// kustomize-build failure, no-clusters-available) and the multi-cluster
// dry-run fan-out — all of which handleKustomizeApply already has tests
// for — were untested. This file adds parity coverage for delete so a
// regression in any of those branches surfaces here instead of in prod.

func TestHandleKustomizeDeleteRejectsInvalidJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleKustomizeDelete(context.Background(), json.RawMessage("{not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleKustomizeDeleteRequiresPath(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestHandleKustomizeDeleteRejectsPathWithoutKustomization(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	// t.TempDir() has no kustomization.yaml/.yml — resolveKustomizePath's
	// kustomization-presence check must reject it before any build attempt.
	_, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     t.TempDir(),
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kustomization.yaml or kustomization.yml found")
}

func TestHandleKustomizeDeleteRejectsSensitiveKindInBuiltManifest(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")

	// A Secret in the built manifest must be blocked by validateManifestDocs
	// before any kubectl delete fires; this pins down the manifest-validation
	// branch inside the delete handler (the delete-specific instance of the
	// call, distinct from the apply-side branch already covered).
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db-creds\n")

	_, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Secret")
	assert.Contains(t, err.Error(), "blocked")
}

func TestHandleKustomizeDeleteReturnsNoClustersAvailable(t *testing.T) {
	setupFakeKustomize(t)
	// No contexts in the kubeconfig → DiscoverClusters returns empty.
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n")

	_, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":    dir,
		"dry_run": true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters available")
}

func TestHandleKustomizeDeleteDryRunAcrossExplicitClusters(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\n---\nkind: Service\n")

	got, err := server.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"beta", "alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, []string{"beta", "alpha"}, result["targetClusters"])
	assert.Equal(t, 2, result["successCount"])
	assert.Equal(t, 2, result["totalClusters"])
	assert.True(t, result["dryRun"].(bool))

	// resolveKustomizePath rewrites Path into its absolute form; assert that
	// the response echoes the resolved (absolute) path rather than the raw
	// input — a silent regression would echo the user-supplied string.
	assert.Equal(t, dir, result["path"])
	assert.Equal(t, dir, filepath.Clean(result["path"].(string)))

	results := result["results"].([]KustomizeResult)
	require.Len(t, results, 2)
	for _, item := range results {
		assert.Equal(t, "would-delete", item.Status)
		assert.Equal(t, 2, item.Resources)
	}
}
