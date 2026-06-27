package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateNamespace_BlockedExact verifies that every namespace in the
// blockedExact map is rejected by ValidateNamespace.
func TestValidateNamespace_BlockedExact(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "kube-system", namespace: "kube-system"},
		{name: "kube-public", namespace: "kube-public"},
		{name: "kube-node-lease", namespace: "kube-node-lease"},
		{name: "gatekeeper-system", namespace: "gatekeeper-system"},
		{name: "openshift", namespace: "openshift"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "not allowed")
		})
	}
}

// TestValidateNamespace_BlockedPrefix verifies that any namespace beginning
// with "openshift-" is rejected.
func TestValidateNamespace_BlockedPrefix(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "openshift-ingress", namespace: "openshift-ingress"},
		{name: "openshift-monitoring", namespace: "openshift-monitoring"},
		{name: "openshift-api", namespace: "openshift-api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "not allowed")
		})
	}
}

// TestValidateNamespace_Allowed verifies that user-facing namespaces and the
// empty string (all-namespaces mode) are accepted.
func TestValidateNamespace_Allowed(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "empty (all-namespaces)", namespace: ""},
		{name: "default", namespace: "default"},
		{name: "my-app", namespace: "my-app"},
		{name: "kube-flannel", namespace: "kube-flannel"},
		{name: "istio-system", namespace: "istio-system"},
		{name: "custom-ns", namespace: "custom-ns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.NoError(t, err)
		})
	}
}

// TestValidateNamespace_CaseSensitive asserts that the function is
// case-sensitive: only the exact lower-case forms in the blocklist are
// rejected, so mixed-case variants are allowed.
func TestValidateNamespace_CaseSensitive(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "Kube-System mixed case", namespace: "Kube-System"},
		{name: "KUBE-SYSTEM upper", namespace: "KUBE-SYSTEM"},
		{name: "OPENSHIFT upper", namespace: "OPENSHIFT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.NoError(t, err, "namespace %q should not be blocked (case-sensitive)", tt.namespace)
		})
	}
}

// TestValidateNamespace_EdgeCases covers inputs that could be confused with
// blocked namespaces but should be allowed.
func TestValidateNamespace_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "kube without suffix", namespace: "kube"},
		{name: "openshifted (not openshift- prefix)", namespace: "openshifted"},
		{name: "dot", namespace: "."},
		{name: "kube- prefix only", namespace: "kube-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.NoError(t, err, "namespace %q should be allowed", tt.namespace)
		})
	}
}

// TestExtractAndValidateNamespace exercises the helper that extracts and
// validates the "namespace" key from a tool argument map.
func TestExtractAndValidateNamespace(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		wantNs    string
		wantErr   bool
		errExpect string
	}{
		{
			name:    "blocked namespace kube-system",
			args:    map[string]interface{}{"namespace": "kube-system"},
			wantErr: true,
		},
		{
			name:    "allowed namespace default",
			args:    map[string]interface{}{"namespace": "default"},
			wantNs:  "default",
			wantErr: false,
		},
		{
			name:    "key absent",
			args:    map[string]interface{}{},
			wantNs:  "",
			wantErr: false,
		},
		{
			name:    "namespace is integer, not string",
			args:    map[string]interface{}{"namespace": 123},
			wantErr: true,
		},
		{
			name:    "empty string namespace",
			args:    map[string]interface{}{"namespace": ""},
			wantNs:  "",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractAndValidateNamespace(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errExpect != "" {
					assert.Contains(t, err.Error(), tt.errExpect)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantNs, got)
		})
	}
}
