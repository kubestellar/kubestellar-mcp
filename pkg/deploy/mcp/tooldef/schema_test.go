package tooldef

import (
	"reflect"
	"testing"
)

func TestDryRunProperty(t *testing.T) {
	got := DryRunProperty()
	want := map[string]interface{}{
		"type":        "boolean",
		"description": "Preview changes without applying",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DryRunProperty() = %#v, want %#v", got, want)
	}
}

func TestClustersProperty(t *testing.T) {
	got := ClustersProperty("clusters where release exists")
	want := map[string]interface{}{
		"type":        "array",
		"items":       map[string]interface{}{"type": "string"},
		"description": "clusters where release exists",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ClustersProperty(...) = %#v, want %#v", got, want)
	}

	// A different description must produce a different property value,
	// confirming the parameter is actually threaded through.
	other := ClustersProperty("all clusters")
	if reflect.DeepEqual(got, other) {
		t.Fatalf("ClustersProperty with different descriptions produced identical results")
	}
}
