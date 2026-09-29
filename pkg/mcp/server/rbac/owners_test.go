package rbac

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// --- toolFindResourceOwners ---

func TestToolFindResourceOwnersValidation(t *testing.T) {
	server := &testServer{discoverer: stubDiscoverer{}}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing namespace")
	}
	if !strings.Contains(result.Content[0].Text, "namespace is required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolFindResourceOwnersEmpty(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": "empty-ns",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (empty is valid), got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "No resources found") {
		t.Fatalf("expected 'No resources found', got: %s", text)
	}
}

func TestToolFindResourceOwnersWithResources(t *testing.T) {
	now := metav1.NewTime(time.Date(2025, time.June, 15, 10, 0, 0, 0, time.UTC))
	replicas := int32(3)
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-abc123",
						Namespace: "apps",
						Labels: map[string]string{
							"app.kubernetes.io/managed-by": "helm",
							"team":                         "platform",
						},
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "ReplicaSet", Name: "web-deploy-abc123"},
						},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{Manager: "kube-controller-manager", Time: &now},
						},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-deploy",
						Namespace: "apps",
						Labels: map[string]string{
							"owner": "team-alpha",
						},
						Annotations: map[string]string{
							"meta.helm.sh/release-name": "my-app",
						},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{Manager: "helm", Time: &now},
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "web"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "web"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "web", Image: "nginx"},
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-svc",
						Namespace: "apps",
						Labels: map[string]string{
							"created-by": "developer-bob",
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 3 resources") {
		t.Fatalf("expected 3 resources, got: %s", text)
	}
	if !strings.Contains(text, "Pod/web-abc123") {
		t.Fatalf("expected pod name, got: %s", text)
	}
	if !strings.Contains(text, "ReplicaSet/web-deploy-abc123") {
		t.Fatalf("expected owner reference, got: %s", text)
	}
	if !strings.Contains(text, "helm:my-app") {
		t.Fatalf("expected helm release annotation, got: %s", text)
	}
	if !strings.Contains(text, "team: platform") {
		t.Fatalf("expected team label, got: %s", text)
	}
	if !strings.Contains(text, "created-by: developer-bob") {
		t.Fatalf("expected created-by label, got: %s", text)
	}
}

func TestToolFindResourceOwnersFilterByType(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "ns"},
				},
			), nil
		},
	}

	// Filter to pods only
	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace":     "ns",
		"resource_type": "pods",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Pod/pod-1") {
		t.Fatalf("expected pod in output, got: %s", text)
	}
	if strings.Contains(text, "Service/svc-1") {
		t.Fatalf("services should be filtered out with resource_type=pods, got: %s", text)
	}
}
