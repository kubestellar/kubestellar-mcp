package kubectl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolsReturnsKubectlToolDefs(t *testing.T) {
	deps := newFakeDeps()
	defs := deps.deps().Tools()

	require.Len(t, defs, 2)
	assert.Equal(t, []string{"delete_resource", "kubectl_apply"}, []string{defs[0].Name, defs[1].Name})

	for _, def := range defs {
		assert.NotEmpty(t, def.Description)
		assert.NotNil(t, def.InputSchema)
		assert.NotNil(t, def.Handler)
	}

	deleteRes, err := defs[0].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"kind":     "Pod",
		"name":     "demo",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)
	deleteMap, ok := deleteRes.(map[string]interface{})
	require.True(t, ok)
	deleteResults, ok := deleteMap["results"].([]DeleteResult)
	require.True(t, ok)
	require.Len(t, deleteResults, 1)
	assert.Equal(t, "would-delete", deleteResults[0].Status)

	applyRes, err := defs[1].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)
	applyMap, ok := applyRes.(map[string]interface{})
	require.True(t, ok)
	applyResults, ok := applyMap["results"].([]ApplyResult)
	require.True(t, ok)
	require.Len(t, applyResults, 1)
	assert.Equal(t, "would-apply", applyResults[0].Status)
}
