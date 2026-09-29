package workloads

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Closes previously-uncovered error and all-namespaces arms in
// pkg/mcp/server/tools_workloads.go::toolGetPods. The companion
// toolAnalyzeSubjectPermissions arms moved with the rbac domain to
// pkg/mcp/server/rbac/analyze_branches_test.go (kubestellar-mcp#1027).
//
// Baseline (go test -cover ./pkg/mcp/server/):
//   toolGetPods                     89.7%  (client-err, all-ns, list-err all cov0)
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
	server := &testServer{
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
	server := &testServer{
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
	server := &testServer{
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
	server := &testServer{
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
