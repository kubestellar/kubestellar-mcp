package gitops

import (
	"context"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestSyncCreatesResourcesAndTracksSkippedKinds(t *testing.T) {
	syncer := &Syncer{dynClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	manifests := []Manifest{
		testManifest("v1", "ConfigMap", "created", "apps"),
		testManifest("v1", "Secret", "skipped", "apps"),
	}

	summary, err := syncer.Sync(context.Background(), manifests, "alpha", SyncOptions{Exclude: []string{"Secret"}})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if summary.Created != 1 || summary.Skipped != 1 || summary.Updated != 0 || summary.Unchanged != 0 || summary.Failed != 0 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if len(summary.Results) != 2 {
		t.Fatalf("result count = %d, want 2", len(summary.Results))
	}

	created, err := syncer.dynClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("apps").Get(context.Background(), "created", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("created resource lookup error = %v", err)
	}
	if created.GetName() != "created" || created.GetNamespace() != "apps" {
		t.Fatalf("unexpected created object: %#v", created)
	}
	if summary.Results[1].Action != SyncActionSkipped || summary.Results[1].Message != "Kind excluded from sync" {
		t.Fatalf("unexpected skipped result: %#v", summary.Results[1])
	}
}

func TestSyncUpdatesAndDetectsUnchangedResources(t *testing.T) {
	tests := []struct {
		name          string
		updatedRV     string
		wantAction    SyncAction
		wantUpdated   int
		wantUnchanged int
		wantMessage   string
	}{
		{
			name:        "updated resource",
			updatedRV:   "2",
			wantAction:  SyncActionUpdated,
			wantUpdated: 1,
			wantMessage: "Updated (rv: 1 -> 2)",
		},
		{
			name:          "unchanged resource",
			updatedRV:     "1",
			wantAction:    SyncActionUnchanged,
			wantUnchanged: 1,
			wantMessage:   "No changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := testManifestObject("ConfigMap", "demo", "apps", "1")
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
			client.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, ok := action.(k8stesting.PatchAction)
				if !ok {
					t.Fatalf("patch action type = %T, want PatchAction", action)
				}
				if patchAction.GetPatchType() != types.ApplyPatchType {
					t.Fatalf("patch type = %v, want %v", patchAction.GetPatchType(), types.ApplyPatchType)
				}

				var raw map[string]interface{}
				if err := json.Unmarshal(patchAction.GetPatch(), &raw); err != nil {
					t.Fatalf("json.Unmarshal() error = %v", err)
				}
				updated := &unstructured.Unstructured{Object: raw}
				updated.SetResourceVersion(tt.updatedRV)
				return true, updated, nil
			})

			summary, err := (&Syncer{dynClient: client}).Sync(context.Background(), []Manifest{testManifest("v1", "ConfigMap", "demo", "apps")}, "alpha", SyncOptions{})
			if err != nil {
				t.Fatalf("Sync() error = %v", err)
			}
			if summary.Updated != tt.wantUpdated || summary.Unchanged != tt.wantUnchanged {
				t.Fatalf("unexpected summary counts: %#v", summary)
			}
			if len(summary.Results) != 1 {
				t.Fatalf("result count = %d, want 1", len(summary.Results))
			}
			if summary.Results[0].Action != tt.wantAction {
				t.Fatalf("action = %q, want %q", summary.Results[0].Action, tt.wantAction)
			}
			if summary.Results[0].Message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", summary.Results[0].Message, tt.wantMessage)
			}
		})
	}
}

func TestSyncDryRunPatchesExistingResourcesAndDetectsUnchanged(t *testing.T) {
	tests := []struct {
		name          string
		existingValue string
		wantAction    SyncAction
		wantUpdated   int
		wantUnchanged int
		wantMessage   string
	}{
		{
			name:          "would update",
			existingValue: "old",
			wantAction:    SyncActionUpdated,
			wantUpdated:   1,
			wantMessage:   "Would update (dry-run)",
		},
		{
			name:          "unchanged",
			existingValue: "value",
			wantAction:    SyncActionUnchanged,
			wantUnchanged: 1,
			wantMessage:   "No changes (dry-run)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := testManifestObject("ConfigMap", "demo", "apps", "1")
			existing.Object["data"].(map[string]interface{})["key"] = tt.existingValue
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
			patchCalled := false
			client.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchCalled = true
				patchAction, ok := action.(k8stesting.PatchAction)
				if !ok {
					t.Fatalf("patch action type = %T, want PatchAction", action)
				}
				if patchAction.GetPatchType() != types.ApplyPatchType {
					t.Fatalf("patch type = %v, want %v", patchAction.GetPatchType(), types.ApplyPatchType)
				}
				patchOptionsGetter, ok := action.(interface {
					GetPatchOptions() metav1.PatchOptions
				})
				if !ok {
					t.Fatalf("patch action type = %T, want GetPatchOptions", action)
				}
				patchOpts := patchOptionsGetter.GetPatchOptions()
				if patchOpts.FieldManager != "kubestellar-deploy" {
					t.Fatalf("field manager = %q, want kubestellar-deploy", patchOpts.FieldManager)
				}
				if patchOpts.Force == nil || !*patchOpts.Force {
					t.Fatalf("force option = %v, want true", patchOpts.Force)
				}
				if len(patchOpts.DryRun) != 1 || patchOpts.DryRun[0] != metav1.DryRunAll {
					t.Fatalf("dry-run options = %v, want [%s]", patchOpts.DryRun, metav1.DryRunAll)
				}

				var raw map[string]interface{}
				if err := json.Unmarshal(patchAction.GetPatch(), &raw); err != nil {
					t.Fatalf("json.Unmarshal() error = %v", err)
				}
				updated := &unstructured.Unstructured{Object: raw}
				updated.SetResourceVersion("1")
				return true, updated, nil
			})

			summary, err := (&Syncer{dynClient: client}).Sync(context.Background(), []Manifest{testManifest("v1", "ConfigMap", "demo", "apps")}, "alpha", SyncOptions{DryRun: true})
			if err != nil {
				t.Fatalf("Sync() error = %v", err)
			}
			if !patchCalled {
				t.Fatal("expected dry-run sync to patch existing resource")
			}
			if summary.Updated != tt.wantUpdated || summary.Unchanged != tt.wantUnchanged {
				t.Fatalf("unexpected summary counts: %#v", summary)
			}
			if len(summary.Results) != 1 {
				t.Fatalf("result count = %d, want 1", len(summary.Results))
			}
			if summary.Results[0].Action != tt.wantAction {
				t.Fatalf("action = %q, want %q", summary.Results[0].Action, tt.wantAction)
			}
			if summary.Results[0].Message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", summary.Results[0].Message, tt.wantMessage)
			}
		})
	}
}

func TestSyncObjectsEqualIgnoresOnlyKubernetesManagedPaths(t *testing.T) {
	current := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "demo",
			"resourceVersion": "1",
			"managedFields":   []interface{}{map[string]interface{}{"manager": "other"}},
		},
		"data": map[string]interface{}{
			"status":     "ready",
			"generation": "1",
		},
		"status": map[string]interface{}{
			"observedGeneration": float64(1),
		},
	}
	dryRun := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "demo",
			"resourceVersion": "2",
			"managedFields":   []interface{}{map[string]interface{}{"manager": "kubestellar-deploy"}},
		},
		"data": map[string]interface{}{
			"status":     "ready",
			"generation": "1",
		},
		"status": map[string]interface{}{
			"observedGeneration": float64(2),
		},
	}

	if !syncObjectsEqual(current, dryRun) {
		t.Fatal("syncObjectsEqual() should ignore Kubernetes-managed metadata and top-level status changes")
	}

	dryRun["data"].(map[string]interface{})["generation"] = "2"
	if syncObjectsEqual(current, dryRun) {
		t.Fatal("syncObjectsEqual() should not ignore user data keys named like Kubernetes-managed fields")
	}

	currentPod := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"spec": map[string]interface{}{
			"nodeName": "node-a",
		},
	}
	dryRunPod := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"spec": map[string]interface{}{
			"nodeName": "node-b",
		},
	}
	if syncObjectsEqual(currentPod, dryRunPod) {
		t.Fatal("syncObjectsEqual() should not ignore spec.nodeName changes")
	}
}

func TestShouldSyncHonorsIncludeAndExclude(t *testing.T) {
	syncer := &Syncer{}
	if syncer.shouldSync("Secret", SyncOptions{Exclude: []string{"Secret"}}) {
		t.Fatal("expected excluded kind to be skipped")
	}
	if !syncer.shouldSync("ConfigMap", SyncOptions{Include: []string{"ConfigMap"}}) {
		t.Fatal("expected explicitly included kind to sync")
	}
	if syncer.shouldSync("Secret", SyncOptions{Include: []string{"ConfigMap"}}) {
		t.Fatal("expected non-included kind to be skipped")
	}
}

func TestSyncerGetGVRUsesRESTMapper(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "example.io", Version: "v1alpha1"}})
	mapper.AddSpecific(
		schema.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widget"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgetz"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widget"},
		meta.RESTScopeRoot,
	)

	syncer := &Syncer{restMapper: mapper}
	gvr, err := syncer.getGVR(Manifest{APIVersion: "example.io/v1alpha1", Kind: "Widget"})
	if err != nil {
		t.Fatalf("getGVR() unexpected error: %v", err)
	}
	if gvr.Resource != "widgetz" {
		t.Fatalf("getGVR() resource = %q, want widgetz", gvr.Resource)
	}
}

func TestSyncIgnoresNamespaceOverrideForClusterScopedMapping(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "example.io", Version: "v1alpha1"}})
	mapper.AddSpecific(
		schema.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widget"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgetz"},
		schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widget"},
		meta.RESTScopeRoot,
	)

	syncer := &Syncer{
		dynClient:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		restMapper: mapper,
	}
	manifest := Manifest{
		APIVersion: "example.io/v1alpha1",
		Kind:       "Widget",
		Metadata:   ManifestMetadata{Name: "demo"},
		Raw: map[string]interface{}{
			"apiVersion": "example.io/v1alpha1",
			"kind":       "Widget",
			"metadata": map[string]interface{}{
				"name": "demo",
			},
		},
	}

	summary, err := syncer.Sync(context.Background(), []Manifest{manifest}, "alpha", SyncOptions{DryRun: true, Namespace: "overridden"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(summary.Results))
	}
	if summary.Results[0].Namespace != "" {
		t.Fatalf("result namespace = %q, want empty for cluster-scoped resource", summary.Results[0].Namespace)
	}
}

func testManifest(apiVersion, kind, name, namespace string) Manifest {
	raw := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}
	if kind == "ConfigMap" {
		raw["data"] = map[string]interface{}{"key": "value"}
	}
	if kind == "Secret" {
		raw["data"] = map[string]interface{}{"token": "abcd"}
	}
	return Manifest{
		APIVersion: apiVersion,
		Kind:       kind,
		Metadata: ManifestMetadata{
			Name:      name,
			Namespace: namespace,
		},
		Raw: raw,
	}
}

func testManifestObject(kind, name, namespace, resourceVersion string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: testManifest("v1", kind, name, namespace).Raw}
	obj.SetResourceVersion(resourceVersion)
	return obj
}
