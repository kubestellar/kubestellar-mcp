package gitops

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestReadFromReaderDecodeError exercises the decoder-error branch in
// ReadFromReader (manifest.go line ~290) — a non-EOF error from the YAML/JSON
// decoder should surface as a non-nil error return. The existing test only
// covers the happy path.
func TestReadFromReaderDecodeError(t *testing.T) {
	reader := NewManifestReader()
	// Truly malformed YAML — unclosed flow mapping — causes the decoder to
	// return a non-EOF error mid-stream.
	bad := strings.NewReader("apiVersion: v1\nkind: ConfigMap\ndata: {unterminated\n")

	manifests, err := reader.ReadFromReader(bad)
	if err == nil {
		t.Fatalf("ReadFromReader() expected error for malformed YAML, got manifests=%#v", manifests)
	}
	if manifests != nil {
		t.Fatalf("ReadFromReader() expected nil manifests on decode error, got %d", len(manifests))
	}
}

// TestCompareManifestsSpecAndDataMissing exercises the two "missing in cluster"
// branches in compareManifests (drift.go ~168 and ~179) — when the git
// manifest declares a spec/data map but the live cluster object omits that
// field entirely. The existing TestCompareManifests only hits "data missing";
// this adds explicit spec-missing coverage and the both-missing case.
func TestCompareManifestsSpecAndDataMissing(t *testing.T) {
	d := &DriftDetector{}
	git := Manifest{
		Kind: "ConfigMap",
		Metadata: ManifestMetadata{
			Name: "demo",
		},
		Spec: map[string]interface{}{"replicas": float64(3)},
		Data: map[string]interface{}{"config": "expected"},
	}
	// Cluster object has neither spec nor data.
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "demo"},
	}}

	diffs := d.compareManifests(git, cluster)

	var sawSpec, sawData bool
	for _, diff := range diffs {
		if diff == "spec: missing in cluster" {
			sawSpec = true
		}
		if diff == "data: missing in cluster" {
			sawData = true
		}
	}
	if !sawSpec {
		t.Fatalf("compareManifests() missing 'spec: missing in cluster' diff, got %#v", diffs)
	}
	if !sawData {
		t.Fatalf("compareManifests() missing 'data: missing in cluster' diff, got %#v", diffs)
	}
}
