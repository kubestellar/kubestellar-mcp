package upgrades

import (
	"testing"
)

// TestToolsReturnsAllUpgradeTools verifies the Tools() function returns
// all expected upgrade tool definitions.
func TestToolsReturnsAllUpgradeTools(t *testing.T) {
	tools := Tools()

	expected := []struct {
		name     string
		required []string
	}{
		{"detect_cluster_type", nil},
		{"get_cluster_version_info", nil},
		{"check_olm_operator_upgrades", nil},
		{"check_helm_release_upgrades", nil},
		{"get_upgrade_prerequisites", nil},
		{"trigger_openshift_upgrade", []string{"target_version", "confirm"}},
		{"get_upgrade_status", nil},
	}

	if len(tools) != len(expected) {
		t.Fatalf("Tools() returned %d tools, want %d", len(tools), len(expected))
	}

	toolMap := make(map[string]ToolDef, len(tools))
	for _, td := range tools {
		toolMap[td.Schema.Name] = td
	}

	for _, exp := range expected {
		td, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected upgrade tool %q to be returned by Tools()", exp.name)
			continue
		}
		if td.Schema.Description == "" {
			t.Errorf("upgrade tool %q has empty description", exp.name)
		}
		if td.Schema.InputSchema.Type != "object" {
			t.Errorf("upgrade tool %q schema type = %q, want \"object\"", exp.name, td.Schema.InputSchema.Type)
		}
		if td.Handler == nil {
			t.Errorf("upgrade tool %q has nil handler", exp.name)
		}
		assertRequired(t, exp.name, td.Schema.InputSchema.Required, exp.required)
	}
}

// TestTriggerOpenShiftUpgradeRequiresConfirmation verifies the confirmation
// gate on the upgrade trigger tool.
func TestTriggerOpenShiftUpgradeRequiresConfirmation(t *testing.T) {
	tools := Tools()
	for _, td := range tools {
		if td.Schema.Name != "trigger_openshift_upgrade" {
			continue
		}

		reqSet := make(map[string]bool)
		for _, r := range td.Schema.InputSchema.Required {
			reqSet[r] = true
		}
		if !reqSet["confirm"] {
			t.Error("trigger_openshift_upgrade must require 'confirm' field")
		}
		if !reqSet["target_version"] {
			t.Error("trigger_openshift_upgrade must require 'target_version' field")
		}
		return
	}
	t.Fatal("trigger_openshift_upgrade not found in Tools()")
}

// TestToolsNoDuplicateNames verifies no duplicate tool names.
func TestToolsNoDuplicateNames(t *testing.T) {
	tools := Tools()
	seen := make(map[string]bool)
	for _, td := range tools {
		if seen[td.Schema.Name] {
			t.Errorf("duplicate upgrade tool name: %q", td.Schema.Name)
		}
		seen[td.Schema.Name] = true
	}
}

// TestToolsAllHaveClusterProperty verifies every upgrade tool accepts
// an optional "cluster" property.
func TestToolsAllHaveClusterProperty(t *testing.T) {
	tools := Tools()
	for _, td := range tools {
		if _, ok := td.Schema.InputSchema.Properties["cluster"]; !ok {
			t.Errorf("upgrade tool %q missing 'cluster' property", td.Schema.Name)
		}
	}
}

func assertRequired(t *testing.T, toolName string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("tool %q: required fields = %v, want %v", toolName, got, want)
		return
	}
	gotSet := make(map[string]bool, len(got))
	for _, r := range got {
		gotSet[r] = true
	}
	for _, r := range want {
		if !gotSet[r] {
			t.Errorf("tool %q: missing required field %q", toolName, r)
		}
	}
}
