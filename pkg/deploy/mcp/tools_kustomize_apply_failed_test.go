package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestApplyKustomizeFailedArm exercises the `kubectl apply` non-zero-exit arm
// of applyKustomize — the branch that sets result.Status = "failed" and copies
// stderr into result.Message. Previously only the dry-run and success arms
// were covered (applyKustomize sat at 92.9%). The failure arm is what surfaces
// per-cluster kubectl-apply errors to callers of handleKustomizeApply, so it
// is the critical decision point for multi-cluster fan-out reporting.
func TestApplyKustomizeFailedArm(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	t.Setenv("FAKE_KUBECTL_APPLY_FAIL", "1")
	t.Setenv("FAKE_KUBECTL_APPLY_STDERR", "error: the server rejected the manifest")

	result := server.applyKustomize(
		context.Background(),
		"alpha",
		"/workdir/demo",
		"kind: ConfigMap\nmetadata:\n  name: demo\n",
		1,
		false,
	)

	assert.Equal(t, "alpha", result.Cluster)
	assert.Equal(t, "/workdir/demo", result.Path)
	assert.Equal(t, 1, result.Resources)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Message, "error: the server rejected the manifest")
}

// TestHandleKustomizeApplyReportsFailedPerCluster verifies the failed-arm
// signal propagates through handleKustomizeApply into the aggregated results
// slice: a failed cluster contributes 0 to successCount, and its per-cluster
// KustomizeResult carries Status="failed" and the stderr message.
func TestHandleKustomizeApplyReportsFailedPerCluster(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")

	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")
	t.Setenv("FAKE_KUBECTL_APPLY_FAIL", "1")
	t.Setenv("FAKE_KUBECTL_APPLY_STDERR", "namespaces \"missing\" not found")

	got, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"alpha"},
	}))
	assert.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, 0, result["successCount"])
	assert.Equal(t, 1, result["totalClusters"])

	results := result["results"].([]KustomizeResult)
	assert.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "namespaces \"missing\" not found")
}
