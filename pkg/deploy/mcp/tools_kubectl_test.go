package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetGVR(t *testing.T) {
	tests := []struct {
		kind       string
		resource   string
		namespaced bool
		found      bool
	}{
		{kind: "Deployment", resource: "deployments", namespaced: true, found: true},
		{kind: "namespaces", resource: "namespaces", namespaced: false, found: true},
		{kind: "hpa", resource: "horizontalpodautoscalers", namespaced: true, found: true},
		{kind: "Widget", found: false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			gvr, namespaced, found := getGVR(tt.kind)
			if found != tt.found {
				t.Fatalf("getGVR(%q) found = %v, want %v", tt.kind, found, tt.found)
			}
			if found {
				if gvr.Resource != tt.resource || namespaced != tt.namespaced {
					t.Fatalf("getGVR(%q) = (%#v, %v), want resource=%q namespaced=%v", tt.kind, gvr, namespaced, tt.resource, tt.namespaced)
				}
			}
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

func TestParseYAMLRejectsYAMLInput(t *testing.T) {
	var parsed map[string]interface{}
	err := parseYAML([]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: demo\n"), &parsed)
	if err == nil || !strings.Contains(err.Error(), "use JSON format or deploy_app tool") {
		t.Fatalf("parseYAML() error = %v, want YAML unsupported error", err)
	}
}
