package gitops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleSyncFromGitRejectsBlockedNamespace covers the
// server.ValidateNamespace branch inside handleSyncFromGit that guards the
// namespace override against system namespaces (#377). The equivalent branch
// on handleDetectDrift does not exist because that handler does not accept a
// namespace override.
func TestHandleSyncFromGitRejectsBlockedNamespace(t *testing.T) {
	setGitOpsTempDir(t)
	repo := createGitRepo(t, map[string]string{"manifests/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"})
	server := newTestServer(t, map[string]string{})

	cases := []string{"kube-system", "openshift-monitoring", "Invalid_NS"}
	for _, ns := range cases {
		t.Run(ns, func(t *testing.T) {
			_, err := server.HandleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
				"repo":      repo,
				"path":      "manifests",
				"namespace": ns,
			}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid namespace")
		})
	}
}

// TestHandleSyncFromGitReportsCloneFailure covers the ReadFromGit error
// branch on handleSyncFromGit. A file:// URL pointing at a path that is not
// a git repository fails the underlying git-clone subprocess and the handler
// wraps that error as "failed to read manifests from git".
func TestHandleSyncFromGitReportsCloneFailure(t *testing.T) {
	setGitOpsTempDir(t)
	server := newTestServer(t, map[string]string{})

	_, err := server.HandleSyncFromGit(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo": "file:///nonexistent/quality-agent-mcp-not-a-repo",
		"path": ".",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read manifests from git")
}

// TestHandleDetectDriftReportsCloneFailure is the drift-side twin of the
// sync clone-failure test above. It exercises the ReadFromGit error branch
// on handleDetectDrift.
func TestHandleDetectDriftReportsCloneFailure(t *testing.T) {
	setGitOpsTempDir(t)
	server := newTestServer(t, map[string]string{})

	_, err := server.HandleDetectDrift(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"repo": "file:///nonexistent/quality-agent-mcp-not-a-repo",
		"path": ".",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read manifests from git")
}
