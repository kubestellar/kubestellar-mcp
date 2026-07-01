package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateNamespace_RFC1123_Empty verifies that empty namespace is rejected
// after the RFC 1123 allowlist change (previously allowed for all-namespaces mode).
func TestValidateNamespace_RFC1123_Empty(t *testing.T) {
	err := ValidateNamespace("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestValidateNamespace_RFC1123_Overlength verifies that namespaces exceeding
// 63 characters are rejected per DNS label length limits.
func TestValidateNamespace_RFC1123_Overlength(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "exactly 64 chars", namespace: strings.Repeat("a", 64)},
		{name: "100 chars", namespace: strings.Repeat("x", 100)},
		{name: "255 chars", namespace: strings.Repeat("n", 255)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "maximum length")
		})
	}
}

// TestValidateNamespace_RFC1123_ExactlyMaxLength verifies 63-char namespace is
// valid (boundary test).
func TestValidateNamespace_RFC1123_ExactlyMaxLength(t *testing.T) {
	ns := strings.Repeat("a", 63)
	err := ValidateNamespace(ns)
	assert.NoError(t, err)
}

// TestValidateNamespace_RFC1123_InvalidChars verifies that non-alphanumeric,
// non-hyphen characters are rejected by the regex.
func TestValidateNamespace_RFC1123_InvalidChars(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "dot", namespace: "my.namespace"},
		{name: "underscore", namespace: "my_namespace"},
		{name: "uppercase", namespace: "MyNamespace"},
		{name: "space", namespace: "my namespace"},
		{name: "slash", namespace: "my/namespace"},
		{name: "colon", namespace: "ns:test"},
		{name: "at sign", namespace: "ns@test"},
		{name: "unicode", namespace: "ñamespace"},
		{name: "starts with hyphen", namespace: "-invalid"},
		{name: "ends with hyphen", namespace: "invalid-"},
		{name: "single hyphen", namespace: "-"},
		{name: "only dot", namespace: "."},
		{name: "semicolon injection", namespace: "ns;echo pwned"},
		{name: "newline injection", namespace: "ns\nmalicious"},
		{name: "null byte", namespace: "ns\x00evil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.Error(t, err, "namespace %q should be rejected", tt.namespace)
			assert.Contains(t, err.Error(), "invalid")
		})
	}
}

// TestValidateNamespace_RFC1123_ValidFormats verifies that valid DNS-1123 label
// namespaces pass validation (assuming they're not blocked).
func TestValidateNamespace_RFC1123_ValidFormats(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "single char", namespace: "a"},
		{name: "single digit", namespace: "0"},
		{name: "alphanumeric", namespace: "my-app-123"},
		{name: "all digits", namespace: "12345"},
		{name: "starts with digit", namespace: "1my-ns"},
		{name: "ends with digit", namespace: "my-ns-2"},
		{name: "hyphen in middle", namespace: "a-b"},
		{name: "multiple hyphens", namespace: "a-b-c-d"},
		{name: "max valid 63 chars", namespace: strings.Repeat("ab", 31) + "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.NoError(t, err, "namespace %q should be valid", tt.namespace)
		})
	}
}

// TestValidateNamespace_RFC1123_CaseRejected verifies that uppercase characters
// are rejected by the RFC 1123 regex (K8s namespaces must be lowercase).
func TestValidateNamespace_RFC1123_CaseRejected(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "Kube-System mixed case", namespace: "Kube-System"},
		{name: "KUBE-SYSTEM upper", namespace: "KUBE-SYSTEM"},
		{name: "OPENSHIFT upper", namespace: "OPENSHIFT"},
		{name: "camelCase", namespace: "myNamespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			assert.Error(t, err, "namespace %q should be rejected (uppercase)", tt.namespace)
		})
	}
}

// TestExtractAndValidateNamespace_RFC1123_EmptyStringRejected verifies that
// an explicit empty string namespace in args is now rejected.
func TestExtractAndValidateNamespace_RFC1123_EmptyStringRejected(t *testing.T) {
	args := map[string]interface{}{"namespace": ""}
	_, err := extractAndValidateNamespace(args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestExtractAndValidateNamespace_RFC1123_InvalidFormat verifies that invalid
// format namespaces are rejected through the extract helper.
func TestExtractAndValidateNamespace_RFC1123_InvalidFormat(t *testing.T) {
	tests := []struct {
		name string
		ns   string
	}{
		{name: "dot separated", ns: "app.prod"},
		{name: "uppercase", ns: "AppProd"},
		{name: "overlength", ns: strings.Repeat("z", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{"namespace": tt.ns}
			_, err := extractAndValidateNamespace(args)
			assert.Error(t, err)
		})
	}
}
