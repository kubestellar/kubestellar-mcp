package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleKustomizeApplyRejectsInvalidJSON covers the json.Unmarshal
// error-return arm of handleKustomizeApply — the earliest exit path.
// tools_kustomize_delete_branches_test.go already covers the symmetric
// arm on the Delete side.
func TestHandleKustomizeApplyRejectsInvalidJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	_, err := server.handleKustomizeApply(context.Background(), []byte(`"not-an-object"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestHandleKustomizeApplyRejectsMissingPath covers the empty-path guard
// (params.Path == "") — the second early-return arm.
func TestHandleKustomizeApplyRejectsMissingPath(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	_, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

// TestHandleKustomizeApplyRejectsUnresolvablePath covers the
// resolveKustomizePath error arm of handleKustomizeApply. A path that
// does not exist on disk fails resolveKustomizePath (Stat), and
// handleKustomizeApply returns the error verbatim without wrapping.
func TestHandleKustomizeApplyRejectsUnresolvablePath(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	_, err := server.handleKustomizeApply(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"path":     "/does/not/exist/at/all/kustomization",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
}
