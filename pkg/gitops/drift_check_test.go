package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCheckResource(t *testing.T) {
	tests := []struct {
		name          string
		manifest      Manifest
		existing      []runtime.Object
		clusterName   string
		wantNilResult bool
		wantDriftType DriftType
		wantDiffs     []string
		wantNotDiffs  []string
	}{
		{
			name: "resource missing in cluster",
			manifest: Manifest{
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
			},
			clusterName:   "alpha",
			wantDriftType: DriftTypeMissing,
			wantDiffs:     []string{"Resource does not exist in cluster"},
		},
		{
			name: "spec differs",
			manifest: Manifest{
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
			},
			existing: []runtime.Object{
				unstructuredObj("apps/v1", "Deployment", "demo", "apps", map[string]interface{}{
					"spec": map[string]interface{}{"replicas": int64(1)},
				}),
			},
			clusterName:   "alpha",
			wantDriftType: DriftTypeModified,
			wantDiffs:     []string{"spec.replicas"},
		},
		{
			name: "identical resource yields no drift",
			manifest: Manifest{
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
			},
			existing: []runtime.Object{
				unstructuredObj("v1", "ConfigMap", "same", "apps", map[string]interface{}{
					"data": map[string]interface{}{"key": "value"},
				}),
			},
			clusterName:   "alpha",
			wantNilResult: true,
		},
		{
			name: "system-managed fields ignored",
			manifest: Manifest{
				APIVersion: "v1",
				Kind:       "Service",
				Metadata:   ManifestMetadata{Name: "svc", Namespace: "apps"},
				Spec: map[string]interface{}{
					"selector": map[string]interface{}{"app": "demo"},
					"ports": map[string]interface{}{
						"port": int64(80),
					},
				},
				Raw: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata":   map[string]interface{}{"name": "svc", "namespace": "apps"},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{"app": "demo"},
						"ports": map[string]interface{}{
							"port": int64(80),
						},
					},
				},
			},
			existing: []runtime.Object{
				unstructuredObj("v1", "Service", "svc", "apps", map[string]interface{}{
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{"app": "demo"},
						"ports": map[string]interface{}{
							"port": int64(80),
						},
						"clusterIP":  "10.0.0.1",
						"clusterIPs": []interface{}{"10.0.0.1"},
					},
					"status": map[string]interface{}{
						"loadBalancer": map[string]interface{}{"ingress": []interface{}{}},
					},
				}),
			},
			clusterName:   "alpha",
			wantNilResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), tt.existing...)
			d := &DriftDetector{dynClient: client}

			got, err := d.checkResource(context.Background(), tt.manifest, tt.clusterName)
			if err != nil {
				t.Fatalf("checkResource() unexpected error: %v", err)
			}

			if tt.wantNilResult {
				if got != nil {
					t.Fatalf("checkResource() = %#v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("checkResource() = nil, want drift result")
			}
			if got.DriftType != tt.wantDriftType {
				t.Fatalf("DriftType = %q, want %q", got.DriftType, tt.wantDriftType)
			}
			if got.Cluster != tt.clusterName {
				t.Fatalf("Cluster = %q, want %q", got.Cluster, tt.clusterName)
			}
			if got.Name != tt.manifest.Metadata.Name {
				t.Fatalf("Name = %q, want %q", got.Name, tt.manifest.Metadata.Name)
			}
			for _, want := range tt.wantDiffs {
				assertContainsDiff(t, got.Differences, want)
			}
			for _, unwanted := range tt.wantNotDiffs {
				assertNotContainsDiff(t, got.Differences, unwanted)
			}
		})
	}
}

func TestCheckResourcePropagatesNonNotFoundErrors(t *testing.T) {
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
	_, err := d.checkResource(context.Background(), manifest, "alpha")
	if err == nil {
		t.Fatal("checkResource() error = nil, want error for forbidden get")
	}
	if !strings.Contains(err.Error(), "failed to get resource") {
		t.Fatalf("checkResource() error = %v, want wrap of get error", err)
	}
}

func TestCheckResourceUsesClusterScopedLookupForClusterScopedKind(t *testing.T) {
	existing := unstructuredObj("rbac.authorization.k8s.io/v1", "ClusterRole", "viewer", "", map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{"verbs": []interface{}{"get"}},
		},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	d := &DriftDetector{dynClient: client}

	manifest := Manifest{
		APIVersion: "rbac.authorization.k8s.io/v1",
		Kind:       "ClusterRole",
		Metadata:   ManifestMetadata{Name: "viewer"},
		Raw: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata":   map[string]interface{}{"name": "viewer"},
		},
	}
	got, err := d.checkResource(context.Background(), manifest, "alpha")
	if err != nil {
		t.Fatalf("checkResource() unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("checkResource() = %#v, want nil for identical cluster-scoped resource", got)
	}
}

func unstructuredObj(apiVersion, kind, name, namespace string, extra map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name": name,
		},
	}}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}
	for k, v := range extra {
		obj.Object[k] = v
	}
	obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(apiVersion, kind))
	return obj
}
