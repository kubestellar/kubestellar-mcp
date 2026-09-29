package cluster

import (
	"testing"
)

func TestTools_ExpectedNames(t *testing.T) {
	got := Tools()
	if len(got) != 2 {
		t.Fatalf("Tools() len = %d, want 2", len(got))
	}
	want := []string{"list_clusters", "get_cluster_health"}
	for i, name := range want {
		if got[i].Schema.Name != name {
			t.Errorf("Tools()[%d].Schema.Name = %q, want %q", i, got[i].Schema.Name, name)
		}
		if got[i].Schema.Description == "" {
			t.Errorf("Tools()[%d] (%s) has empty description", i, name)
		}
		if got[i].Handler == nil {
			t.Errorf("Tools()[%d] (%s) has nil handler", i, name)
		}
	}
}

func TestTools_ListClustersSourceEnum(t *testing.T) {
	for _, td := range Tools() {
		if td.Schema.Name != "list_clusters" {
			continue
		}
		src, ok := td.Schema.InputSchema.Properties["source"]
		if !ok {
			t.Fatal("list_clusters missing source property")
		}
		if len(src.Enum) != 3 {
			t.Fatalf("source.Enum = %v, want 3 values", src.Enum)
		}
		wantVals := map[string]bool{"all": true, "kubeconfig": true, "kubestellar": true}
		for _, v := range src.Enum {
			if !wantVals[v] {
				t.Errorf("unexpected enum value %q", v)
			}
			delete(wantVals, v)
		}
		if len(wantVals) != 0 {
			t.Errorf("missing enum values: %v", wantVals)
		}
		return
	}
	t.Fatal("list_clusters not found in Tools()")
}
