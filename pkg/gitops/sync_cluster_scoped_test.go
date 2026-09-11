package gitops

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The cluster-scoped write branches of Syncer.syncResource previously had no
// dedicated coverage: sync_test.go's cluster-scoped test exercises only the
// pre-Create dry-run early-return, and sync_error_branches_test.go's Create /
// Patch tests all use ConfigMap (namespace-scoped). That left three
// cluster-scoped code paths at 0 hits in the coverage profile:
//
//   - sync.go:191-192  ClusterScoped Create (non-dry-run)
//   - sync.go:221-223  ClusterScoped dry-run Patch
//   - sync.go:241-246  ClusterScoped non-dry-run Patch
//
// plus one namespace-override arm inside Sync's main loop:
//
//   - sync.go:117      opts.Namespace override of manifest.GetNamespace()
//
// Each test below drives exactly one of those arms. Widgets are registered
// through a RESTMapper that reports RESTScopeRoot so resolveManifestResource
// classifies them as ClusterScoped without relying on the static
// IsClusterScoped list.

func clusterScopedWidgetMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "example.io", Version: "v1alpha1"}})
	m.AddSpecific(
		schema.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widget"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgetz"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widget"},
		meta.RESTScopeRoot,
	)
	return m
}

func newClusterScopedWidgetManifest(name string) Manifest {
	raw := map[string]interface{}{
		"apiVersion": "example.io/v1alpha1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	return Manifest{
		APIVersion: "example.io/v1alpha1",
		Kind:       "Widget",
		Metadata:   ManifestMetadata{Name: name},
		Raw:        raw,
	}
}

// TestSyncClusterScopedCreatesResource covers the ClusterScoped Create branch
// (sync.go:191-192): DryRun=false, no existing object, mapping.ClusterScoped
// is true, so the syncer takes the .Resource(GVR).Create(...) arm rather than
// .Resource(GVR).Namespace(ns).Create(...).
func TestSyncClusterScopedCreatesResource(t *testing.T) {
	syncer := &Syncer{
		dynClient:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		restMapper: clusterScopedWidgetMapper(),
	}
	manifest := newClusterScopedWidgetManifest("cluster-widget")

	summary, err := syncer.Sync(context.Background(), []Manifest{manifest}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if summary.Created != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if summary.Results[0].Namespace != "" {
		t.Fatalf("cluster-scoped result namespace = %q, want empty", summary.Results[0].Namespace)
	}

	// Fake client should now hold the resource at the cluster scope (no ns).
	gvr := schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgetz"}
	got, err := syncer.dynClient.Resource(gvr).Get(context.Background(), "cluster-widget", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("cluster-scoped Get after create: %v", err)
	}
	if got.GetName() != "cluster-widget" {
		t.Fatalf("got name = %q", got.GetName())
	}
	if got.GetNamespace() != "" {
		t.Fatalf("cluster-scoped object namespace = %q, want empty", got.GetNamespace())
	}
}

// TestSyncClusterScopedDryRunPatchesExisting covers the ClusterScoped dry-run
// Patch branch (sync.go:221-223): DryRun=true, existing object at the cluster
// scope, mapping.ClusterScoped is true, so the syncer takes the
// .Resource(GVR).Patch(...) arm inside the `if dryRun {` block rather than
// the namespace-qualified one.
func TestSyncClusterScopedDryRunPatchesExisting(t *testing.T) {
	raw := map[string]interface{}{
		"apiVersion": "example.io/v1alpha1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name": "cluster-widget",
		},
	}
	existing := &unstructured.Unstructured{Object: raw}
	existing.SetResourceVersion("1")

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	// Make Get return the existing object at cluster scope; register a Patch
	// reactor to assert the code took the ClusterScoped dry-run Patch arm.
	client.PrependReactor("get", "widgetz", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "" {
			t.Fatalf("cluster-scoped Get was routed with namespace %q", action.GetNamespace())
		}
		return true, existing, nil
	})
	var sawClusterScopedPatch bool
	client.PrependReactor("patch", "widgetz", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "" {
			t.Fatalf("cluster-scoped dry-run patch was routed with namespace %q", action.GetNamespace())
		}
		sawClusterScopedPatch = true
		updated := &unstructured.Unstructured{Object: raw}
		updated.SetResourceVersion("1") // unchanged
		return true, updated, nil
	})

	syncer := &Syncer{dynClient: client, restMapper: clusterScopedWidgetMapper()}
	manifest := newClusterScopedWidgetManifest("cluster-widget")

	summary, err := syncer.Sync(context.Background(), []Manifest{manifest}, "alpha", SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if summary.Failed != 0 {
		t.Fatalf("failed count = %d, want 0 (summary=%#v)", summary.Failed, summary)
	}
	if !sawClusterScopedPatch {
		t.Fatal("expected cluster-scoped Patch reactor to fire")
	}
	if summary.Unchanged != 1 {
		t.Fatalf("expected Unchanged=1, got %#v", summary)
	}
	if summary.Results[0].Namespace != "" {
		t.Fatalf("cluster-scoped result namespace = %q, want empty", summary.Results[0].Namespace)
	}
}

// TestSyncClusterScopedPatchesExisting covers the ClusterScoped non-dry-run
// Patch branch (sync.go:241-246): DryRun=false, existing object at the cluster
// scope, mapping.ClusterScoped is true, so the syncer takes the
// .Resource(GVR).Patch(...) arm at the end of syncResource rather than the
// namespace-qualified one.
func TestSyncClusterScopedPatchesExisting(t *testing.T) {
	raw := map[string]interface{}{
		"apiVersion": "example.io/v1alpha1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name": "cluster-widget",
		},
	}
	existing := &unstructured.Unstructured{Object: raw}
	existing.SetResourceVersion("1")

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", "widgetz", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "" {
			t.Fatalf("cluster-scoped Get was routed with namespace %q", action.GetNamespace())
		}
		return true, existing, nil
	})
	var sawClusterScopedPatch bool
	client.PrependReactor("patch", "widgetz", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "" {
			t.Fatalf("cluster-scoped patch was routed with namespace %q", action.GetNamespace())
		}
		sawClusterScopedPatch = true
		updated := &unstructured.Unstructured{Object: raw}
		updated.SetResourceVersion("2")
		return true, updated, nil
	})

	syncer := &Syncer{dynClient: client, restMapper: clusterScopedWidgetMapper()}
	manifest := newClusterScopedWidgetManifest("cluster-widget")

	summary, err := syncer.Sync(context.Background(), []Manifest{manifest}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if summary.Failed != 0 {
		t.Fatalf("failed count = %d, want 0 (summary=%#v)", summary.Failed, summary)
	}
	if !sawClusterScopedPatch {
		t.Fatal("expected cluster-scoped Patch reactor to fire")
	}
	if summary.Updated+summary.Unchanged != 1 {
		t.Fatalf("expected one Updated/Unchanged result, got %#v", summary)
	}
	if summary.Results[0].Namespace != "" {
		t.Fatalf("cluster-scoped result namespace = %q, want empty", summary.Results[0].Namespace)
	}
}

// TestSyncNamespaceOverrideAppliesToNamespaceScopedResource covers
// sync.go:117 — the `namespace = opts.Namespace` override arm inside the
// Sync loop when mapping is namespace-scoped. Existing sync tests either use
// a matching source namespace (no override effect) or exercise cluster-scoped
// mappings (which are supposed to ignore the override). Neither path drives
// the override assignment itself for a namespace-scoped kind.
func TestSyncNamespaceOverrideAppliesToNamespaceScopedResource(t *testing.T) {
	syncer := &Syncer{dynClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	// Manifest declares namespace "source-ns"; the override "override-ns"
	// must win when passed via SyncOptions.Namespace.
	m := testManifest("v1", "ConfigMap", "cm", "source-ns")

	summary, err := syncer.Sync(context.Background(), []Manifest{m}, "alpha", SyncOptions{Namespace: "override-ns"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if summary.Created != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if summary.Results[0].Namespace != "override-ns" {
		t.Fatalf("result namespace = %q, want override-ns", summary.Results[0].Namespace)
	}

	// The object must live at override-ns on the fake client, and no
	// residual object should exist at the manifest's declared source-ns.
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	if _, err := syncer.dynClient.Resource(gvr).Namespace("override-ns").Get(context.Background(), "cm", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected object at override-ns: %v", err)
	}
	if _, err := syncer.dynClient.Resource(gvr).Namespace("source-ns").Get(context.Background(), "cm", metav1.GetOptions{}); err == nil {
		t.Fatalf("unexpected object at source-ns; override was ignored")
	}
}
