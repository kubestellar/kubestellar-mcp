package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriftToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"detect_drift",
	}

	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected drift tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestDriftToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 1
	driftTools := 0
	for _, td := range toolRegistry {
		if td.Schema.Name == "detect_drift" {
			driftTools++
		}
	}
	assert.Equal(t, expectedCount, driftTools, "Drift registry should have exactly %d tool", expectedCount)
}

func TestDriftToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"detect_drift": {"repo_url"},
	}

	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	for toolName, expectedRequired := range requiredFields {
		tool, ok := registered[toolName]
		require.True(t, ok, "tool %q should be registered", toolName)
		assert.ElementsMatch(t, expectedRequired, tool.InputSchema.Required,
			"tool %q should have required fields: %v", toolName, expectedRequired)
	}
}

func TestDriftToolRegistry_StringProperties(t *testing.T) {
	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	tool, ok := registered["detect_drift"]
	require.True(t, ok, "detect_drift should be registered")

	expectedStringProps := []string{"repo_url", "path", "branch", "cluster", "namespace"}
	for _, propName := range expectedStringProps {
		prop, exists := tool.InputSchema.Properties[propName]
		require.True(t, exists, "%q property should exist", propName)
		assert.Equal(t, "string", prop.Type, "%q should be string type", propName)
	}
}
