package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// --- toolGetPodLogs extended tests (was 25.0% coverage) ---

func TestToolGetPodLogs_InvalidNamespace(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}
	result, rpcErr := callTool(t, server, "get_pod_logs", map[string]interface{}{
		"cluster":   "test-cluster",
		"namespace": "kube-system",
		"name":      "web",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for kube-system, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetPodLogs_ClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no kubeconfig")
		},
	}
	result, rpcErr := callTool(t, server, "get_pod_logs", map[string]interface{}{
		"cluster": "bad-cluster",
		"name":    "web",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for factory error, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client', got: %s", result.Content[0].Text)
	}
}

func TestToolGetPodLogs_SuccessDefaultsNamespaceAndTail(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
	}
	cs := k8sfake.NewSimpleClientset(pod)

	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return cs, nil
		},
	}

	// No namespace argument -> defaults to "default".
	// No tail_lines argument -> defaults to 100.
	result, rpcErr := callTool(t, server, "get_pod_logs", map[string]interface{}{
		"cluster": "test-cluster",
		"name":    "web",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	// The fake client returns "fake logs" as the default log body.
	if !strings.Contains(result.Content[0].Text, "fake logs") {
		t.Fatalf("expected 'fake logs' body, got: %q", result.Content[0].Text)
	}

	// Verify GetLogs was routed to namespace 'default'.
	var found bool
	for _, act := range cs.Actions() {
		if act.GetVerb() == "get" && act.GetResource().Resource == "pods" && act.GetSubresource() == "log" {
			if act.GetNamespace() != "default" {
				t.Fatalf("expected namespace 'default', got %q", act.GetNamespace())
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a get pods/log action, actions=%v", cs.Actions())
	}
}

func TestToolGetPodLogs_ContainerAndTailPassedThrough(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "apps"},
	}
	cs := k8sfake.NewSimpleClientset(pod)

	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return cs, nil
		},
	}

	// Exercises both the `container != ""` branch and the
	// `tail_lines` float64 -> int64 conversion branch.
	result, rpcErr := callTool(t, server, "get_pod_logs", map[string]interface{}{
		"cluster":    "test-cluster",
		"namespace":  "apps",
		"name":       "web",
		"container":  "sidecar",
		"tail_lines": float64(42),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	// Verify the GetLogs subresource call was routed to the correct
	// namespace and pod.
	var found bool
	for _, act := range cs.Actions() {
		if act.GetVerb() == "get" && act.GetResource().Resource == "pods" && act.GetSubresource() == "log" {
			if act.GetNamespace() != "apps" {
				t.Fatalf("expected namespace 'apps', got %q", act.GetNamespace())
			}
			if getAct, ok := act.(k8stesting.GetAction); ok && getAct.GetName() != "web" {
				t.Fatalf("expected pod name 'web', got %q", getAct.GetName())
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a get pods/log action, actions=%v", cs.Actions())
	}
}

// Note: TestToolGetPodLogs_GetLogsError would exercise the "Failed to get
// logs" branch, but client-go's fake pod-expansion for GetLogs bypasses the
// reactor chain and always returns "fake logs" via a fixed HTTP transport
// (see kubernetes/client-go fake_pod_expansion.go). So the DoRaw error path
// is not reachable through kubernetes.Interface fakes without hand-rolling a
// custom RoundTripper — deferred to a future refactor.
