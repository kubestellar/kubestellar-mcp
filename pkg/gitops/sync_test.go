package gitops

import (
	"context"
	"encoding/json"
	"testing"

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
		name               string
		updatedRV          string
		wantAction         SyncAction
		wantUpdated        int
		wantUnchanged      int
		wantMessageSnippet string
	}{
		{
			name:               "updated resource",
			updatedRV:          "2",
			wantAction:         SyncActionUpdated,
			wantUpdated:        1,
			wantMessageSnippet: "Updated (rv: 1 -> 2)",
		},
		{
			name:               "unchanged resource",
			updatedRV:          "1",
			wantAction:         SyncActionUnchanged,
			wantUnchanged:      1,
			wantMessageSnippet: "No changes",
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
			if summary.Results[0].Message != tt.wantMessageSnippet {
				t.Fatalf("message = %q, want %q", summary.Results[0].Message, tt.wantMessageSnippet)
			}
		})
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
