package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

func TestPolicyToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"check_gatekeeper",
		"get_ownership_policy_status",
		"list_ownership_violations",
		"install_ownership_policy",
		"set_ownership_policy_mode",
		"uninstall_ownership_policy",
	}

	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected policy tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestPolicyToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 6
	policyTools := 0
	for _, td := range newTestRegistry().Defs() {
		switch td.Schema.Name {
		case "check_gatekeeper", "get_ownership_policy_status",
			"list_ownership_violations", "install_ownership_policy",
			"set_ownership_policy_mode", "uninstall_ownership_policy":
			policyTools++
		}
	}
	assert.Equal(t, expectedCount, policyTools, "Policy registry should have exactly %d tools", expectedCount)
}

func TestPolicyToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"set_ownership_policy_mode": {"mode"},
	}

	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	for toolName, expectedRequired := range requiredFields {
		tool, ok := registered[toolName]
		require.True(t, ok, "tool %q should be registered", toolName)
		assert.ElementsMatch(t, expectedRequired, tool.InputSchema.Required,
			"tool %q should have required fields: %v", toolName, expectedRequired)
	}
}

func TestPolicyToolRegistry_EnumFields(t *testing.T) {
	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

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

func TestRegister_PreservesRegistrationOrder(t *testing.T) {
	want := []string{
		"check_gatekeeper",
		"get_ownership_policy_status",
		"list_ownership_violations",
		"install_ownership_policy",
		"set_ownership_policy_mode",
		"uninstall_ownership_policy",
	}

	var got []string
	for _, td := range newTestRegistry().Defs() {
		got = append(got, td.Schema.Name)
		assert.NotNil(t, td.Handler, "tool %q should have a handler", td.Schema.Name)
	}
	assert.Equal(t, want, got)
}
