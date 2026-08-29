package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestSyncRecordsFailureForInvalidAPIVersion covers the resolveManifestResource
// error branch in Sync (previously uncovered): a malformed apiVersion causes
// mapping resolution to fail, and the summary must record a Failed result with
// the "failed to resolve resource mapping" message rather than aborting.
func TestSyncRecordsFailureForInvalidAPIVersion(t *testing.T) {
	syncer := &Syncer{dynClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	bad := Manifest{
		APIVersion: "a/b/c", // ParseGroupVersion rejects >1 slash
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "broken", Namespace: "apps"},
		Raw: map[string]interface{}{
			"apiVersion": "a/b/c",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "broken", "namespace": "apps"},
		},
	}

	summary, err := syncer.Sync(context.Background(), []Manifest{bad}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if summary.Failed != 1 || summary.Created != 0 || summary.Updated != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(summary.Results))
	}
	got := summary.Results[0]
	if got.Action != SyncActionFailed {
		t.Fatalf("action = %q, want %q", got.Action, SyncActionFailed)
	}
	if got.Cluster != "alpha" || got.Namespace != "apps" {
		t.Fatalf("unexpected cluster/namespace: %#v", got)
	}
	if !strings.Contains(got.Message, "failed to resolve resource mapping") {
		t.Fatalf("message = %q, want to contain %q", got.Message, "failed to resolve resource mapping")
	}
}

// TestSyncRecordsFailureForGetError covers the non-NotFound Get() error branch
// in syncResource: a transient API error must surface as a Failed result with
// the "failed to get resource" prefix, and Sync must continue processing.
func TestSyncRecordsFailureForGetError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	syncer := &Syncer{dynClient: client}

	summary, err := syncer.Sync(context.Background(),
		[]Manifest{testManifest("v1", "ConfigMap", "cm", "apps")}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if summary.Failed != 1 {
		t.Fatalf("failed count = %d, want 1", summary.Failed)
	}
	if !strings.Contains(summary.Results[0].Message, "failed to get resource") {
		t.Fatalf("message = %q", summary.Results[0].Message)
	}
}

// TestSyncRecordsFailureForCreateError covers the Create() error branch:
// Get() returns NotFound (default fake behavior), then Create() fails and
// the result must carry "failed to create".
func TestSyncRecordsFailureForCreateError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("create-blocked")
	})
	syncer := &Syncer{dynClient: client}

	summary, err := syncer.Sync(context.Background(),
		[]Manifest{testManifest("v1", "ConfigMap", "cm", "apps")}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if summary.Failed != 1 || summary.Created != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if !strings.Contains(summary.Results[0].Message, "failed to create") {
		t.Fatalf("message = %q", summary.Results[0].Message)
	}
}

// TestSyncRecordsFailureForPatchError covers the non-dry-run Patch() error
// branch: the resource already exists but SSA patch fails.
func TestSyncRecordsFailureForPatchError(t *testing.T) {
	existing := testManifestObject("ConfigMap", "cm", "apps", "1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	client.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch-blocked")
	})
	syncer := &Syncer{dynClient: client}

	summary, err := syncer.Sync(context.Background(),
		[]Manifest{testManifest("v1", "ConfigMap", "cm", "apps")}, "alpha", SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if summary.Failed != 1 || summary.Updated != 0 || summary.Unchanged != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if !strings.Contains(summary.Results[0].Message, "failed to update") {
		t.Fatalf("message = %q", summary.Results[0].Message)
	}
}

// TestSyncRecordsFailureForDryRunPatchError covers the dry-run Patch error
// branch: dryRun=true but the API rejects the SSA dry-run patch.
func TestSyncRecordsFailureForDryRunPatchError(t *testing.T) {
	existing := testManifestObject("ConfigMap", "cm", "apps", "1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	client.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("dry-run-blocked")
	})
	syncer := &Syncer{dynClient: client}

	summary, err := syncer.Sync(context.Background(),
		[]Manifest{testManifest("v1", "ConfigMap", "cm", "apps")}, "alpha", SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if summary.Failed != 1 {
		t.Fatalf("failed count = %d, want 1", summary.Failed)
	}
	if !strings.Contains(summary.Results[0].Message, "failed dry-run check") {
		t.Fatalf("message = %q", summary.Results[0].Message)
	}
}

// TestSyncCreatesInDryRunWithoutHittingCreate covers the dry-run create
// branch: when Get() returns NotFound and DryRun=true, Sync must report
// SyncActionCreated with the "Would create (dry-run)" message and must NOT
// actually create the object on the fake client.
func TestSyncCreatesInDryRunWithoutHittingCreate(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	var createCalled bool
	client.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createCalled = true
		return true, nil, errors.New("must not be called in dry-run")
	})
	syncer := &Syncer{dynClient: client}

	summary, err := syncer.Sync(context.Background(),
		[]Manifest{testManifest("v1", "ConfigMap", "cm", "apps")}, "alpha", SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() unexpected error = %v", err)
	}
	if createCalled {
		t.Fatalf("Create() was called during dry-run")
	}
	if summary.Created != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if summary.Results[0].Action != SyncActionCreated {
		t.Fatalf("action = %q, want Created", summary.Results[0].Action)
	}
	if !strings.Contains(summary.Results[0].Message, "Would create (dry-run)") {
		t.Fatalf("message = %q", summary.Results[0].Message)
	}

	// Confirm no object was actually created.
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).
		Namespace("apps").Get(context.Background(), "cm", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound after dry-run, got err=%v", err)
	}
}
