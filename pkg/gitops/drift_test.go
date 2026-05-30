package gitops

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCompareObjects(t *testing.T) {
	expected := map[string]interface{}{
		"replicas":        float64(3),
		"resourceVersion": "123",
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": map[string]interface{}{
					"image": "demo:v2",
				},
			},
		},
	}
	actual := map[string]interface{}{
		"replicas":        float64(1),
		"resourceVersion": "999",
		"template": map[string]interface{}{
			"spec": "not-a-map",
		},
	}

	diffs := compareObjects("spec", expected, actual)
	assertContainsDiff(t, diffs, "spec.replicas")
	assertContainsDiff(t, diffs, "spec.template.spec: type mismatch")
	assertNotContainsDiff(t, diffs, "resourceVersion")
}

func TestCompareManifests(t *testing.T) {
	d := &DriftDetector{}
	gitManifest := Manifest{
		Kind: "ConfigMap",
		Metadata: ManifestMetadata{
			Name:   "demo",
			Labels: map[string]string{"app": "demo", "tier": "web"},
		},
		Spec: map[string]interface{}{"replicas": float64(2)},
		Data: map[string]interface{}{"config": "expected"},
	}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{"replicas": float64(1)},
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{"app": "other"},
		},
	}}

	diffs := d.compareManifests(gitManifest, cluster)
	assertContainsDiff(t, diffs, "spec.replicas")
	assertContainsDiff(t, diffs, "data: missing in cluster")
	assertContainsDiff(t, diffs, "label app: other (expected: demo)")
	assertContainsDiff(t, diffs, "label tier: missing in cluster")
}

func TestGetGVRFallback(t *testing.T) {
	d := &DriftDetector{}

	gvr, err := d.getGVR(Manifest{APIVersion: "apps/v1", Kind: "Deployment"})
	if err != nil {
		t.Fatalf("getGVR() unexpected error: %v", err)
	}
	if gvr.Group != "apps" || gvr.Version != "v1" || gvr.Resource != "deployments" {
		t.Fatalf("unexpected GVR: %#v", gvr)
	}

	gvr, err = d.getGVR(Manifest{APIVersion: "example.io/v1alpha1", Kind: "Widget"})
	if err != nil {
		t.Fatalf("getGVR() unexpected error: %v", err)
	}
	if gvr.Resource != "widgets" {
		t.Fatalf("getGVR() resource = %q, want widgets", gvr.Resource)
	}

	if _, err := d.getGVR(Manifest{APIVersion: "not/a/version/at/all", Kind: "Deployment"}); err == nil {
		t.Fatal("getGVR() expected error for invalid api version")
	}
}

func TestKindToResourceAndClusterScope(t *testing.T) {
	tests := []struct {
		kind      string
		resource  string
		clustered bool
	}{
		{kind: "Deployment", resource: "deployments", clustered: false},
		{kind: "ClusterRole", resource: "clusterroles", clustered: true},
		{kind: "Widget", resource: "widgets", clustered: false},
	}

	for _, tt := range tests {
		if got := kindToResource(tt.kind); got != tt.resource {
			t.Fatalf("kindToResource(%q) = %q, want %q", tt.kind, got, tt.resource)
		}
		if got := IsClusterScoped(tt.kind); got != tt.clustered {
			t.Fatalf("IsClusterScoped(%q) = %v, want %v", tt.kind, got, tt.clustered)
		}
	}
}

func TestIsSystemManagedField(t *testing.T) {
	if !isSystemManagedField("managedFields") {
		t.Fatal("managedFields should be treated as system-managed")
	}
	if isSystemManagedField("spec") {
		t.Fatal("spec should not be treated as system-managed")
	}
}

func assertContainsDiff(t *testing.T, diffs []string, want string) {
	t.Helper()
	for _, diff := range diffs {
		if strings.Contains(diff, want) {
			return
		}
	}
	t.Fatalf("expected diff containing %q, got %v", want, diffs)
}

func assertNotContainsDiff(t *testing.T, diffs []string, unwanted string) {
	t.Helper()
	for _, diff := range diffs {
		if strings.Contains(diff, unwanted) {
			t.Fatalf("did not expect diff containing %q, got %v", unwanted, diffs)
		}
	}
}
