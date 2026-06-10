package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func newDetectServer(t *testing.T, nodes []corev1.Node, dynamicObjs ...runtime.Object) *Server {
	t.Helper()
	var objs []runtime.Object
	for i := range nodes {
		objs = append(objs, &nodes[i])
	}
	fakeClient := kubernetesfake.NewSimpleClientset(objs...)

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			clusterVersionGVR: "ClusterVersionList",
		},
		dynamicObjs...,
	)

	return &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeClient, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return dynClient, nil
		},
	}
}

func TestDetectClusterTypeOpenShift(t *testing.T) {
	cv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterVersion",
			"metadata": map[string]interface{}{
				"name": "version",
			},
			"spec": map[string]interface{}{},
		},
	}
	cv.SetGroupVersionKind(clusterVersionGVR.GroupVersion().WithKind("ClusterVersion"))

	server := newDetectServer(t, []corev1.Node{makeNode("node1", nil, nil, "")}, cv)
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeOpenShift) {
		t.Errorf("expected OpenShift detection, got: %s", result)
	}
}

func TestDetectClusterTypeEKS(t *testing.T) {
	node := makeNode("node1",
		map[string]string{"eks.amazonaws.com/nodegroup": "default"},
		nil,
		"aws:///us-east-1a/i-1234567890",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeEKS) {
		t.Errorf("expected EKS detection, got: %s", result)
	}
}

func TestDetectClusterTypeGKE(t *testing.T) {
	node := makeNode("node1",
		map[string]string{"cloud.google.com/gke-nodepool": "default-pool"},
		nil,
		"gce:///projects/my-project/zones/us-central1-a/instances/gke-node-1",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeGKE) {
		t.Errorf("expected GKE detection, got: %s", result)
	}
}

func TestDetectClusterTypeGKENoLabel(t *testing.T) {
	// GKE with gce provider but no specific GKE label
	node := makeNode("node1", nil, nil, "gce:///projects/my-project/zones/us-central1-a/instances/node-1")
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeGKE) {
		t.Errorf("expected GKE detection via provider ID, got: %s", result)
	}
}

func TestDetectClusterTypeAKS(t *testing.T) {
	node := makeNode("node1",
		map[string]string{"kubernetes.azure.com/cluster": "my-aks"},
		nil,
		"azure:///subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Compute/virtualMachineScaleSets/vmss/virtualMachines/0",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeAKS) {
		t.Errorf("expected AKS detection, got: %s", result)
	}
}

func TestDetectClusterTypeKind(t *testing.T) {
	node := makeNode("node1",
		map[string]string{"io.x-k8s.kind.role": "control-plane"},
		nil,
		"",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeKind) {
		t.Errorf("expected kind detection, got: %s", result)
	}
}

func TestDetectClusterTypeMinikube(t *testing.T) {
	node := makeNode("node1",
		map[string]string{"minikube.k8s.io/version": "v1.30.0"},
		nil,
		"",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeMinikube) {
		t.Errorf("expected minikube detection, got: %s", result)
	}
}

func TestDetectClusterTypeKubeadm(t *testing.T) {
	node := makeNode("node1",
		nil,
		map[string]string{"kubeadm.alpha.kubernetes.io/cri-socket": "/var/run/containerd/containerd.sock"},
		"",
	)
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeKubeadm) {
		t.Errorf("expected kubeadm detection, got: %s", result)
	}
}

func TestDetectClusterTypeUnknown(t *testing.T) {
	node := makeNode("node1", nil, nil, "")
	server := newDetectServer(t, []corev1.Node{node})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeUnknown) {
		t.Errorf("expected unknown type, got: %s", result)
	}
}

func TestDetectClusterTypeNoNodes(t *testing.T) {
	server := newDetectServer(t, []corev1.Node{})
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
	if !strings.Contains(result, ClusterTypeUnknown) {
		t.Errorf("expected unknown type when no nodes, got: %s", result)
	}
	if !strings.Contains(result, "No nodes found") {
		t.Errorf("expected 'No nodes found' note, got: %s", result)
	}
}

func TestDetectClusterTypeClientError(t *testing.T) {
	server := &Server{
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	result, isErr := server.toolDetectClusterType(context.Background(), map[string]interface{}{})
	if !isErr {
		t.Fatalf("expected error result, got success: %s", result)
	}
	if !strings.Contains(result, "connection refused") {
		t.Errorf("expected error message in result, got: %s", result)
	}
}

func makeNode(name string, labels, annotations map[string]string, providerID string) corev1.Node {
	if labels == nil {
		labels = map[string]string{}
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
}
