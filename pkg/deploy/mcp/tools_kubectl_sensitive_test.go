package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"role", "rolebinding", "Role", "RoleBinding",
		"ingress", "statefulset", "daemonset", "job", "cronjob",
		"", // empty string
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

// TestManifestSensitiveKind_JSON verifies detection of sensitive kinds
// in JSON manifests.
func TestManifestSensitiveKind_JSON(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantKind  string
		wantBlock bool
	}{
		{
			name:      "ClusterRole JSON",
			manifest:  `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"admin"}}`,
			wantKind:  "ClusterRole",
			wantBlock: true,
		},
		{
			name:      "ClusterRoleBinding JSON",
			manifest:  `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRoleBinding","metadata":{"name":"admin-binding"}}`,
			wantKind:  "ClusterRoleBinding",
			wantBlock: true,
		},
		{
			name:      "Secret JSON",
			manifest:  `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"my-secret"},"data":{"key":"dmFsdWU="}}`,
			wantKind:  "Secret",
			wantBlock: true,
		},
		{
			name:      "ServiceAccount JSON",
			manifest:  `{"apiVersion":"v1","kind":"ServiceAccount","metadata":{"name":"my-sa"}}`,
			wantKind:  "ServiceAccount",
			wantBlock: true,
		},
		{
			name:      "Deployment JSON (allowed)",
			manifest:  `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"my-app"}}`,
			wantKind:  "Deployment",
			wantBlock: false,
		},
		{
			name:      "ConfigMap JSON (allowed)",
			manifest:  `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"config"}}`,
			wantKind:  "ConfigMap",
			wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, blocked := manifestSensitiveKind(tt.manifest)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBlock, blocked)
		})
	}
}

// TestManifestSensitiveKind_YAML verifies detection of sensitive kinds
// in YAML manifests.
func TestManifestSensitiveKind_YAML(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantKind  string
		wantBlock bool
	}{
		{
			name: "ClusterRole YAML",
			manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin`,
			wantKind:  "ClusterRole",
			wantBlock: true,
		},
		{
			name: "Secret YAML",
			manifest: `apiVersion: v1
kind: Secret
metadata:
  name: my-secret
data:
  password: cGFzc3dvcmQ=`,
			wantKind:  "Secret",
			wantBlock: true,
		},
		{
			name: "Deployment YAML (allowed)",
			manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx`,
			wantKind:  "Deployment",
			wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, blocked := manifestSensitiveKind(tt.manifest)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBlock, blocked)
		})
	}
}

// TestManifestSensitiveKind_EdgeCases covers empty, whitespace, and
// malformed manifests.
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
