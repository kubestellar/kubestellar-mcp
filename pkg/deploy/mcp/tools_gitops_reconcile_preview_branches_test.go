package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Reconcile/PreviewChanges handlers are thin wrappers over handleSyncFromGit.
// Their two remaining uncovered branches are the JSON unmarshal error path on
// their own params struct and the downstream "repo is required" validation that
// handleSyncFromGit enforces after the wrapper re-marshals the args. Locking
// both keeps callers from regressing the wrappers into leaking a raw JSON error
// or silently accepting a missing repo.

func TestHandleReconcileReturnsInvalidArgumentsErrorOnMalformedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleReconcile(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandlePreviewChangesReturnsInvalidArgumentsErrorOnMalformedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handlePreviewChanges(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleReconcilePropagatesRepoRequiredFromSync(t *testing.T) {
	setGitOpsTempDir(t)
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleReconcile(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path": "manifests",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo is required")
}

func TestHandlePreviewChangesPropagatesRepoRequiredFromSync(t *testing.T) {
	setGitOpsTempDir(t)
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handlePreviewChanges(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path": "manifests",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo is required")
}

// TestHandleReconcileForcesDryRunFalse and its Preview counterpart lock the
// dry_run polarity of each wrapper: reconcile MUST apply changes (dry_run false)
// even if the caller supplied dry_run=true, and preview MUST NOT apply changes
// even if the caller supplied dry_run=false. If either wrapper ever forwards
// the caller-provided dry_run instead of forcing its own, this test breaks.
func TestHandleReconcileForcesDryRunFalseEvenWhenCallerRequestsTrue(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleReconcile(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"missing"},
		"dry_run":  true, // caller lies — reconcile must override to false
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.False(t, result.DryRun, "reconcile must force dry_run=false")
}

func TestHandlePreviewChangesForcesDryRunTrueEvenWhenCallerRequestsFalse(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{
		"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handlePreviewChanges(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo":     repo,
		"path":     "manifests",
		"clusters": []string{"missing"},
		"dry_run":  false, // caller lies — preview must override to true
	}))
	require.NoError(t, err)

	result := got.(*GitOpsSyncResult)
	assert.True(t, result.DryRun, "preview must force dry_run=true")
}
