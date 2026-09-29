package workloads

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

func TestWorkloadsToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"get_pods",
		"get_deployments",
		"get_services",
		"get_nodes",
		"get_events",
		"describe_pod",
		"get_pod_logs",
	}

	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected workloads tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestWorkloadsToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 7
	assert.Equal(t, expectedCount, len(newTestRegistry().Defs()), "Workloads registry should have exactly %d tools", expectedCount)
}

func TestWorkloadsToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"describe_pod": {"name"},
		"get_pod_logs": {"name"},
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

func TestWorkloadsToolRegistry_IntegerProperties(t *testing.T) {
	registered := make(map[string]protocol.Tool)
	for _, td := range newTestRegistry().Defs() {
		registered[td.Schema.Name] = td.Schema
	}

	testCases := []struct {
		toolName     string
		propertyName string
	}{
		{"get_events", "limit"},
		{"get_pod_logs", "tail_lines"},
	}

	for _, tc := range testCases {
		tool, ok := registered[tc.toolName]
		require.True(t, ok, "%q should be registered", tc.toolName)

		prop, exists := tool.InputSchema.Properties[tc.propertyName]
		require.True(t, exists, "%q property should exist in %q", tc.propertyName, tc.toolName)
		assert.Equal(t, "integer", prop.Type, "%q.%q should be integer type", tc.toolName, tc.propertyName)
	}
}
