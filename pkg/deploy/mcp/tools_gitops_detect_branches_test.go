package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// These tests cover the per-cluster drift-detection body inside
// handleDetectDrift's runGitOpsClusterTasks closure (tools_gitops.go) now
// that it routes through the s.getDriftDetector factory instead of calling
// gitops.NewDriftDetector directly. Because the factory is injectable, we can
// exercise:
//   - the detector-construction error branch ("Failed to create detector")
//   - the detector.DetectDrift error branch ("Failed to detect drift")
//   - the success branch (drifts appended verbatim to result.Drifts)
//
// without talking to a real API server.

// stubDriftDetector is a fully-controllable driftDetector test double.
type stubDriftDetector struct {
	drifts []gitops.DriftResult
	err    error
}

func (d *stubDriftDetector) DetectDrift(_ context.Context, _ []gitops.Manifest, _ string) ([]gitops.DriftResult, error) {
	return d.drifts, d.err
}

func TestHandleDetectDriftReportsDetectorFactoryError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	factoryErr := errors.New("factory boom")
	server.newDriftDetector = func(*rest.Config) (driftDetector, error) {
		return nil, factoryErr
	}

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "alpha", result.Drifts[0].Cluster)
	assert.Equal(t, gitops.DriftTypeMissing, result.Drifts[0].DriftType)
	require.Len(t, result.Drifts[0].Differences, 1)
	assert.Contains(t, result.Drifts[0].Differences[0], "Failed to create detector")
	assert.Contains(t, result.Drifts[0].Differences[0], factoryErr.Error())
}

func TestHandleDetectDriftReportsDetectDriftError(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	detectErr := errors.New("detect boom")
	server.newDriftDetector = func(*rest.Config) (driftDetector, error) {
		return &stubDriftDetector{err: detectErr}, nil
	}

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "alpha", result.Drifts[0].Cluster)
	assert.Equal(t, gitops.DriftTypeMissing, result.Drifts[0].DriftType)
	require.Len(t, result.Drifts[0].Differences, 1)
	assert.Contains(t, result.Drifts[0].Differences[0], "Failed to detect drift")
	assert.Contains(t, result.Drifts[0].Differences[0], detectErr.Error())
}

func TestHandleDetectDriftAppendsSuccessDrifts(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://127.0.0.1:1",
	})
	wantDrifts := []gitops.DriftResult{{
		Cluster:     "alpha",
		ResourceKey: "ConfigMap/default/demo",
		Kind:        "ConfigMap",
		Namespace:   "default",
		Name:        "demo",
		DriftType:   gitops.DriftTypeModified,
		Differences: []string{"data.foo: git=bar cluster=baz"},
	}}
	server.newDriftDetector = func(*rest.Config) (driftDetector, error) {
		return &stubDriftDetector{drifts: wantDrifts}, nil
	}

	got, err := server.handleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"alpha"},
	}))
	require.NoError(t, err)

	result := got.(*GitOpsDriftResult)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, wantDrifts[0], result.Drifts[0])
	assert.Equal(t, 1, result.TotalDrifts)
}
