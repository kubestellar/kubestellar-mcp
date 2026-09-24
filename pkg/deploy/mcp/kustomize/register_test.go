package kustomize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolsReturnsAllKustomizeToolDefs exercises Deps.Tools() directly (it is
// otherwise only invoked indirectly through the root package's adapter,
// which is out of scope for this package's coverage).
func TestToolsReturnsAllKustomizeToolDefs(t *testing.T) {
	deps := newTestDeps(t, map[string]string{})

	defs := deps.Tools()
	require.NotEmpty(t, defs)

	wantNames := []string{"kustomize_build", "kustomize_apply", "kustomize_delete"}
	gotNames := make([]string, 0, len(defs))
	for _, d := range defs {
		gotNames = append(gotNames, d.Name)
		assert.NotEmpty(t, d.Description)
		assert.NotNil(t, d.InputSchema)
		assert.NotNil(t, d.Handler)
	}
	assert.Equal(t, wantNames, gotNames)
}
