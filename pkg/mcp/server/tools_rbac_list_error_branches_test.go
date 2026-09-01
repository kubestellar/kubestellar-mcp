package server

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The existing TestToolGetRoleBindingsSuccess / TestToolGetClusterRoleBindingsSuccess
// / TestToolGetClusterRolesSuccess suites in tools_rbac_test.go cover the happy
// paths only, leaving three error arms per function uncovered at
// pkg/mcp/server/tools_rbac.go:
//
//   toolGetRoleBindings         (line 94): extractAndValidateNamespace error,
//                                          getClientForCluster error,
//                                          RoleBindings.List error
//   toolGetClusterRoleBindings  (line 136): getClientForCluster error,
//                                          ClusterRoleBindings.List error
//   toolGetClusterRoles         (line 56):  getClientForCluster error,
//                                          ClusterRoles.List error
//
// These tests pin each remaining error branch by (a) injecting a bad
// namespace argument type, (b) making clientFactory return an error, and
// (c) using PrependReactor on a fake client to force the List call to fail.

func TestToolGetRoleBindingsInvalidNamespaceType(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) {
			t.Fatalf("clientFactory must not be called when namespace validation fails")
			return nil, nil
		},
	}

	// Non-string namespace exercises the "namespace must be a string" arm
	// of extractAndValidateNamespace, which flows into toolGetRoleBindings'
	// error-return branch.
	result, rpcErr := callTool(t, s, "get_role_bindings", map[string]interface{}{
		"namespace": 42,
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	if !strings.HasPrefix(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetRoleBindingsClientFactoryError(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, rpcErr := callTool(t, s, "get_role_bindings", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true when client factory fails, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client' in output, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "kubeconfig missing") {
		t.Fatalf("expected wrapped error text 'kubeconfig missing', got: %s", result.Content[0].Text)
	}
}

func TestToolGetRoleBindingsListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "rolebindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden: cannot list rolebindings")
	})

	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	// Empty namespace exercises the all-namespaces List(""), which fails
	// via the reactor; this pins the RoleBindings.List error arm.
	result, rpcErr := callTool(t, s, "get_role_bindings", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on list failure, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list role bindings") {
		t.Fatalf("expected 'Failed to list role bindings', got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "forbidden: cannot list rolebindings") {
		t.Fatalf("expected wrapped reactor error, got: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterRoleBindingsClientFactoryError(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("no kubeconfig")
		},
	}

	result, rpcErr := callTool(t, s, "get_cluster_role_bindings", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client', got: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterRoleBindingsListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "clusterrolebindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})

	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, rpcErr := callTool(t, s, "get_cluster_role_bindings", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on list failure, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list cluster role bindings") {
		t.Fatalf("expected 'Failed to list cluster role bindings', got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "apiserver unavailable") {
		t.Fatalf("expected wrapped reactor error, got: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterRolesClientFactoryError(t *testing.T) {
	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing")
		},
	}

	result, rpcErr := callTool(t, s, "get_cluster_roles", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client', got: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterRolesListError(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "clusterroles", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom: apiserver unavailable")
	})

	s := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return client, nil },
	}

	result, rpcErr := callTool(t, s, "get_cluster_roles", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on list failure, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list cluster roles") {
		t.Fatalf("expected 'Failed to list cluster roles', got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "boom: apiserver unavailable") {
		t.Fatalf("expected wrapped reactor error, got: %s", result.Content[0].Text)
	}
}
