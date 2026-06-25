package upgrades

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradesToolRegistry_AllToolsRegistered(t *testing.T) {
	expectedTools := []string{
		"detect_cluster_type",
		"get_cluster_version_info",
		"check_olm_operator_upgrades",
		"check_helm_release_upgrades",
		"get_upgrade_prerequisites",
		"trigger_openshift_upgrade",
		"get_upgrade_status",
	}

	tools := Tools()
	registered := make(map[string]bool)
	for _, td := range tools {
		registered[td.Schema.Name] = true
	}

	for _, name := range expectedTools {
		require.True(t, registered[name], "expected upgrades tool %q to be registered", name)
	}

	for _, td := range tools {
		assert.NotEmpty(t, td.Schema.Description, "tool %q should have a description", td.Schema.Name)
	}
}

func TestUpgradesToolRegistry_ToolCount(t *testing.T) {
	expectedCount := 7
	tools := Tools()
	assert.Equal(t, expectedCount, len(tools), "Upgrades registry should have exactly %d tools", expectedCount)
}

func TestUpgradesToolRegistry_RequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		"trigger_openshift_upgrade": {"target_version", "confirm"},
	}

	tools := Tools()
	registered := make(map[string]ToolDef)
	for _, td := range tools {
		registered[td.Schema.Name] = td
	}

	for toolName, expectedRequired := range requiredFields {
		tool, ok := registered[toolName]
		require.True(t, ok, "tool %q should be registered", toolName)
		assert.ElementsMatch(t, expectedRequired, tool.Schema.InputSchema.Required,
			"tool %q should have required fields: %v", toolName, expectedRequired)
	}
}

func TestUpgradesToolRegistry_ConfirmationGating(t *testing.T) {
	tools := Tools()
	registered := make(map[string]ToolDef)
	for _, td := range tools {
		registered[td.Schema.Name] = td
	}

	tool, ok := registered["trigger_openshift_upgrade"]
	require.True(t, ok, "trigger_openshift_upgrade should be registered")

	confirm, exists := tool.Schema.InputSchema.Properties["confirm"]
	require.True(t, exists, "confirm property should exist")
	assert.Equal(t, "string", confirm.Type, "confirm should be string type")
	assert.Contains(t, confirm.Description, "yes-upgrade-now", "confirm description should mention the required value")

	assert.Contains(t, tool.Schema.Description, "REQUIRES CONFIRMATION", "tool description should indicate confirmation requirement")
}

func TestUpgradesToolRegistry_AllHandlersExist(t *testing.T) {
	tools := Tools()
	for _, td := range tools {
		assert.NotNil(t, td.Handler, "tool %q should have a handler", td.Schema.Name)
	}
}
