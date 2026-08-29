package mcp

import (
	"strings"
	"testing"
)

// TestParseYAMLReturnsYAMLToJSONError covers the first error branch of
// parseYAML — when yamlToJSONBytes (k8syaml.ToJSON) rejects malformed
// YAML. The existing test only exercises the happy path, leaving both
// error returns uncovered.
func TestParseYAMLReturnsYAMLToJSONError(t *testing.T) {
	var out map[string]interface{}
	// Unclosed flow mapping — invalid YAML the parser cannot convert.
	err := parseYAML([]byte("{unclosed: ["), &out)
	if err == nil {
		t.Fatal("parseYAML(<invalid yaml>, ...) returned nil, want error")
	}
}

// TestParseYAMLReturnsUnmarshalError covers the second error branch —
// when yamlToJSONBytes succeeds (the YAML is well-formed) but the
// resulting JSON cannot be unmarshalled into the caller's target type.
// Here we hand a YAML mapping to a []string target so json.Unmarshal
// returns a type error.
func TestParseYAMLReturnsUnmarshalError(t *testing.T) {
	var out []string
	err := parseYAML([]byte("apiVersion: v1\nkind: Pod\n"), &out)
	if err == nil {
		t.Fatal("parseYAML(mapping into []string) returned nil, want unmarshal error")
	}
	// The k8s YAML→JSON path funnels through encoding/json; its type
	// errors carry the "cannot unmarshal" phrase. Guarding on it here
	// keeps the test from silently passing on a future refactor that
	// bubbles a different (e.g., yaml-side) error out of parseYAML.
	if !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("parseYAML() error = %v, want json unmarshal error", err)
	}
}
