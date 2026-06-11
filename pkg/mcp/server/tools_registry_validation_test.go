package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedToolsByRegistry maps each registry file to its expected tool names.
var expectedToolsByRegistry = map[string][]string{
	"cluster": {"list_clusters", "get_cluster_health"},
	"drift":   {"detect_drift"},
	"policy": {
		"check_gatekeeper", "get_ownership_policy_status",
		"list_ownership_violations", "install_ownership_policy",
		"set_ownership_policy_mode", "uninstall_ownership_policy",
	},
	"rbac": {
		"get_roles", "get_cluster_roles", "get_role_bindings",
		"get_cluster_role_bindings", "can_i", "analyze_subject_permissions",
		"describe_role",
	},
	"workloads": {
		"get_pods", "get_deployments", "get_services", "get_nodes",
		"get_events", "describe_pod", "get_pod_logs", "find_pod_issues",
		"find_deployment_issues", "check_resource_limits",
		"check_security_issues", "analyze_namespace", "get_warning_events",
		"audit_kubeconfig", "find_resource_owners",
	},
}

func TestRegistryTools_AllExpectedToolsRegistered(t *testing.T) {
	registered := make(map[string]bool)
	for _, td := range toolRegistry {
		registered[td.Schema.Name] = true
	}

	for domain, tools := range expectedToolsByRegistry {
		for _, name := range tools {
			assert.True(t, registered[name],
				"tool %q from %s registry is not registered", name, domain)
		}
	}
}

func TestRegistryTools_RequiredFieldsAreInProperties(t *testing.T) {
	for _, td := range toolRegistry {
		schema := td.Schema.InputSchema
		for _, req := range schema.Required {
			_, exists := schema.Properties[req]
			assert.True(t, exists,
				"tool %q: required field %q is not defined in Properties",
				td.Schema.Name, req)
		}
	}
}

func TestRegistryTools_EnumFieldsAreNonEmpty(t *testing.T) {
	for _, td := range toolRegistry {
		for propName, prop := range td.Schema.InputSchema.Properties {
			if prop.Enum != nil {
				assert.NotEmpty(t, prop.Enum,
					"tool %q property %q has empty Enum slice",
					td.Schema.Name, propName)
				for i, val := range prop.Enum {
					assert.NotEmpty(t, val,
						"tool %q property %q Enum[%d] is empty",
						td.Schema.Name, propName, i)
				}
			}
		}
	}
}

func TestRegistryTools_PropertyTypesAreValid(t *testing.T) {
	validTypes := map[string]bool{
		"string":  true,
		"number":  true,
		"integer": true,
		"boolean": true,
		"array":   true,
		"object":  true,
	}
	for _, td := range toolRegistry {
		for propName, prop := range td.Schema.InputSchema.Properties {
			if prop.Type != "" {
				assert.True(t, validTypes[prop.Type],
					"tool %q property %q has invalid type %q",
					td.Schema.Name, propName, prop.Type)
			}
		}
	}
}

func TestRegistryTools_HandlersResolveByName(t *testing.T) {
	for _, td := range toolRegistry {
		handler := findToolHandler(td.Schema.Name)
		require.NotNil(t, handler,
			"findToolHandler(%q) should return a non-nil handler", td.Schema.Name)
	}
}

func TestRegistryTools_DescriptionsAreSubstantive(t *testing.T) {
	for _, td := range toolRegistry {
		desc := td.Schema.Description
		assert.Greater(t, len(desc), 10,
			"tool %q description is too short (%d chars): %q",
			td.Schema.Name, len(desc), desc)
	}
}

func TestRegistryTools_NoDuplicateEnumValues(t *testing.T) {
	for _, td := range toolRegistry {
		for propName, prop := range td.Schema.InputSchema.Properties {
			if prop.Enum == nil {
				continue
			}
			seen := make(map[string]bool)
			for _, val := range prop.Enum {
				assert.False(t, seen[val],
					"tool %q property %q has duplicate enum value %q",
					td.Schema.Name, propName, val)
				seen[val] = true
			}
		}
	}
}
