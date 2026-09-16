package gitops

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// TestCheckResourceReturnsErrorOnInvalidAPIVersion covers the
// `if err != nil { return nil, err }` branch of (*DriftDetector).checkResource
// at drift.go:106-108, reached when resolveManifestResource fails because
// schema.ParseGroupVersion rejects the manifest's APIVersion (e.g. more
// than one slash). The existing TestCheckResource matrix always uses
// well-formed APIVersions, leaving this branch uncovered and holding
// checkResource at 94.4%.
func TestCheckResourceReturnsErrorOnInvalidAPIVersion(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	d := &DriftDetector{dynClient: client}

	// "a/b/c" has two slashes — schema.ParseGroupVersion rejects it.
	manifest := Manifest{
		APIVersion: "a/b/c",
		Kind:       "Widget",
		Metadata:   ManifestMetadata{Name: "w", Namespace: "apps"},
		Raw: map[string]interface{}{
			"apiVersion": "a/b/c",
			"kind":       "Widget",
			"metadata":   map[string]interface{}{"name": "w", "namespace": "apps"},
		},
	}

	got, err := d.checkResource(context.Background(), manifest, "alpha")
	if err == nil {
		t.Fatal("checkResource() with unparseable APIVersion should return error, got nil")
	}
	if got != nil {
		t.Errorf("checkResource() drift result on parse error = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "parse apiVersion") {
		t.Errorf("expected parse-apiVersion wrap, got: %v", err)
	}
}
