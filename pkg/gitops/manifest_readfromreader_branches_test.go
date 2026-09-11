package gitops

import (
	"io"
	"strings"
	"testing"
)

// TestReadFromReader_DecodeErrorReturnsError covers the non-EOF error branch
// of ReadFromReader: when the underlying YAMLOrJSONDecoder cannot parse the
// input, ReadFromReader must surface the error and NOT return a partial
// slice. The existing suite only exercises the EOF and skip-when-Kind-empty
// paths.
func TestReadFromReader_DecodeErrorReturnsError(t *testing.T) {
	reader := NewManifestReader()

	// Multi-line YAML followed by a nested structure that violates the
	// document form the decoder expects — enough to force a non-EOF Decode
	// error. Using a top-level YAML mapping followed by intentionally
	// broken indentation ensures the error surfaces from Decode.
	data := strings.NewReader("apiVersion: v1\nkind: ConfigMap\n\tmetadata:\n\t\tname: bad\n")

	manifests, err := reader.ReadFromReader(data)
	if err == nil {
		t.Fatalf("ReadFromReader() error = nil, want decode error; manifests=%#v", manifests)
	}
	if manifests != nil {
		t.Fatalf("ReadFromReader() manifests = %#v, want nil on decode error", manifests)
	}
	if err == io.EOF {
		t.Fatalf("ReadFromReader() unexpectedly returned io.EOF; wanted a real decode error")
	}
}

// TestReadFromReader_NullDocumentIsSkipped covers the `if raw == nil { continue }`
// branch of ReadFromReader by feeding an explicit YAML `null` document
// between two real manifests. Decode fills `raw` with nil (not an empty map)
// for a literal null; a bare `---\n` separator alone also produces a nil
// document. The reader must skip these without erroring and without
// emitting a Manifest entry.
func TestReadFromReader_NullDocumentIsSkipped(t *testing.T) {
	reader := NewManifestReader()
	data := strings.NewReader(`apiVersion: v1
kind: ConfigMap
metadata:
  name: first
---
null
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: second
`)

	manifests, err := reader.ReadFromReader(data)
	if err != nil {
		t.Fatalf("ReadFromReader() unexpected error: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("ReadFromReader() len = %d, want 2 (null doc must be skipped, not counted or errored)", len(manifests))
	}
	if manifests[0].Metadata.Name != "first" || manifests[1].Metadata.Name != "second" {
		t.Fatalf("unexpected manifests: %#v", manifests)
	}
}

// TestReadFromReader_EmptyInputReturnsNoManifests exercises the immediate-EOF
// path (Decode returns io.EOF on first iteration → break → return nil, nil).
// The existing suite only exercises EOF at the *end* of a multi-doc stream;
// this covers EOF at the *start*, which is the common "empty file" case.
func TestReadFromReader_EmptyInputReturnsNoManifests(t *testing.T) {
	reader := NewManifestReader()

	manifests, err := reader.ReadFromReader(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ReadFromReader(empty) unexpected error: %v", err)
	}
	if len(manifests) != 0 {
		t.Fatalf("ReadFromReader(empty) len = %d, want 0", len(manifests))
	}
}
