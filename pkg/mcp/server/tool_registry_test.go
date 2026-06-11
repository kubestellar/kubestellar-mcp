package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindToolHandler_ReturnsNilForUnknownTool(t *testing.T) {
	handler := findToolHandler("completely_nonexistent_tool_xyz")
	assert.Nil(t, handler, "findToolHandler should return nil for unknown tool names")
}

func TestRegisteredTools_ReturnsNonEmptyList(t *testing.T) {
	tools := registeredTools()
	require.NotEmpty(t, tools, "registeredTools() should return tools after init()")
}

func TestRegisteredTools_AllHaveNonEmptyName(t *testing.T) {
	tools := registeredTools()
	for i, tool := range tools {
		assert.NotEmpty(t, tool.Name, "tool at index %d has empty Name", i)
	}
}

func TestRegisteredTools_AllHaveDescription(t *testing.T) {
	tools := registeredTools()
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Description, "tool %q has empty Description", tool.Name)
	}
}

func TestRegisteredTools_AllHaveHandler(t *testing.T) {
	for _, td := range toolRegistry {
		assert.NotNil(t, td.Handler, "tool %q has nil handler", td.Schema.Name)
	}
}

func TestRegisteredTools_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]int)
	for _, td := range toolRegistry {
		seen[td.Schema.Name]++
	}
	for name, count := range seen {
		assert.Equal(t, 1, count, "tool %q is registered %d times (expected 1)", name, count)
	}
}

func TestFindToolHandler_ReturnsHandlerForKnownTool(t *testing.T) {
	// list_clusters is registered by tools_cluster_registry.go init()
	handler := findToolHandler("list_clusters")
	require.NotNil(t, handler, "findToolHandler should find 'list_clusters'")
}

func TestRegisteredTools_CountMatchesRegistry(t *testing.T) {
	tools := registeredTools()
	assert.Equal(t, len(toolRegistry), len(tools),
		"registeredTools() length should match toolRegistry length")
}

func TestRegisteredTools_InputSchemaHasObjectType(t *testing.T) {
	for _, td := range toolRegistry {
		assert.Equal(t, "object", td.Schema.InputSchema.Type,
			"tool %q InputSchema.Type should be 'object'", td.Schema.Name)
	}
}
