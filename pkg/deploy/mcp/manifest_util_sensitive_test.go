package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestIsSensitiveKind verifies that all RBAC, Secret, and ServiceAccount
// resource kinds are blocked regardless of case or plural form.
func TestIsSensitiveKind(t *testing.T) {
	blocked := []struct {
		name string
		kind string
	}{
		{name: "clusterrole", kind: "clusterrole"},
		{name: "clusterroles plural", kind: "clusterroles"},
		{name: "ClusterRole mixed case", kind: "ClusterRole"},
		{name: "CLUSTERROLE upper", kind: "CLUSTERROLE"},
		{name: "clusterrolebinding", kind: "clusterrolebinding"},
		{name: "clusterrolebindings plural", kind: "clusterrolebindings"},
		{name: "ClusterRoleBinding mixed", kind: "ClusterRoleBinding"},
		{name: "secret", kind: "secret"},
		{name: "secrets plural", kind: "secrets"},
		{name: "Secret capitalized", kind: "Secret"},
		{name: "SECRET upper", kind: "SECRET"},
		{name: "serviceaccount", kind: "serviceaccount"},
		{name: "serviceaccounts plural", kind: "serviceaccounts"},
		{name: "ServiceAccount mixed", kind: "ServiceAccount"},
		{name: "sa shorthand", kind: "sa"},
		{name: "SA upper", kind: "SA"},
		{name: "role", kind: "role"},
		{name: "roles plural", kind: "roles"},
		{name: "Role mixed", kind: "Role"},
		{name: "rolebinding", kind: "rolebinding"},
		{name: "rolebindings plural", kind: "rolebindings"},
		{name: "RoleBinding mixed", kind: "RoleBinding"},
		{name: "mutatingwebhookconfiguration", kind: "mutatingwebhookconfiguration"},
		{name: "MutatingWebhookConfiguration mixed", kind: "MutatingWebhookConfiguration"},
		{name: "validatingwebhookconfiguration", kind: "validatingwebhookconfiguration"},
		{name: "ValidatingWebhookConfiguration mixed", kind: "ValidatingWebhookConfiguration"},
		{name: "certificatesigningrequest", kind: "certificatesigningrequest"},
		{name: "CertificateSigningRequest mixed", kind: "CertificateSigningRequest"},
		{name: "csr shorthand", kind: "csr"},
		{name: "podsecuritypolicy", kind: "podsecuritypolicy"},
		{name: "PodSecurityPolicy mixed", kind: "PodSecurityPolicy"},
		{name: "psp shorthand", kind: "psp"},
	}

	for _, tt := range blocked {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, isSensitiveKind(tt.kind), "kind %q should be blocked", tt.kind)
		})
	}
}

// TestIsSensitiveKind_Allowed verifies that non-sensitive resource kinds pass.
func TestIsSensitiveKind_Allowed(t *testing.T) {
	allowed := []string{
		"deployment", "Deployment", "pod", "Pod", "service", "Service",
		"configmap", "ConfigMap", "namespace", "Namespace",
		"ingress", "statefulset", "daemonset", "job", "cronjob",
		"",
	}

	for _, kind := range allowed {
		t.Run(kind, func(t *testing.T) {
			assert.False(t, isSensitiveKind(kind), "kind %q should be allowed", kind)
		})
	}
}

// TestSensitiveKindError verifies error message format.
func TestSensitiveKindError(t *testing.T) {
	err := sensitiveKindError("ClusterRole")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClusterRole")
	assert.Contains(t, err.Error(), "blocked")
	assert.Contains(t, err.Error(), "privilege escalation")
	assert.Contains(t, err.Error(), "kubectl directly")
}

// TestManifestSensitiveKind_JSON verifies detection of sensitive kinds in JSON manifests.
func TestManifestSensitiveKind_JSON(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantKind  string
		wantBlock bool
	}{
		{name: "ClusterRole JSON", manifest: `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"admin"}}`, wantKind: "ClusterRole", wantBlock: true},
		{name: "ClusterRoleBinding JSON", manifest: `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRoleBinding","metadata":{"name":"admin-binding"}}`, wantKind: "ClusterRoleBinding", wantBlock: true},
		{name: "Secret JSON", manifest: `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"my-secret"},"data":{"key":"dmFsdWU="}}`, wantKind: "Secret", wantBlock: true},
		{name: "ServiceAccount JSON", manifest: `{"apiVersion":"v1","kind":"ServiceAccount","metadata":{"name":"my-sa"}}`, wantKind: "ServiceAccount", wantBlock: true},
		{name: "Deployment JSON (allowed)", manifest: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"my-app"}}`, wantKind: "Deployment", wantBlock: false},
		{name: "ConfigMap JSON (allowed)", manifest: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"config"}}`, wantKind: "ConfigMap", wantBlock: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, blocked := manifestSensitiveKind(tt.manifest)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBlock, blocked)
		})
	}
}

// TestManifestSensitiveKind_YAML verifies detection of sensitive kinds in YAML manifests.
func TestManifestSensitiveKind_YAML(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantKind  string
		wantBlock bool
	}{
		{name: "ClusterRole YAML", manifest: "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: admin", wantKind: "ClusterRole", wantBlock: true},
		{name: "Secret YAML", manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: my-secret\ndata:\n  password: cGFzc3dvcmQ=", wantKind: "Secret", wantBlock: true},
		{name: "Deployment YAML (allowed)", manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: nginx", wantKind: "Deployment", wantBlock: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, blocked := manifestSensitiveKind(tt.manifest)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBlock, blocked)
		})
	}
}

// TestManifestSensitiveKind_EdgeCases covers empty, whitespace, and malformed manifests.
func TestManifestSensitiveKind_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantKind  string
		wantBlock bool
	}{
		{name: "empty string", manifest: "", wantKind: "", wantBlock: false},
		{name: "whitespace only", manifest: "   \n\t\n  ", wantKind: "", wantBlock: false},
		{name: "invalid YAML", manifest: "not: valid: yaml: [[[", wantKind: "", wantBlock: false},
		{name: "no kind field", manifest: `{"apiVersion":"v1","metadata":{"name":"x"}}`, wantKind: "", wantBlock: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, blocked := manifestSensitiveKind(tt.manifest)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBlock, blocked)
		})
	}
}

func TestYAMLHelpersWithJSONInput(t *testing.T) {
	input := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"demo"}}`
	if yamlToJSON(input) != input {
		t.Fatalf("yamlToJSON() should return input unchanged")
	}

	data, err := yamlToJSONBytes([]byte(input))
	if err != nil {
		t.Fatalf("yamlToJSONBytes() unexpected error: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal converted bytes: %v", err)
	}
	if parsed["kind"] != "ConfigMap" {
		t.Fatalf("unexpected parsed object: %#v", parsed)
	}

	var obj unstructured.Unstructured
	if err := unstructuredFromYAML(input, &obj); err != nil {
		t.Fatalf("unstructuredFromYAML() unexpected error: %v", err)
	}
	if obj.GetKind() != "ConfigMap" || obj.GetName() != "demo" {
		t.Fatalf("unexpected unstructured object: %#v", obj.Object)
	}
}

func TestParseYAMLHandlesYAMLInput(t *testing.T) {
	var parsed map[string]interface{}
	err := parseYAML([]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: demo\n"), &parsed)
	if err != nil {
		t.Fatalf("parseYAML() unexpected error: %v", err)
	}
	if parsed["kind"] != "Pod" {
		t.Fatalf("parseYAML() kind = %v, want Pod", parsed["kind"])
	}
	meta, ok := parsed["metadata"].(map[string]interface{})
	if !ok || meta["name"] != "demo" {
		t.Fatalf("parseYAML() metadata.name = %v, want demo", meta["name"])
	}
}

func TestParseYAML_MalformedYAMLReturnsError(t *testing.T) {
	bad := []byte("key: {unterminated: mapping\n  another: entry")

	var out map[string]interface{}
	err := parseYAML(bad, &out)
	require.Error(t, err)
}
