package kustomize

import (
	"context"
	"fmt"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKustomizeBuildRejectsInvalidJSON covers the malformed-arguments
// arm of handleKustomizeBuild.
func TestHandleKustomizeBuildRejectsInvalidJSON(t *testing.T) {
	deps := newTestDeps(t, map[string]string{})

	_, err := deps.handleKustomizeBuild(context.Background(), mustMarshalJSON(t, "{invalid"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestHandleKustomizeApplyWrapsBuildFailure covers the build-failure wrap arm
// of handleKustomizeApply: the path resolves, but no kustomization.yaml/.yml
// is present, so the internal handleKustomizeBuild call fails and the error
// is wrapped with "kustomize build failed".
func TestHandleKustomizeApplyWrapsBuildFailure(t *testing.T) {
	deps := newTestDeps(t, map[string]string{})
	dir := t.TempDir()

	_, err := deps.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kustomize build failed")
}

// TestHandleKustomizeDeleteWrapsBuildFailure is the handleKustomizeDelete
// analog of TestHandleKustomizeApplyWrapsBuildFailure.
func TestHandleKustomizeDeleteWrapsBuildFailure(t *testing.T) {
	deps := newTestDeps(t, map[string]string{})
	dir := t.TempDir()

	_, err := deps.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kustomize build failed")
}

// TestHandleKustomizeApplyDiscoverClustersErrorPropagates covers the
// DiscoverClusters error arm reached when no clusters are explicitly
// requested.
func TestHandleKustomizeApplyDiscoverClustersErrorPropagates(t *testing.T) {
	setupFakeKustomize(t)
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")

	deps := Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			return nil, fmt.Errorf("discovery unavailable")
		},
		ValidateClusters: fakeValidateClusters,
		ValidateManifest: fakeValidateManifest,
	}

	_, err := deps.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery unavailable")
}

// TestHandleKustomizeDeleteDiscoverClustersErrorPropagates is the
// handleKustomizeDelete analog of
// TestHandleKustomizeApplyDiscoverClustersErrorPropagates.
func TestHandleKustomizeDeleteDiscoverClustersErrorPropagates(t *testing.T) {
	setupFakeKustomize(t)
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")

	deps := Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			return nil, fmt.Errorf("discovery unavailable")
		},
		ValidateClusters: fakeValidateClusters,
		ValidateManifest: fakeValidateManifest,
	}

	_, err := deps.handleKustomizeDelete(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery unavailable")
}

// TestHandleKustomizeApplyDiscoversAllClustersWhenNoTargetSpecified covers
// the successful-discovery loop that appends discovered cluster names into
// targetClusters.
func TestHandleKustomizeApplyDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	setupFakeKustomize(t)
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")

	deps := Deps{
		DiscoverClusters: func() ([]multicluster.ClusterInfo, error) {
			return []multicluster.ClusterInfo{{Name: "alpha"}, {Name: "beta"}}, nil
		},
		ValidateClusters: fakeValidateClusters,
		ValidateManifest: fakeValidateManifest,
	}

	got, err := deps.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{"path": dir, "dry_run": true}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, 2, result["totalClusters"])
}

// TestDeleteKustomizeFailedArm exercises the `kubectl delete` non-zero-exit
// arm of deleteKustomize — the branch that sets result.Status = "failed" and
// copies stderr into result.Message.
func TestDeleteKustomizeFailedArm(t *testing.T) {
	setupFakeKustomize(t)
	deps := newTestDeps(t, map[string]string{"alpha": "https://alpha.example.com"})

	t.Setenv("FAKE_KUBECTL_DELETE_FAIL", "1")
	t.Setenv("FAKE_KUBECTL_DELETE_STDERR", "error: the server rejected the delete")

	result := deps.deleteKustomize(
		context.Background(),
		"alpha",
		"/workdir/demo",
		"kind: ConfigMap\nmetadata:\n  name: demo\n",
		1,
		false,
	)

	assert.Equal(t, "alpha", result.Cluster)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Message, "error: the server rejected the delete")
}
