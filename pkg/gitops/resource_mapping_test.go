package gitops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestKindToResource(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		{"Deployment", "deployments"},
		{"Service", "services"},
		{"ConfigMap", "configmaps"},
		{"Secret", "secrets"},
		{"Pod", "pods"},
		{"StatefulSet", "statefulsets"},
		{"DaemonSet", "daemonsets"},
		{"ReplicaSet", "replicasets"},
		{"Job", "jobs"},
		{"CronJob", "cronjobs"},
		{"Ingress", "ingresses"},
		{"ServiceAccount", "serviceaccounts"},
		{"Role", "roles"},
		{"RoleBinding", "rolebindings"},
		{"ClusterRole", "clusterroles"},
		{"ClusterRoleBinding", "clusterrolebindings"},
		{"PersistentVolumeClaim", "persistentvolumeclaims"},
		{"PersistentVolume", "persistentvolumes"},
		{"Namespace", "namespaces"},
		{"NetworkPolicy", "networkpolicies"},
		{"HorizontalPodAutoscaler", "horizontalpodautoscalers"},
		// Fallback: unknown kind lowercased + "s"
		{"MyCustomResource", "mycustomresources"},
		{"Widget", "widgets"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := kindToResource(tt.kind)
			if got != tt.expected {
				t.Fatalf("kindToResource(%q) = %q, want %q", tt.kind, got, tt.expected)
			}
		})
	}
}

func TestIsClusterScoped(t *testing.T) {
	clusterScoped := []string{
		"Namespace", "Node", "PersistentVolume",
		"ClusterRole", "ClusterRoleBinding",
		"CustomResourceDefinition", "StorageClass", "PriorityClass",
	}
	for _, kind := range clusterScoped {
		t.Run(kind+"_scoped", func(t *testing.T) {
			if !IsClusterScoped(kind) {
				t.Fatalf("IsClusterScoped(%q) = false, want true", kind)
			}
		})
	}

	namespacedKinds := []string{
		"Deployment", "Service", "Pod", "ConfigMap", "Secret",
		"Role", "RoleBinding", "Job",
	}
	for _, kind := range namespacedKinds {
		t.Run(kind+"_namespaced", func(t *testing.T) {
			if IsClusterScoped(kind) {
				t.Fatalf("IsClusterScoped(%q) = true, want false", kind)
			}
		})
	}
}

func TestIsSystemManagedFieldExtended(t *testing.T) {
	managed := []string{
		"resourceVersion", "uid", "creationTimestamp",
		"generation", "managedFields", "selfLink", "status",
		"clusterIP", "clusterIPs", "nodeName", "podIP", "podIPs", "hostIP",
	}
	for _, field := range managed {
		t.Run(field+"_managed", func(t *testing.T) {
			if !isSystemManagedField(field) {
				t.Fatalf("isSystemManagedField(%q) = false, want true", field)
			}
		})
	}

	userFields := []string{"replicas", "image", "ports", "env", "labels", "annotations"}
	for _, field := range userFields {
		t.Run(field+"_user", func(t *testing.T) {
			if isSystemManagedField(field) {
				t.Fatalf("isSystemManagedField(%q) = true, want false", field)
			}
		})
	}
}

func TestCompareObjectsExtended(t *testing.T) {
	tests := []struct {
		name       string
		expected   map[string]interface{}
		actual     map[string]interface{}
		wantCount  int
		wantSubstr string
	}{
		{
			name:      "identical objects produce no differences",
			expected:  map[string]interface{}{"replicas": float64(3), "image": "nginx:1.27"},
			actual:    map[string]interface{}{"replicas": float64(3), "image": "nginx:1.27"},
			wantCount: 0,
		},
		{
			name:       "missing field in cluster",
			expected:   map[string]interface{}{"replicas": float64(3), "image": "nginx:1.27"},
			actual:     map[string]interface{}{"replicas": float64(3)},
			wantCount:  1,
			wantSubstr: "missing in cluster",
		},
		{
			name:       "value mismatch",
			expected:   map[string]interface{}{"replicas": float64(3)},
			actual:     map[string]interface{}{"replicas": float64(5)},
			wantCount:  1,
			wantSubstr: "spec.replicas",
		},
		{
			name:      "system-managed fields are skipped",
			expected:  map[string]interface{}{"resourceVersion": "123", "replicas": float64(3)},
			actual:    map[string]interface{}{"resourceVersion": "456", "replicas": float64(3)},
			wantCount: 0,
		},
		{
			name:       "nested map difference",
			expected:   map[string]interface{}{"containers": map[string]interface{}{"image": "nginx:1.27"}},
			actual:     map[string]interface{}{"containers": map[string]interface{}{"image": "nginx:1.26"}},
			wantCount:  1,
			wantSubstr: "containers.image",
		},
		{
			name:       "type mismatch nested",
			expected:   map[string]interface{}{"containers": map[string]interface{}{"image": "nginx"}},
			actual:     map[string]interface{}{"containers": "not-a-map"},
			wantCount:  1,
			wantSubstr: "type mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := compareObjects("spec", tt.expected, tt.actual)
			if len(diffs) != tt.wantCount {
				t.Fatalf("compareObjects() returned %d diffs, want %d: %v", len(diffs), tt.wantCount, diffs)
			}
			if tt.wantSubstr != "" && tt.wantCount > 0 {
				found := false
				for _, d := range diffs {
					if contains(d, tt.wantSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected diff containing %q, got %v", tt.wantSubstr, diffs)
				}
			}
		})
	}
}

func TestResolveManifestResource_NilMapper(t *testing.T) {
	tests := []struct {
		name         string
		manifest     Manifest
		wantResource string
		wantScoped   bool
	}{
		{
			name: "deployment falls back to static mapping",
			manifest: Manifest{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Metadata:   ManifestMetadata{Name: "test"},
			},
			wantResource: "deployments",
			wantScoped:   false,
		},
		{
			name: "namespace is cluster scoped",
			manifest: Manifest{
				APIVersion: "v1",
				Kind:       "Namespace",
				Metadata:   ManifestMetadata{Name: "test-ns"},
			},
			wantResource: "namespaces",
			wantScoped:   true,
		},
		{
			name: "cluster role is cluster scoped",
			manifest: Manifest{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
				Metadata:   ManifestMetadata{Name: "admin"},
			},
			wantResource: "clusterroles",
			wantScoped:   true,
		},
		{
			name: "configmap is namespaced",
			manifest: Manifest{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Metadata:   ManifestMetadata{Name: "cfg"},
			},
			wantResource: "configmaps",
			wantScoped:   false,
		},
		{
			name: "unknown CRD uses fallback",
			manifest: Manifest{
				APIVersion: "custom.io/v1alpha1",
				Kind:       "Widget",
				Metadata:   ManifestMetadata{Name: "w1"},
			},
			wantResource: "widgets",
			wantScoped:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping, err := resolveManifestResource(tt.manifest, nil)
			if err != nil {
				t.Fatalf("resolveManifestResource() error = %v", err)
			}
			if mapping.GVR.Resource != tt.wantResource {
				t.Fatalf("GVR.Resource = %q, want %q", mapping.GVR.Resource, tt.wantResource)
			}
			if mapping.ClusterScoped != tt.wantScoped {
				t.Fatalf("ClusterScoped = %v, want %v", mapping.ClusterScoped, tt.wantScoped)
			}
		})
	}
}

func TestResolveManifestResource_InvalidAPIVersion(t *testing.T) {
	manifest := Manifest{
		APIVersion: "not/a/valid/version",
		Kind:       "Something",
		Metadata:   ManifestMetadata{Name: "x"},
	}
	_, err := resolveManifestResource(manifest, nil)
	if err == nil {
		t.Fatal("expected error for invalid apiVersion, got nil")
	}
}

func TestResolveManifestResource_UsesRESTMapperWhenAvailable(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})
	mapper.AddSpecific(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployment"},
		meta.RESTScopeNamespace,
	)

	mapping, err := resolveManifestResource(Manifest{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata:   ManifestMetadata{Name: "frontend", Namespace: "apps"},
	}, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.GVR != (schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}) {
		t.Fatalf("unexpected GVR: %#v", mapping.GVR)
	}
	if mapping.ClusterScoped {
		t.Fatal("expected deployment to be namespaced")
	}
}

func TestResolveManifestResource_FallsBackWhenRESTMapperMisses(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})

	mapping, err := resolveManifestResource(Manifest{
		APIVersion: "rbac.authorization.k8s.io/v1",
		Kind:       "ClusterRole",
		Metadata:   ManifestMetadata{Name: "viewer"},
	}, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.GVR.Resource != "clusterroles" {
		t.Fatalf("GVR.Resource = %q, want clusterroles", mapping.GVR.Resource)
	}
	if !mapping.ClusterScoped {
		t.Fatal("expected cluster role to be cluster scoped")
	}
}

func TestNewRESTMapperInvalidConfigReturnsNil(t *testing.T) {
	if mapper := newRESTMapper(&rest.Config{}); mapper != nil {
		t.Fatalf("expected nil mapper for invalid config, got %#v", mapper)
	}
}

func TestNewRESTMapperBuildsMapperFromDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kind":                        "APIVersions",
				"versions":                    []string{"v1"},
				"serverAddressByClientCIDRs": []any{},
			})
		case "/api/v1":
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "configmaps", SingularName: "configmap", Namespaced: true, Kind: "ConfigMap"},
				},
			})
		case "/apis":
			_ = json.NewEncoder(w).Encode(metav1.APIGroupList{
				Groups: []metav1.APIGroup{
					{
						Name: "rbac.authorization.k8s.io",
						Versions: []metav1.GroupVersionForDiscovery{
							{GroupVersion: "rbac.authorization.k8s.io/v1", Version: "v1"},
						},
						PreferredVersion: metav1.GroupVersionForDiscovery{
							GroupVersion: "rbac.authorization.k8s.io/v1",
							Version:      "v1",
						},
					},
				},
			})
		case "/apis/rbac.authorization.k8s.io/v1":
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				GroupVersion: "rbac.authorization.k8s.io/v1",
				APIResources: []metav1.APIResource{
					{Name: "clusterroles", SingularName: "clusterrole", Namespaced: false, Kind: "ClusterRole"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mapper := newRESTMapper(&rest.Config{Host: server.URL})
	if mapper == nil {
		t.Fatal("expected REST mapper from discovery server")
	}

	mapping, err := resolveManifestResource(Manifest{
		APIVersion: "rbac.authorization.k8s.io/v1",
		Kind:       "ClusterRole",
		Metadata:   ManifestMetadata{Name: "viewer"},
	}, mapper)
	if err != nil {
		t.Fatalf("resolveManifestResource() error = %v", err)
	}
	if mapping.GVR.Resource != "clusterroles" {
		t.Fatalf("GVR.Resource = %q, want clusterroles", mapping.GVR.Resource)
	}
	if !mapping.ClusterScoped {
		t.Fatal("expected cluster role mapping to be cluster scoped")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
