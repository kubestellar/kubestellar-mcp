package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestToolGetRolesInvalidNamespaceArg exercises the branch where
// extractAndValidateNamespace returns an error (namespace passed as a non-string).
// The existing TestToolGetRolesSuccess only covers the valid-namespace path.
func TestToolGetRolesInvalidNamespaceArg(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{
		"namespace": 42, // number, not string — triggers extractAndValidateNamespace error
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "namespace must be a string") {
		t.Fatalf("expected 'namespace must be a string' in output, got: %s", result.Content[0].Text)
	}
}

// TestToolGetRolesClientFactoryError exercises the clientFactory error branch:
// s.getClientForCluster returns an error, tool must surface it with the
// "Failed to create client" prefix.
func TestToolGetRolesClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, fmt.Errorf("kubeconfig context %q not found", clusterName)
		},
	}

	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{
		"cluster":   "missing-cluster",
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on clientFactory failure, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client' prefix, got: %s", result.Content[0].Text)
	}
}

// TestToolGetRolesListError exercises the roles.List error branch:
// the fake client's list reactor returns an error, tool must surface it with
// "Failed to list roles" prefix. Uses the namespaced path (explicit namespace).
func TestToolGetRolesListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "roles", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on list failure, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list roles") {
		t.Fatalf("expected 'Failed to list roles' prefix, got: %s", result.Content[0].Text)
	}
}

// TestToolGetRolesEmpty exercises the empty-result branch:
// list succeeds but returns zero items — tool must return "No roles found"
// with IsError=false. Existing tests only cover populated lists.
func TestToolGetRolesEmpty(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil // no roles seeded
		},
	}

	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{"namespace": "empty"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success on empty list, got error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No roles found") {
		t.Fatalf("expected 'No roles found' in output, got: %s", result.Content[0].Text)
	}
}

// TestToolGetRolesAllNamespaces exercises the all-namespaces branch
// (namespace == "" -> Roles("") list). The existing TestToolGetRolesSuccess
// passes an explicit "apps" namespace, so the "" branch was uncovered.
func TestToolGetRolesAllNamespaces(t *testing.T) {
	// Seed roles in two different namespaces so we can verify the tool
	// aggregates them under a single "Roles("")" call.
	now := metav1.NewTime(time.Date(2024, time.March, 15, 10, 0, 0, 0, time.UTC))
	mkRole := func(ns, name string) *rbacv1.Role {
		return &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         ns,
				CreationTimestamp: now,
			},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
			},
		}
	}
	client := k8sfake.NewSimpleClientset(
		mkRole("apps", "pod-reader"),
		mkRole("infra", "node-reader"),
	)
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return client, nil
		},
	}

	// No "namespace" key -> extractAndValidateNamespace returns ("", nil),
	// steering into the namespace == "" branch.
	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 roles") {
		t.Fatalf("expected 'Found 2 roles' in output, got: %s", text)
	}
	if !strings.Contains(text, "apps/pod-reader") {
		t.Fatalf("expected 'apps/pod-reader' in output, got: %s", text)
	}
	if !strings.Contains(text, "infra/node-reader") {
		t.Fatalf("expected 'infra/node-reader' in output, got: %s", text)
	}
}
