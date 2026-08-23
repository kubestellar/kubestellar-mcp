package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDetectDrift_EmptyManifests(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	d := &DriftDetector{dynClient: client}

	drifts, err := d.DetectDrift(context.Background(), nil, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("DetectDrift() = %#v, want no drifts for empty manifests", drifts)
	}
}

func TestDetectDrift_MissingInCluster(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	d := &DriftDetector{dynClient: client}

	manifest := Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "absent", Namespace: "apps"},
		Data:       map[string]interface{}{"key": "value"},
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "absent", "namespace": "apps"},
			"data":       map[string]interface{}{"key": "value"},
		},
	}

	drifts, err := d.DetectDrift(context.Background(), []Manifest{manifest}, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("DetectDrift() = %d drifts, want 1", len(drifts))
	}
	if drifts[0].DriftType != DriftTypeMissing {
		t.Fatalf("DriftType = %q, want %q", drifts[0].DriftType, DriftTypeMissing)
	}
	if drifts[0].Cluster != "alpha" {
		t.Fatalf("Cluster = %q, want %q", drifts[0].Cluster, "alpha")
	}
}

func TestDetectDrift_MatchingResource(t *testing.T) {
	existing := unstructuredObj("v1", "ConfigMap", "same", "apps", map[string]interface{}{
		"data": map[string]interface{}{"key": "value"},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	d := &DriftDetector{dynClient: client}

	manifest := Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "same", Namespace: "apps"},
		Data:       map[string]interface{}{"key": "value"},
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "same", "namespace": "apps"},
			"data":       map[string]interface{}{"key": "value"},
		},
	}

	drifts, err := d.DetectDrift(context.Background(), []Manifest{manifest}, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("DetectDrift() = %#v, want no drifts for identical resource", drifts)
	}
}

func TestDetectDrift_MixedResults(t *testing.T) {
	existing := unstructuredObj("v1", "ConfigMap", "same", "apps", map[string]interface{}{
		"data": map[string]interface{}{"key": "value"},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	d := &DriftDetector{dynClient: client}

	inSync := Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "same", Namespace: "apps"},
		Data:       map[string]interface{}{"key": "value"},
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "same", "namespace": "apps"},
			"data":       map[string]interface{}{"key": "value"},
		},
	}
	gone := Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "absent", Namespace: "apps"},
		Data:       map[string]interface{}{"key": "value"},
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "absent", "namespace": "apps"},
			"data":       map[string]interface{}{"key": "value"},
		},
	}

	drifts, err := d.DetectDrift(context.Background(), []Manifest{inSync, gone}, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("DetectDrift() = %d drifts, want exactly 1 (in-sync + gone)", len(drifts))
	}
	if drifts[0].Name != "absent" {
		t.Fatalf("drift Name = %q, want %q", drifts[0].Name, "absent")
	}
	if drifts[0].DriftType != DriftTypeMissing {
		t.Fatalf("DriftType = %q, want %q", drifts[0].DriftType, DriftTypeMissing)
	}
}

func TestDetectDrift_ModifiedRecordedAsDrift(t *testing.T) {
	existing := unstructuredObj("apps/v1", "Deployment", "demo", "apps", map[string]interface{}{
		"spec": map[string]interface{}{"replicas": int64(1)},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	d := &DriftDetector{dynClient: client}

	manifest := Manifest{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata:   ManifestMetadata{Name: "demo", Namespace: "apps"},
		Spec:       map[string]interface{}{"replicas": int64(3)},
		Raw: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]interface{}{"name": "demo", "namespace": "apps"},
			"spec":       map[string]interface{}{"replicas": int64(3)},
		},
	}

	drifts, err := d.DetectDrift(context.Background(), []Manifest{manifest}, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("DetectDrift() = %d drifts, want 1", len(drifts))
	}
	if drifts[0].DriftType != DriftTypeModified {
		t.Fatalf("DriftType = %q, want %q", drifts[0].DriftType, DriftTypeModified)
	}
	if drifts[0].ClusterValue == nil {
		t.Fatal("ClusterValue not populated for modified drift")
	}
}

func TestDetectDrift_CheckResourceErrorRecordedAsMissing(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "denied", errors.New("nope"))
	})
	d := &DriftDetector{dynClient: client}

	manifest := Manifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   ManifestMetadata{Name: "denied", Namespace: "apps"},
		Raw: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "denied", "namespace": "apps"},
		},
	}

	drifts, err := d.DetectDrift(context.Background(), []Manifest{manifest}, "alpha")
	if err != nil {
		t.Fatalf("DetectDrift() unexpected error: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("DetectDrift() = %d drifts, want 1", len(drifts))
	}
	if drifts[0].DriftType != DriftTypeMissing {
		t.Fatalf("DriftType = %q, want %q", drifts[0].DriftType, DriftTypeMissing)
	}
	if len(drifts[0].Differences) != 1 || !strings.Contains(drifts[0].Differences[0], "Error checking resource:") {
		t.Fatalf("Differences = %#v, want single 'Error checking resource:' entry", drifts[0].Differences)
	}
}
