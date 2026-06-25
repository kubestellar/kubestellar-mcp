package server

import (
	"testing"
)

// TestAllExpectedToolsRegistered verifies that every tool from the registry
// files (RBAC, policy, workloads, drift, cluster) is present in the global
// tool registry after init().
func TestAllExpectedToolsRegistered(t *testing.T) {
	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	// Every registered tool must have a non-empty description.
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}

	// Verify no duplicate tool names.
	if len(toolMap) != len(tools) {
		seen := make(map[string]int)
		for _, tool := range tools {
			seen[tool.Name]++
		}
		for name, count := range seen {
			if count > 1 {
				t.Errorf("duplicate tool registration: %q (registered %d times)", name, count)
			}
		}
	}
}

// TestRBACRegistryTools verifies all RBAC tools from tools_rbac_registry.go.
func TestRBACRegistryTools(t *testing.T) {
	expected := []struct {
		name     string
		required []string
	}{
		{"get_roles", nil},
		{"get_cluster_roles", nil},
		{"get_role_bindings", nil},
		{"get_cluster_role_bindings", nil},
		{"can_i", []string{"verb", "resource"}},
		{"analyze_subject_permissions", []string{"subject_kind", "subject_name"}},
		{"describe_role", []string{"name"}},
	}

	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, exp := range expected {
		tool, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected RBAC tool %q to be registered", exp.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("RBAC tool %q has empty description", exp.name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("RBAC tool %q schema type = %q, want \"object\"", exp.name, tool.InputSchema.Type)
		}
		assertRequired(t, exp.name, tool.InputSchema.Required, exp.required)
	}
}

// TestPolicyRegistryTools verifies all policy tools from tools_policy_registry.go.
func TestPolicyRegistryTools(t *testing.T) {
	expected := []struct {
		name     string
		required []string
	}{
		{"check_gatekeeper", nil},
		{"get_ownership_policy_status", nil},
		{"list_ownership_violations", nil},
		{"install_ownership_policy", nil},
		{"set_ownership_policy_mode", []string{"mode"}},
		{"uninstall_ownership_policy", nil},
	}

	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, exp := range expected {
		tool, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected policy tool %q to be registered", exp.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("policy tool %q has empty description", exp.name)
		}
		assertRequired(t, exp.name, tool.InputSchema.Required, exp.required)
	}
}

// TestWorkloadsRegistryTools verifies all workload tools from tools_workloads_registry.go.
func TestWorkloadsRegistryTools(t *testing.T) {
	expected := []struct {
		name     string
		required []string
	}{
		{"get_pods", nil},
		{"get_deployments", nil},
		{"get_services", nil},
		{"get_nodes", nil},
		{"get_events", nil},
		{"describe_pod", []string{"name"}},
		{"get_pod_logs", []string{"name"}},
		{"find_pod_issues", nil},
		{"find_deployment_issues", nil},
		{"check_resource_limits", nil},
		{"check_security_issues", nil},
		{"analyze_namespace", []string{"namespace"}},
		{"get_warning_events", nil},
		{"audit_kubeconfig", nil},
		{"find_resource_owners", []string{"namespace"}},
	}

	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, exp := range expected {
		tool, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected workloads tool %q to be registered", exp.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("workloads tool %q has empty description", exp.name)
		}
		assertRequired(t, exp.name, tool.InputSchema.Required, exp.required)
	}
}

// TestDriftRegistryTools verifies the drift tool from tools_drift_registry.go.
func TestDriftRegistryTools(t *testing.T) {
	expected := []struct {
		name     string
		required []string
	}{
		{"detect_drift", []string{"repo_url"}},
	}

	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, exp := range expected {
		tool, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected drift tool %q to be registered", exp.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("drift tool %q has empty description", exp.name)
		}
		assertRequired(t, exp.name, tool.InputSchema.Required, exp.required)
	}
}

// TestClusterRegistryTools verifies cluster tools from tools_cluster_registry.go.
func TestClusterRegistryTools(t *testing.T) {
	expected := []struct {
		name     string
		required []string
	}{
		{"list_clusters", nil},
		{"get_cluster_health", nil},
	}

	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, exp := range expected {
		tool, ok := toolMap[exp.name]
		if !ok {
			t.Errorf("expected cluster tool %q to be registered", exp.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("cluster tool %q has empty description", exp.name)
		}
		assertRequired(t, exp.name, tool.InputSchema.Required, exp.required)
	}
}

// TestFindToolHandlerReturnsNilForUnknown verifies the lookup function.
func TestFindToolHandlerReturnsNilForUnknown(t *testing.T) {
	if h := findToolHandler("nonexistent_tool_xyz"); h != nil {
		t.Error("expected nil handler for unknown tool")
	}
}

// TestFindToolHandlerReturnsHandlerForKnown verifies known tools have handlers.
func TestFindToolHandlerReturnsHandlerForKnown(t *testing.T) {
	known := []string{"list_clusters", "get_cluster_health", "can_i", "detect_drift", "get_pods"}
	for _, name := range known {
		if h := findToolHandler(name); h == nil {
			t.Errorf("expected non-nil handler for tool %q", name)
		}
	}
}

// TestPolicyToolEnumValues verifies enum constraints on policy tools.
func TestPolicyToolEnumValues(t *testing.T) {
	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	// install_ownership_policy.mode should have dryrun, warn, enforce
	if tool, ok := toolMap["install_ownership_policy"]; ok {
		modeProp, ok := tool.InputSchema.Properties["mode"]
		if !ok {
			t.Fatal("install_ownership_policy missing 'mode' property")
		}
		expectedEnums := []string{"dryrun", "warn", "enforce"}
		if len(modeProp.Enum) != len(expectedEnums) {
			t.Errorf("mode enum count = %d, want %d", len(modeProp.Enum), len(expectedEnums))
		}
		enumSet := make(map[string]bool)
		for _, e := range modeProp.Enum {
			enumSet[e] = true
		}
		for _, e := range expectedEnums {
			if !enumSet[e] {
				t.Errorf("mode enum missing %q", e)
			}
		}
	}

	// set_ownership_policy_mode.mode should have same enum
	if tool, ok := toolMap["set_ownership_policy_mode"]; ok {
		modeProp, ok := tool.InputSchema.Properties["mode"]
		if !ok {
			t.Fatal("set_ownership_policy_mode missing 'mode' property")
		}
		if len(modeProp.Enum) != 3 {
			t.Errorf("mode enum count = %d, want 3", len(modeProp.Enum))
		}
	}
}

// TestRBACToolAnalyzeSubjectEnumValues verifies subject_kind enum.
func TestRBACToolAnalyzeSubjectEnumValues(t *testing.T) {
	tools := registeredTools()
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	tool, ok := toolMap["analyze_subject_permissions"]
	if !ok {
		t.Fatal("analyze_subject_permissions not registered")
	}

	kindProp, ok := tool.InputSchema.Properties["subject_kind"]
	if !ok {
		t.Fatal("missing subject_kind property")
	}

	expected := []string{"User", "Group", "ServiceAccount"}
	if len(kindProp.Enum) != len(expected) {
		t.Fatalf("subject_kind enum count = %d, want %d", len(kindProp.Enum), len(expected))
	}
	enumSet := make(map[string]bool)
	for _, e := range kindProp.Enum {
		enumSet[e] = true
	}
	for _, e := range expected {
		if !enumSet[e] {
			t.Errorf("subject_kind enum missing %q", e)
		}
	}
}

// assertRequired checks that got and want slices match.
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
