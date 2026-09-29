package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// registeredSchemas returns a name -> Schema map from a fresh Register-populated
// registry, so these assertions cover exactly what pkg/mcp/server's package
// init() wires in through policy_tools.go.
func registeredSchemas() map[string]protocol.Tool {
	reg := newTestRegistry()
	out := make(map[string]protocol.Tool)
	for _, td := range reg.Defs() {
		out[td.Schema.Name] = td.Schema
	}
	return out
}

func TestPolicyToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"check_gatekeeper",
		"get_ownership_policy_status",
		"list_ownership_violations",
		"install_ownership_policy",
		"set_ownership_policy_mode",
		"uninstall_ownership_policy",
	}

	registered := registeredSchemas()

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected policy tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestPolicyToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 6
	assert.Equal(t, expectedCount, len(registeredSchemas()),
		"Policy registry should have exactly %d tools", expectedCount)
}

func TestPolicyToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"set_ownership_policy_mode": {"mode"},
	}

	registered := registeredSchemas()

	for toolName, expectedRequired := range requiredFields {
		tool, ok := registered[toolName]
		require.True(t, ok, "tool %q should be registered", toolName)
		assert.ElementsMatch(t, expectedRequired, tool.InputSchema.Required,
			"tool %q should have required fields: %v", toolName, expectedRequired)
	}
}

func TestPolicyToolRegistry_EnumFields(t *testing.T) {
	registered := registeredSchemas()

	testCases := []struct {
		toolName     string
		propertyName string
		expectedEnum []string
	}{
		{
			toolName:     "install_ownership_policy",
			propertyName: "mode",
			expectedEnum: []string{"dryrun", "warn", "enforce"},
		},
		{
			toolName:     "set_ownership_policy_mode",
			propertyName: "mode",
			expectedEnum: []string{"dryrun", "warn", "enforce"},
		},
	}

	for _, tc := range testCases {
		tool, ok := registered[tc.toolName]
		require.True(t, ok, "%q should be registered", tc.toolName)

		prop, exists := tool.InputSchema.Properties[tc.propertyName]
		require.True(t, exists, "%q property should exist in %q", tc.propertyName, tc.toolName)
		assert.NotEmpty(t, prop.Enum, "%q.%q should have enum values", tc.toolName, tc.propertyName)
		assert.ElementsMatch(t, tc.expectedEnum, prop.Enum)
	}
}
