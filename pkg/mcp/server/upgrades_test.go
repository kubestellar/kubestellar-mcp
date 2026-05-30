package server

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetOpenShiftVersionInfo(t *testing.T) {
	cv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "version",
			},
			"spec": map[string]interface{}{
				"channel":   "stable-4.14",
				"clusterID": "abc-123",
			},
			"status": map[string]interface{}{
				"desired": map[string]interface{}{
					"version": "4.14.3",
				},
				"availableUpdates": []interface{}{
					map[string]interface{}{
						"version": "4.14.4",
						"image":   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
					},
					map[string]interface{}{
						"version": "4.14.5",
						"image":   "quay.io/openshift-release-dev/ocp-release@sha256:def456",
					},
				},
				"history": []interface{}{
					map[string]interface{}{
						"version":        "4.14.3",
						"state":          "Completed",
						"completionTime": "2023-11-01T10:00:00Z",
					},
					map[string]interface{}{
						"version":        "4.14.2",
						"state":          "Completed",
						"completionTime": "2023-10-15T08:30:00Z",
					},
				},
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "Progressing",
						"status":  "True",
						"message": "Working towards 4.14.3",
					},
				},
			},
		},
	}
	cv.SetGroupVersionKind(clusterVersionGVR.GroupVersion().WithKind("ClusterVersion"))

	s := &Server{}
	var sb strings.Builder

	result, isErr := s.getOpenShiftVersionInfo(nil, cv, &sb)
	if isErr {
		t.Fatalf("getOpenShiftVersionInfo() returned error: %s", result)
	}

	wantStrings := []string{
		"OpenShift",
		"4.14.3",
		"stable-4.14",
		"abc-123",
		"Upgrade Status",
		"In Progress",
		"Working towards 4.14.3",
		"Available Updates",
		"4.14.4",
		"4.14.5",
		"Upgrade History",
		"Completed",
	}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("getOpenShiftVersionInfo() result missing %q in:\n%s", want, result)
		}
	}
}

func TestGetOpenShiftVersionInfo_NoUpdates(t *testing.T) {
	cv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "version",
			},
			"spec": map[string]interface{}{
				"channel": "stable-4.14",
			},
			"status": map[string]interface{}{
				"desired": map[string]interface{}{
					"version": "4.14.10",
				},
				"availableUpdates": []interface{}{},
			},
		},
	}
	cv.SetGroupVersionKind(clusterVersionGVR.GroupVersion().WithKind("ClusterVersion"))

	s := &Server{}
	var sb strings.Builder

	result, isErr := s.getOpenShiftVersionInfo(nil, cv, &sb)
	if isErr {
		t.Fatalf("getOpenShiftVersionInfo() returned error: %s", result)
	}

	wantStrings := []string{
		"OpenShift",
		"4.14.10",
		"stable-4.14",
		"None (cluster is at latest version for this channel)",
	}
	for _, want := range wantStrings {
		if !strings.Contains(result, want) {
			t.Errorf("getOpenShiftVersionInfo() result missing %q in:\n%s", want, result)
		}
	}
}

func TestClusterTypeConstants(t *testing.T) {
	types := []string{
		ClusterTypeOpenShift,
		ClusterTypeEKS,
		ClusterTypeGKE,
		ClusterTypeAKS,
		ClusterTypeKubeadm,
		ClusterTypeK3s,
		ClusterTypeKind,
		ClusterTypeMinikube,
		ClusterTypeUnknown,
	}

	expectedValues := map[string]string{
		ClusterTypeOpenShift: "openshift",
		ClusterTypeEKS:       "eks",
		ClusterTypeGKE:       "gke",
		ClusterTypeAKS:       "aks",
		ClusterTypeKubeadm:   "kubeadm",
		ClusterTypeK3s:       "k3s",
		ClusterTypeKind:      "kind",
		ClusterTypeMinikube:  "minikube",
		ClusterTypeUnknown:   "unknown",
	}

	for _, ct := range types {
		if expectedValues[ct] != ct {
			t.Errorf("cluster type constant %q has unexpected value", ct)
		}
	}
}
