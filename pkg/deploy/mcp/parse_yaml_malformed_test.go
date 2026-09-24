package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseYAML_MalformedYAMLReturnsError exercises parseYAML's early-
// return branch where yamlToJSONBytes (k8syaml.ToJSON) rejects the input.
// The rest of the function is well-covered by existing tests via the
// happy path.
func TestParseYAML_MalformedYAMLReturnsError(t *testing.T) {
	// A YAML mapping opened but not closed is a hard parse error in
	// k8s.io/apimachinery/pkg/util/yaml.
	bad := []byte("key: {unterminated: mapping\n  another: entry")

	var out map[string]interface{}
	err := parseYAML(bad, &out)
	require.Error(t, err)
}
