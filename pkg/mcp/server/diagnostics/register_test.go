package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

func TestDiagnosticsToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"find_pod_issues",
		"find_deployment_issues",
		"check_resource_limits",
		"check_security_issues",
		"analyze_namespace",
		"get_warning_events",
	}

	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected diagnostics tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestDiagnosticsToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 6
	assert.Equal(t, expectedCount, len(newTestRegistry().Defs()), "Diagnostics registry should have exactly %d tools", expectedCount)
}

func TestDiagnosticsToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"analyze_namespace": {"namespace"},
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

func TestDiagnosticsToolRegistry_IntegerProperties(t *testing.T) {
	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	tool, ok := registered["get_warning_events"]
	require.True(t, ok, "get_warning_events should be registered")

	prop, exists := tool.InputSchema.Properties["limit"]
	require.True(t, exists, "limit property should exist in get_warning_events")
	assert.Equal(t, "integer", prop.Type, "get_warning_events.limit should be integer type")
}
