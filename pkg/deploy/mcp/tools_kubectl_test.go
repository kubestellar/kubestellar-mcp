package mcp

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetGVR(t *testing.T) {
	tests := []struct {
		kind       string
		resource   string
		namespaced bool
	}{
		{kind: "Deployment", resource: "deployments", namespaced: true},
		{kind: "namespaces", resource: "namespaces", namespaced: false},
		{kind: "hpa", resource: "horizontalpodautoscalers", namespaced: true},
		{kind: "Widget", resource: "", namespaced: false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			gvr, namespaced := getGVR(tt.kind)
			if namespaced != tt.namespaced {
				t.Fatalf("getGVR(%q) namespaced = %v, want %v", tt.kind, namespaced, tt.namespaced)
			}
			if gvr.Resource != tt.resource {
				t.Fatalf("getGVR(%q) = %#v, want resource=%q", tt.kind, gvr, tt.resource)
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
