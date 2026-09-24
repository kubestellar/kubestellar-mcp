package gitops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolsReturnsAllGitOpsToolDefs exercises Server.Tools() directly (it is
// otherwise only invoked indirectly through the root package's adapter,
// which is out of scope for this package's coverage).
func TestToolsReturnsAllGitOpsToolDefs(t *testing.T) {
	server := newTestServer(t, map[string]string{})

	defs := server.Tools()
	require.NotEmpty(t, defs)

	wantNames := []string{"detect_drift", "sync_from_git", "reconcile", "preview_changes"}
	gotNames := make([]string, 0, len(defs))
	for _, d := range defs {
		gotNames = append(gotNames, d.Name)
		assert.NotEmpty(t, d.Description)
		assert.NotNil(t, d.InputSchema)
		assert.NotNil(t, d.Handler)
	}
	assert.Equal(t, wantNames, gotNames)
}
