package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Closes the last uncovered arm of parseKustomizeBuildResult: the
// "resources" field type check. Existing TestParseKustomizeBuildResultValidatesTypes
// covers the outer type mismatch and the "output" field arm; it does not
// exercise a map whose "output" is a valid string but "resources" is not
// an int, nor the fully happy path. Both are asserted here so a refactor
// that swaps int for another numeric type (e.g. json.Number after a JSON
// round-trip) surfaces immediately.

func TestParseKustomizeBuildResult_ResourcesTypeMismatch(t *testing.T) {
	_, _, err := parseKustomizeBuildResult(map[string]interface{}{
		"output":    "apiVersion: v1\nkind: ConfigMap\n",
		"resources": "not-an-int",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected kustomize build resources type")
}

func TestParseKustomizeBuildResult_HappyPath(t *testing.T) {
	manifest, count, err := parseKustomizeBuildResult(map[string]interface{}{
		"output":    "apiVersion: v1\nkind: ConfigMap\n",
		"resources": 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: v1\nkind: ConfigMap\n", manifest)
	assert.Equal(t, 3, count)
}
