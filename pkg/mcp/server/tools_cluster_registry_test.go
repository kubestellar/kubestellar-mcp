package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"list_clusters",
		"get_cluster_health",
	}

	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected cluster tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestClusterToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 2
	clusterTools := 0
	for _, td := range toolRegistry {
		switch td.Schema.Name {
		case "list_clusters", "get_cluster_health":
			clusterTools++
		}
	}
	assert.Equal(t, expectedCount, clusterTools, "Cluster registry should have exactly %d tools", expectedCount)
}

func TestClusterToolRegistry_EnumFields(t *testing.T) {
	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	tool, ok := registered["list_clusters"]
	require.True(t, ok, "list_clusters should be registered")

	source, exists := tool.InputSchema.Properties["source"]
	require.True(t, exists, "source property should exist")
	assert.NotEmpty(t, source.Enum, "source should have enum values")
	assert.ElementsMatch(t, []string{"all", "kubeconfig", "kubestellar"}, source.Enum)
}
