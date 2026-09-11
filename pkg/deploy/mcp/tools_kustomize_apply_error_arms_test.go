package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKustomizeApplyRejectsSensitiveKindInBuildOutput exercises the
// validateManifestDocs error arm of handleKustomizeApply — the guard that
// prevents cluster-mutating fan-out when kustomize output contains a
// blocked/sensitive kind (Secret, ClusterRoleBinding, ServiceAccount, etc.).
// Previously handleKustomizeApply's validateManifestDocs failure branch was
// uncovered; only the resolveKustomizePath, build, and no-clusters arms had
// tests.
func TestHandleKustomizeApplyRejectsSensitiveKindInBuildOutput(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	dir := createTestKustomization(t, "kustomization.yaml")

	// A Secret in the build output must be rejected before any cluster
	// receives the manifest.
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT",
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: leaked\n  namespace: default\ndata:\n  token: dGVzdA==\n")

	_, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     dir,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	// sensitiveKindError formats as "kind <k> is sensitive..."
	assert.Contains(t, err.Error(), "Secret")
}

// TestParseKustomizeBuildResultErrorArms unit-tests the three type-assertion
// error arms of parseKustomizeBuildResult — the defensive branches that guard
// against a future refactor of handleKustomizeBuild changing its return shape.
// These arms are unreachable through handleKustomizeApply as the code stands
// (handleKustomizeBuild always returns a map[string]interface{} with the
// right types), but they are the contract the parser advertises.
func TestParseKustomizeBuildResultErrorArms(t *testing.T) {
	t.Run("wrong top-level type", func(t *testing.T) {
		_, _, err := parseKustomizeBuildResult("not-a-map")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected kustomize build result type")
	})

	t.Run("wrong output field type", func(t *testing.T) {
		_, _, err := parseKustomizeBuildResult(map[string]interface{}{
			"output":    42,
			"resources": 1,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected kustomize build output type")
	})

	t.Run("wrong resources field type", func(t *testing.T) {
		_, _, err := parseKustomizeBuildResult(map[string]interface{}{
			"output":    "kind: ConfigMap\n",
			"resources": "not-an-int",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected kustomize build resources type")
	})
}
