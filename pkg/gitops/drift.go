package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// DriftType indicates the type of drift detected
type DriftType string

const (
	DriftTypeMissing  DriftType = "missing"  // Resource exists in git but not in cluster
	DriftTypeExtra    DriftType = "extra"    // Resource exists in cluster but not in git
	DriftTypeModified DriftType = "modified" // Resource differs between git and cluster
)

// DriftResult represents a detected drift
type DriftResult struct {
	Cluster      string            `json:"cluster"`
	ResourceKey  string            `json:"resourceKey"`
	Kind         string            `json:"kind"`
	Namespace    string            `json:"namespace"`
	Name         string            `json:"name"`
	DriftType    DriftType         `json:"driftType"`
	Differences  []string          `json:"differences,omitempty"`
	GitValue     interface{}       `json:"gitValue,omitempty"`
	ClusterValue interface{}       `json:"clusterValue,omitempty"`
}

// DriftDetector detects drift between git manifests and cluster state
type DriftDetector struct {
	client    *kubernetes.Clientset
	dynClient dynamic.Interface
}

// NewDriftDetector creates a new drift detector
func NewDriftDetector(config *rest.Config) (*DriftDetector, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &DriftDetector{
		client:    client,
		dynClient: dynClient,
	}, nil
}

// DetectDrift compares git manifests against cluster state
func (d *DriftDetector) DetectDrift(ctx context.Context, manifests []Manifest, clusterName string) ([]DriftResult, error) {
	var drifts []DriftResult

	// Build a map of expected resources from git
	expected := make(map[string]Manifest)
	for _, m := range manifests {
		key := m.GetKey().String()
		expected[key] = m
	}

	// Check each manifest against cluster state
	for key, manifest := range expected {
		drift, err := d.checkResource(ctx, manifest, clusterName)
		if err != nil {
			// Record error as a drift
			drifts = append(drifts, DriftResult{
				Cluster:     clusterName,
				ResourceKey: key,
				Kind:        manifest.Kind,
				Namespace:   manifest.GetNamespace(),
				Name:        manifest.Metadata.Name,
				DriftType:   DriftTypeMissing,
				Differences: []string{fmt.Sprintf("Error checking resource: %v", err)},
			})
			continue
		}

		if drift != nil {
			drifts = append(drifts, *drift)
		}
	}

	return drifts, nil
}

// checkResource checks a single resource for drift
func (d *DriftDetector) checkResource(ctx context.Context, manifest Manifest, clusterName string) (*DriftResult, error) {
	gvr, err := d.getGVR(manifest)
	if err != nil {
		return nil, err
	}

	namespace := manifest.GetNamespace()

	// Get current state from cluster
	var current *unstructured.Unstructured
	if namespace != "" && !isClusterScoped(manifest.Kind) {
		current, err = d.dynClient.Resource(gvr).Namespace(namespace).Get(ctx, manifest.Metadata.Name, metav1.GetOptions{})
	} else {
		current, err = d.dynClient.Resource(gvr).Get(ctx, manifest.Metadata.Name, metav1.GetOptions{})
	}

	if err != nil {
		// Resource doesn't exist in cluster
		return &DriftResult{
			Cluster:     clusterName,
			ResourceKey: manifest.GetKey().String(),
			Kind:        manifest.Kind,
			Namespace:   namespace,
			Name:        manifest.Metadata.Name,
			DriftType:   DriftTypeMissing,
			Differences: []string{"Resource does not exist in cluster"},
			GitValue:    manifest.Raw,
		}, nil
	}

	// Compare relevant fields
	differences := d.compareManifests(manifest, current)
	if len(differences) > 0 {
		return &DriftResult{
			Cluster:      clusterName,
			ResourceKey:  manifest.GetKey().String(),
			Kind:         manifest.Kind,
			Namespace:    namespace,
			Name:         manifest.Metadata.Name,
			DriftType:    DriftTypeModified,
			Differences:  differences,
			GitValue:     manifest.Raw,
			ClusterValue: current.Object,
		}, nil
	}

	return nil, nil
}

// compareManifests compares a git manifest with cluster state
func (d *DriftDetector) compareManifests(git Manifest, cluster *unstructured.Unstructured) []string {
	var differences []string

	// Compare spec if present
	if git.Spec != nil {
		clusterSpec, found, _ := unstructured.NestedMap(cluster.Object, "spec")
		if !found {
			differences = append(differences, "spec: missing in cluster")
		} else {
			specDiffs := compareObjects("spec", git.Spec, clusterSpec)
			differences = append(differences, specDiffs...)
		}
	}

	// Compare data for ConfigMaps/Secrets
	if git.Data != nil {
		clusterData, found, _ := unstructured.NestedMap(cluster.Object, "data")
		if !found {
			differences = append(differences, "data: missing in cluster")
		} else {
			dataDiffs := compareObjects("data", git.Data, clusterData)
			differences = append(differences, dataDiffs...)
		}
	}

	// Compare labels
	if git.Metadata.Labels != nil {
		clusterLabels, _, _ := unstructured.NestedStringMap(cluster.Object, "metadata", "labels")
		for k, v := range git.Metadata.Labels {
			if cv, ok := clusterLabels[k]; !ok {
				differences = append(differences, fmt.Sprintf("label %s: missing in cluster (expected: %s)", k, v))
			} else if cv != v {
				differences = append(differences, fmt.Sprintf("label %s: %s (expected: %s)", k, cv, v))
			}
		}
	}

	return differences
}

// compareObjects recursively compares two maps
func compareObjects(path string, expected, actual map[string]interface{}) []string {
	var differences []string

	for key, expectedVal := range expected {
		actualVal, exists := actual[key]
		newPath := fmt.Sprintf("%s.%s", path, key)

		if !exists {
			differences = append(differences, fmt.Sprintf("%s: missing in cluster", newPath))
			continue
		}

		// Skip certain fields that are managed by the system
		if isSystemManagedField(key) {
			continue
		}

		// Handle nested maps
		if expectedMap, ok := expectedVal.(map[string]interface{}); ok {
			if actualMap, ok := actualVal.(map[string]interface{}); ok {
				nested := compareObjects(newPath, expectedMap, actualMap)
				differences = append(differences, nested...)
			} else {
				differences = append(differences, fmt.Sprintf("%s: type mismatch", newPath))
			}
			continue
		}

		// Compare values
		if !reflect.DeepEqual(expectedVal, actualVal) {
			expectedJSON, _ := json.Marshal(expectedVal)
			actualJSON, _ := json.Marshal(actualVal)
			differences = append(differences, fmt.Sprintf("%s: %s (expected: %s)", newPath, string(actualJSON), string(expectedJSON)))
		}
	}

	return differences
}

// getGVR returns the GroupVersionResource for a manifest
func (d *DriftDetector) getGVR(manifest Manifest) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(manifest.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	// Map kind to resource name (lowercase plural)
	resource := kindToResource(manifest.Kind)

	return schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: resource,
	}, nil
}

// kindToResource converts Kind to resource name
func kindToResource(kind string) string {
	// Common mappings
	mappings := map[string]string{
		"Deployment":            "deployments",
		"Service":               "services",
		"ConfigMap":             "configmaps",
		"Secret":                "secrets",
		"Pod":                   "pods",
		"StatefulSet":           "statefulsets",
		"DaemonSet":             "daemonsets",
		"ReplicaSet":            "replicasets",
		"Job":                   "jobs",
		"CronJob":               "cronjobs",
		"Ingress":               "ingresses",
		"ServiceAccount":        "serviceaccounts",
		"Role":                  "roles",
		"RoleBinding":           "rolebindings",
		"ClusterRole":           "clusterroles",
		"ClusterRoleBinding":    "clusterrolebindings",
		"PersistentVolumeClaim": "persistentvolumeclaims",
		"PersistentVolume":      "persistentvolumes",
		"Namespace":             "namespaces",
		"NetworkPolicy":         "networkpolicies",
		"HorizontalPodAutoscaler": "horizontalpodautoscalers",
	}

	if resource, ok := mappings[kind]; ok {
		return resource
	}

	// Default: lowercase and add 's'
	return strings.ToLower(kind) + "s"
}

// isClusterScoped returns true if the kind is cluster-scoped
func isClusterScoped(kind string) bool {
	clusterScoped := map[string]bool{
		"Namespace":             true,
		"Node":                  true,
		"PersistentVolume":      true,
		"ClusterRole":           true,
		"ClusterRoleBinding":    true,
		"CustomResourceDefinition": true,
		"StorageClass":          true,
		"PriorityClass":         true,
	}
	return clusterScoped[kind]
}

// isSystemManagedField returns true if the field is managed by Kubernetes
func isSystemManagedField(field string) bool {
	systemFields := map[string]bool{
		"resourceVersion":     true,
		"uid":                 true,
		"creationTimestamp":   true,
		"generation":          true,
		"managedFields":       true,
		"selfLink":            true,
		"status":              true,
		"clusterIP":           true,
		"clusterIPs":          true,
		"nodeName":            true,
		"podIP":               true,
		"podIPs":              true,
		"hostIP":              true,
	}
	return systemFields[field]
}