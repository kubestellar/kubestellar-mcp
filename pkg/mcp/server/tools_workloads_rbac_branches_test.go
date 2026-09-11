package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Closes previously-uncovered error and all-namespaces arms in
// pkg/mcp/server/tools_workloads.go::toolGetPods and
// pkg/mcp/server/tools_rbac.go::toolAnalyzeSubjectPermissions.
//
// Baseline (go test -cover ./pkg/mcp/server/):
//   toolGetPods                     89.7%  (client-err, all-ns, list-err all cov0)
//   toolAnalyzeSubjectPermissions   88.2%  (client-err, CRB-list-err, RB-list-err cov0)
//
// Existing tests only cover the happy path with an explicit namespace and
// no failures. A regression that silently swallowed any of these error
// arms and returned an empty result would ship without any test signal.

// -----------------------------------------------------------------------
// toolGetPods
// -----------------------------------------------------------------------

// TestToolGetPodsClientError covers the "Failed to create client" arm:
// when the clientFactory returns an error before List is called, the tool
// must surface it via IsError=true with the descriptive prefix. Without
// this test a factory regression would silently return "No pods found".
func TestToolGetPodsClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing for cluster")
		},
	}
	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") ||
		!strings.Contains(result.Content[0].Text, "kubeconfig missing for cluster") {
		t.Fatalf("expected 'Failed to create client: ...' prefix, got: %s", result.Content[0].Text)
	}
}

// TestToolGetPodsAllNamespacesSuccess covers the `namespace == ""` arm.
// The tool selects `Pods("")` (all namespaces) instead of the scoped
// Pods(namespace). Omitting `namespace` from args yields "" from
// extractAndValidateNamespace, and the fake client returns every seeded
// pod regardless of namespace when the empty selector is used.
func TestToolGetPodsAllNamespacesSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-b"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			), nil
		},
	}
	// Omit namespace to drive the all-namespaces arm.
	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 pods") {
		t.Fatalf("expected 'Found 2 pods' across namespaces, got: %s", text)
	}
	if !strings.Contains(text, "ns-a/pod-a") || !strings.Contains(text, "ns-b/pod-b") {
		t.Fatalf("expected cross-namespace pod names in output, got: %s", text)
	}
}

// TestToolGetPodsListError covers the "Failed to list pods" arm. A
// PrependReactor causes CoreV1().Pods().List to return an error; the tool
// must not swallow it or fall through to "No pods found".
func TestToolGetPodsListError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "pods",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("pod list failed")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list pods") ||
		!strings.Contains(text, "pod list failed") {
		t.Fatalf("expected 'Failed to list pods: pod list failed', got: %s", text)
	}
}

// TestToolGetPodsNoPodsFound covers the len(pods.Items) == 0 arm — happy
// path but with an empty fake client. Pins the exact "No pods found"
// return so a regression that emitted "Found 0 pods:\n\n" instead would
// be caught by the test.
func TestToolGetPodsNoPodsFound(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}
	result, rpcErr := callTool(t, server, "get_pods", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success arm on empty list, got IsError: %s", result.Content[0].Text)
	}
	if result.Content[0].Text != "No pods found" {
		t.Fatalf("expected exact 'No pods found', got: %q", result.Content[0].Text)
	}
}

// -----------------------------------------------------------------------
// toolAnalyzeSubjectPermissions
// -----------------------------------------------------------------------

// TestToolAnalyzeSubjectPermissionsClientError covers the
// "Failed to create client" arm — the factory error must surface with
// IsError=true rather than the RBAC-analysis being silently skipped.
func TestToolAnalyzeSubjectPermissionsClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing for cluster")
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") ||
		!strings.Contains(result.Content[0].Text, "kubeconfig missing for cluster") {
		t.Fatalf("expected 'Failed to create client: ...' prefix, got: %s", result.Content[0].Text)
	}
}

// TestToolAnalyzeSubjectPermissionsListCRBError covers the
// "Failed to list cluster role bindings" arm. The tool must not fall
// through to the RoleBindings list step when the CRB list fails, because
// the caller would see a misleading partial RBAC report.
func TestToolAnalyzeSubjectPermissionsListCRBError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "clusterrolebindings",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("crb list denied")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for CRB list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list cluster role bindings") ||
		!strings.Contains(text, "crb list denied") {
		t.Fatalf("expected 'Failed to list cluster role bindings: crb list denied', got: %s", text)
	}
}

// TestToolAnalyzeSubjectPermissionsListRBError covers the
// "Failed to list role bindings" arm. CRB list succeeds (empty) but the
// namespaced RB list fails; the tool must not silently emit "No RBAC
// bindings found".
func TestToolAnalyzeSubjectPermissionsListRBError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "rolebindings",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("rb list denied")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for RB list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list role bindings") ||
		!strings.Contains(text, "rb list denied") {
		t.Fatalf("expected 'Failed to list role bindings: rb list denied', got: %s", text)
	}
}

// TestToolAnalyzeSubjectPermissionsClusterRoleGetError covers the
// "error fetching" per-role branch inside the CRB loop: a matching CRB
// references a ClusterRole whose Get() fails. The tool must skip the
// individual role (with an inline error note) and continue rather than
// aborting the whole analysis.
func TestToolAnalyzeSubjectPermissionsClusterRoleGetError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "alice-binding"},
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "alice"},
					},
					RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "missing-role"},
				},
			)
			client.PrependReactor("get", "clusterroles",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("cluster role fetch failed")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (soft-fail per role), got IsError: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "missing-role") ||
		!strings.Contains(text, "error fetching") ||
		!strings.Contains(text, "cluster role fetch failed") {
		t.Fatalf("expected inline '- missing-role (error fetching: cluster role fetch failed)', got: %s", text)
	}
}
