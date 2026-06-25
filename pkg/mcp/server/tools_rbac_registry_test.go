package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRBACToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"get_roles",
		"get_cluster_roles",
		"get_role_bindings",
		"get_cluster_role_bindings",
		"can_i",
		"analyze_subject_permissions",
		"describe_role",
	}

	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	for _, name := range expectedTools {
		tool, ok := registered[name]
		require.True(t, ok, "expected RBAC tool %q to be registered", name)
		assert.NotEmpty(t, tool.Description, "tool %q should have a description", name)
	}
}

func TestRBACToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 7
	rbacTools := 0
	for _, td := range toolRegistry {
		switch td.Schema.Name {
		case "get_roles", "get_cluster_roles", "get_role_bindings",
			"get_cluster_role_bindings", "can_i", "analyze_subject_permissions",
			"describe_role":
			rbacTools++
		}
	}
	assert.Equal(t, expectedCount, rbacTools, "RBAC registry should have exactly %d tools", expectedCount)
}

func TestRBACToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"can_i":                       {"verb", "resource"},
		"analyze_subject_permissions": {"subject_kind", "subject_name"},
		"describe_role":               {"name"},
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

func TestRBACToolRegistry_EnumFields(t *testing.T) {
	registered := make(map[string]Tool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = td.Schema
	}

	tool, ok := registered["analyze_subject_permissions"]
	require.True(t, ok, "analyze_subject_permissions should be registered")

	subjectKind, exists := tool.InputSchema.Properties["subject_kind"]
	require.True(t, exists, "subject_kind property should exist")
	assert.NotEmpty(t, subjectKind.Enum, "subject_kind should have enum values")
	assert.ElementsMatch(t, []string{"User", "Group", "ServiceAccount"}, subjectKind.Enum)
}
