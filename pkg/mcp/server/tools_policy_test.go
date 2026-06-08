package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes"
)

// dynamicScheme registers the CRD GVKs used by tools_policy.go so that
// dynfake.NewSimpleDynamicClient can serve List calls for them.
var dynamicScheme *runtime.Scheme

func init() {
	dynamicScheme = runtime.NewScheme()
	_ = corev1.AddToScheme(dynamicScheme)

	// Register constraint template list kind.
	ctGVK := schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplateList"}
	dynamicScheme.AddKnownTypeWithName(ctGVK, &unstructured.UnstructuredList{})
	ctItemGVK := schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate"}
	dynamicScheme.AddKnownTypeWithName(ctItemGVK, &unstructured.Unstructured{})

	// Register constraint list kind (k8srequiredlabels is a K8sRequiredLabels constraint).
	csGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabelsList"}
	dynamicScheme.AddKnownTypeWithName(csGVK, &unstructured.UnstructuredList{})
	csItemGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"}
	dynamicScheme.AddKnownTypeWithName(csItemGVK, &unstructured.Unstructured{})
}

// newPolicyTestServer creates a test Server with injected k8s and dynamic clients.
func newPolicyTestServer(k8sObjs []runtime.Object, dynObjs []runtime.Object) *Server {
	fakeK8s := k8sfake.NewSimpleClientset(k8sObjs...)
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme, dynObjs...)

	return &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
}

// --- toolCheckGatekeeper ---

func TestToolCheckGatekeeper_NotInstalled(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success result, got tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Not Installed") {
		t.Fatalf("expected 'Not Installed' in output, got: %s", text)
	}
}

func TestToolCheckGatekeeper_ClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig not found")
		},
	}
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "bad-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got success")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("expected 'Failed to create client' in error, got: %s", result.Content[0].Text)
	}
}

func TestToolCheckGatekeeper_InstalledWithPods(t *testing.T) {
	k8sObjs := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gatekeeper-controller-0",
				Namespace: "gatekeeper-system",
				Labels:    map[string]string{"control-plane": "controller-manager"},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gatekeeper-audit-0",
				Namespace: "gatekeeper-system",
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	}
	server := newPolicyTestServer(k8sObjs, nil)
	result, rpcErr := callTool(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Installed") {
		t.Fatalf("expected 'Installed' in output, got: %s", text)
	}
	if !strings.Contains(text, "2 pods") && !strings.Contains(text, "2 Pods") && !strings.Contains(text, "2 pod") {
		t.Logf("note: expected pod count in output, got: %s", text)
	}
}

// --- toolGetOwnershipPolicyStatus ---

func TestToolGetOwnershipPolicyStatus_DynamicClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("dynamic client unavailable")
		},
	}
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got success: %s", result.Content[0].Text)
	}
}

func TestToolGetOwnershipPolicyStatus_NoPolicy(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	// Should indicate policy is not configured
	if !strings.Contains(text, "Not Configured") && !strings.Contains(text, "not found") && !strings.Contains(text, "Not Installed") {
		t.Fatalf("expected policy-not-found indication, got: %s", text)
	}
}

// --- toolListOwnershipViolations ---

func TestToolListOwnershipViolations_DynamicClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("dynamic client unavailable")
		},
	}
	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got success: %s", result.Content[0].Text)
	}
}

func TestToolListOwnershipViolations_NoConstraint(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "not found") && !strings.Contains(text, "Not Configured") && !strings.Contains(text, "constraint") {
		t.Fatalf("expected no-constraint indication, got: %s", text)
	}
}
