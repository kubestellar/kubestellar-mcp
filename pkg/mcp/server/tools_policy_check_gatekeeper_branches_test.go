package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes"
	k8stesting "k8s.io/client-go/testing"
)

// TestToolCheckGatekeeper_DegradedPods covers the `else if totalPods > 0`
// branch where the namespace has pods but not all report Ready+Running.
func TestToolCheckGatekeeper_DegradedPods(t *testing.T) {
	k8sObjs := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "gk-audit", Namespace: "gatekeeper-system"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: false}},
			},
		},
	}
	server := newPolicyTestServer(k8sObjs, nil)
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "c"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Degraded") {
		t.Fatalf("expected 'Degraded' in output, got: %s", text)
	}
}

// TestToolCheckGatekeeper_NoPods covers the final `else` branch — the
// gatekeeper-system namespace exists but has zero pods.
func TestToolCheckGatekeeper_NoPods(t *testing.T) {
	k8sObjs := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
	}
	server := newPolicyTestServer(k8sObjs, nil)
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "c"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "no pods found") {
		t.Fatalf("expected 'no pods found' in output, got: %s", text)
	}
}

// TestToolCheckGatekeeper_PodListError covers the pod-list error branch
// where the k8s reactor returns a non-nil error.
func TestToolCheckGatekeeper_PodListError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
	)
	fakeK8s.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	server := &Server{
		discoverer:    stubDiscoverer{},
		clientFactory: func(_ string) (kubernetes.Interface, error) { return fakeK8s, nil },
	}
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "c"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to list Gatekeeper pods") {
		t.Fatalf("expected 'Failed to list Gatekeeper pods', got: %s", result.Content[0].Text)
	}
}

// TestToolCheckGatekeeper_DynamicClientError covers the branch that logs
// "Failed to check ConstraintTemplates" when getDynamicClientForCluster
// returns a non-nil error but the k8s client works.
func TestToolCheckGatekeeper_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
	)
	server := &Server{
		discoverer:    stubDiscoverer{},
		clientFactory: func(_ string) (kubernetes.Interface, error) { return fakeK8s, nil },
		dynamicClientFactory: func(_ string) (dynamic.Interface, error) {
			return nil, errors.New("no dynamic client")
		},
	}
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "c"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to check ConstraintTemplates") {
		t.Fatalf("expected 'Failed to check ConstraintTemplates', got: %s", result.Content[0].Text)
	}
}

// TestToolCheckGatekeeper_WithTemplatesAndOwnershipInstalled covers both the
// `len(templates.Items) > 0` iteration branch AND the ownership-policy
// "Installed" branch (dynClient.Get on the ownership template succeeds).
func TestToolCheckGatekeeper_WithTemplatesAndOwnershipInstalled(t *testing.T) {
	k8sObjs := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "gk-controller", Namespace: "gatekeeper-system"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
	}
	ownership := &unstructured.Unstructured{}
	ownership.SetAPIVersion("templates.gatekeeper.sh/v1")
	ownership.SetKind("ConstraintTemplate")
	ownership.SetName(ownershipTemplateName)

	other := &unstructured.Unstructured{}
	other.SetAPIVersion("templates.gatekeeper.sh/v1")
	other.SetKind("ConstraintTemplate")
	other.SetName("k8sallowedrepos")

	server := newPolicyTestServer(k8sObjs, []runtime.Object{ownership, other})
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "c"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "2 installed") {
		t.Fatalf("expected '2 installed' template count, got: %s", text)
	}
	if !strings.Contains(text, "- "+ownershipTemplateName) {
		t.Fatalf("expected ownership template to be listed, got: %s", text)
	}
	if !strings.Contains(text, "- k8sallowedrepos") {
		t.Fatalf("expected other template to be listed, got: %s", text)
	}
	if !strings.Contains(text, "Ownership Policy:** Installed") {
		t.Fatalf("expected 'Ownership Policy: Installed', got: %s", text)
	}
}
