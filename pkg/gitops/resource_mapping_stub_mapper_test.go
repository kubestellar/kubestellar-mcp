package gitops

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// stubRESTMapper implements meta.RESTMapper for testing resolveManifestResource's
// mapper != nil arms. Only the RESTMapping method is exercised by production code;
// the rest satisfy the interface for compile-time correctness.
type stubRESTMapper struct {
	// RESTMappingFn is invoked when RESTMapping is called. When nil, RESTMapping
	// returns a "no match" error (exercising the fallback-to-static branch).
	RESTMappingFn func(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error)
}

func (s stubRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if s.RESTMappingFn != nil {
		return s.RESTMappingFn(gk, versions...)
	}
	return nil, errors.New("stub: no match")
}

func (stubRESTMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errors.New("not implemented")
}
func (stubRESTMapper) KindsFor(schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, errors.New("not implemented")
}
func (stubRESTMapper) ResourceFor(schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, errors.New("not implemented")
}
func (stubRESTMapper) ResourcesFor(schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, errors.New("not implemented")
}
func (stubRESTMapper) RESTMappings(schema.GroupKind, ...string) ([]*meta.RESTMapping, error) {
	return nil, errors.New("not implemented")
}
func (stubRESTMapper) ResourceSingularizer(string) (string, error) {
	return "", errors.New("not implemented")
}

// TestResolveManifestResource_MapperSuccessReturnsMapperGVR exercises the
// `mapper != nil` + `err == nil` arm at pkg/gitops/resource_mapping.go:41-46.
// When the mapper returns a valid RESTMapping, resolveManifestResource must
// use its GVR + Scope rather than falling back to the static kindToResource
// / IsClusterScoped tables. All existing tests pass a nil mapper, so this
// arm was previously untested.
func TestResolveManifestResource_MapperSuccessReturnsMapperGVR(t *testing.T) {
	// Static tables would resolve custom.io/v1/Widget -> "widgets" +
	// non-cluster-scoped. Force the mapper to return a distinct GVR
	// ("frobs") + cluster-scoped so we can assert the mapper won.
	mapper := stubRESTMapper{
		RESTMappingFn: func(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
			if gk.Group != "custom.io" || gk.Kind != "Widget" {
				t.Fatalf("unexpected GroupKind: %+v", gk)
			}
			if len(versions) != 1 || versions[0] != "v1" {
				t.Fatalf("unexpected versions: %v", versions)
			}
			return &meta.RESTMapping{
				Resource: schema.GroupVersionResource{
					Group: "custom.io", Version: "v1", Resource: "frobs",
				},
				Scope: meta.RESTScopeRoot,
			}, nil
		},
	}

	manifest := Manifest{
		APIVersion: "custom.io/v1",
		Kind:       "Widget",
		Metadata:   ManifestMetadata{Name: "w1"},
	}
	mapping, err := resolveManifestResource(manifest, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.GVR.Resource != "frobs" {
		t.Fatalf("GVR.Resource = %q, want %q (mapper output)", mapping.GVR.Resource, "frobs")
	}
	if mapping.GVR.Group != "custom.io" || mapping.GVR.Version != "v1" {
		t.Fatalf("unexpected GVR: %+v", mapping.GVR)
	}
	if !mapping.ClusterScoped {
		t.Fatal("ClusterScoped = false, want true (mapper.Scope = RESTScopeRoot)")
	}
}

// TestResolveManifestResource_MapperSuccessNamespacedScope covers the
// `mapping.Scope != nil && mapping.Scope.Name() == RESTScopeNameRoot` boolean
// with a false-arm (namespaced scope).
func TestResolveManifestResource_MapperSuccessNamespacedScope(t *testing.T) {
	mapper := stubRESTMapper{
		RESTMappingFn: func(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
			return &meta.RESTMapping{
				Resource: schema.GroupVersionResource{
					Group: "", Version: "v1", Resource: "configmaps",
				},
				Scope: meta.RESTScopeNamespace,
			}, nil
		},
	}

	mapping, err := resolveManifestResource(Manifest{
		APIVersion: "v1", Kind: "ConfigMap",
		Metadata: ManifestMetadata{Name: "cm"},
	}, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.ClusterScoped {
		t.Fatal("ClusterScoped = true, want false (namespace-scoped mapping)")
	}
}

// TestResolveManifestResource_MapperFailsFallsBackToStatic exercises the
// `mapper != nil` + `err != nil` arm at pkg/gitops/resource_mapping.go:49-51.
// When the mapper cannot resolve the kind, resolveManifestResource must
// still succeed by falling back to the static kindToResource +
// IsClusterScoped tables.
func TestResolveManifestResource_MapperFailsFallsBackToStatic(t *testing.T) {
	mapper := stubRESTMapper{
		RESTMappingFn: func(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
			return nil, errors.New("no kind registered")
		},
	}

	// Deployment is in the built-in static kind table:
	// kindToResource("Deployment") == "deployments", IsClusterScoped == false.
	manifest := Manifest{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata:   ManifestMetadata{Name: "d1", Namespace: "default"},
	}
	mapping, err := resolveManifestResource(manifest, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.GVR.Resource != "deployments" {
		t.Fatalf("GVR.Resource = %q, want \"deployments\" (static fallback)", mapping.GVR.Resource)
	}
	if mapping.GVR.Group != "apps" || mapping.GVR.Version != "v1" {
		t.Fatalf("unexpected GVR: %+v", mapping.GVR)
	}
	if mapping.ClusterScoped {
		t.Fatal("ClusterScoped = true, want false (Deployment is namespaced)")
	}
}
