package rbac

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// -----------------------------------------------------------------------
// toolFindResourceOwners — branches not covered by owners_test.go
// -----------------------------------------------------------------------

// TestToolFindResourceOwnersInvalidNamespaceArg exercises the
// extractAndValidateNamespace error arm (namespace passed as a non-string),
// which the existing validation/empty/success/filter tests never trigger.
func TestToolFindResourceOwnersInvalidNamespaceArg(t *testing.T) {
	server := &testServer{discoverer: stubDiscoverer{}}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": 42, // number, not string — triggers extractAndValidateNamespace error
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "error:") || !strings.Contains(text, "namespace must be a string") {
		t.Fatalf("expected 'error: ... namespace must be a string', got: %s", text)
	}
}

// TestToolFindResourceOwnersClientError covers the "Failed to create
// client" arm — the factory error must surface with IsError=true.
func TestToolFindResourceOwnersClientError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing for cluster")
		},
	}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to create client") || !strings.Contains(text, "kubeconfig missing for cluster") {
		t.Fatalf("expected 'Failed to create client: ...', got: %s", text)
	}
}

// TestToolFindResourceOwnersListDeploymentsError covers the "Failed to
// list deployments" arm. The tool must not fall through to the Services
// step when the Deployments list fails.
func TestToolFindResourceOwnersListDeploymentsError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "deployments",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("deployments list denied")
				})
			return client, nil
		},
	}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for deployments list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list deployments") || !strings.Contains(text, "deployments list denied") {
		t.Fatalf("expected 'Failed to list deployments: deployments list denied', got: %s", text)
	}
}

// TestToolFindResourceOwnersListServicesError covers the "Failed to list
// services" arm. Pods and Deployments list succeed (empty) but the
// Services list fails.
func TestToolFindResourceOwnersListServicesError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "services",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("services list denied")
				})
			return client, nil
		},
	}

	result, rpcErr := callTool(t, server, "find_resource_owners", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for services list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list services") || !strings.Contains(text, "services list denied") {
		t.Fatalf("expected 'Failed to list services: services list denied', got: %s", text)
	}
}

// TestToolFindResourceOwnersDeploymentAndServiceOwnerRefs covers the
// OwnerReferences loop bodies for Deployments and Services — the existing
// TestToolFindResourceOwnersWithResources only sets an OwnerReference on
// the Pod fixture, leaving the equivalent Deployment/Service branches
// (owners.go's "for _, ownerRef := range dep/svc.OwnerReferences") unhit.
func TestToolFindResourceOwnersDeploymentAndServiceOwnerRefs(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-deploy",
						Namespace: "apps",
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "ReplicaSet", Name: "web-rs-owner"},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-svc",
						Namespace: "apps",
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "Deployment", Name: "web-deploy"},
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
	if !strings.Contains(text, "**Deployment/web-deploy** (owner: ReplicaSet/web-rs-owner)") {
		t.Fatalf("expected deployment owner reference, got: %s", text)
	}
	if !strings.Contains(text, "**Service/web-svc** (owner: Deployment/web-deploy)") {
		t.Fatalf("expected service owner reference, got: %s", text)
	}
}
