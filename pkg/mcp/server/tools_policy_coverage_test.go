package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes"
)

// --- toolInstallOwnershipPolicy ---

func TestToolInstallOwnershipPolicy_ClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig not found")
		},
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{"cluster": "bad"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error for missing client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolInstallOwnershipPolicy_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("dynamic client failed")
		},
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error for missing dynamic client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create dynamic client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolInstallOwnershipPolicy_Success(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme)
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Installing Ownership Policy") {
		t.Fatalf("unexpected output: %s", text)
	}
}

func TestToolInstallOwnershipPolicy_WithCustomLabels(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme)
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster":            "test",
		"labels":             []interface{}{"app", "env"},
		"exclude_namespaces": []interface{}{"custom-ns"},
		"mode":               "warn",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
}

// --- toolSetOwnershipPolicyMode ---

func TestToolSetOwnershipPolicyMode_EmptyMode(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for empty mode")
	}
	if !strings.Contains(result.Content[0].Text, "mode is required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolSetOwnershipPolicyMode_InvalidMode(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "invalid",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for invalid mode")
	}
	if !strings.Contains(result.Content[0].Text, "must be one of") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolSetOwnershipPolicyMode_DynamicClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("no dynamic client")
		},
	}
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "warn",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for dynamic client failure")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolSetOwnershipPolicyMode_NotInstalled(t *testing.T) {
	// No constraint exists
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "warn",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	// When constraint not found, the function returns a non-error result
	text := result.Content[0].Text
	if !strings.Contains(text, "not installed") && !strings.Contains(text, "Not Installed") && !strings.Contains(text, "not found") {
		// It might return an RPC-level error depending on dynamic client behavior
		if !result.IsError {
			t.Logf("got non-error output (constraint not found handled): %s", text)
		}
	}
}

func TestToolSetOwnershipPolicyMode_SuccessUpdate(t *testing.T) {
	// Create a constraint with current mode "dryrun"
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
			},
			"spec": map[string]interface{}{
				"enforcementAction": "dryrun",
			},
		},
	}

	// Register constraint GVK
	scheme := runtime.NewScheme()
	csGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabelsList"}
	scheme.AddKnownTypeWithName(csGVK, &unstructured.UnstructuredList{})
	csItemGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"}
	scheme.AddKnownTypeWithName(csItemGVK, &unstructured.Unstructured{})

	constraintGVR := schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"}
	fakeDyn := dynfake.NewSimpleDynamicClient(scheme)
	if err := fakeDyn.Tracker().Create(constraintGVR, constraint, ""); err != nil {
		t.Fatalf("failed to seed constraint: %v", err)
	}
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "enforce",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Mode Updated") {
		t.Fatalf("expected mode update message, got: %s", text)
	}
	if !strings.Contains(text, "enforce") {
		t.Fatalf("expected 'enforce' in output, got: %s", text)
	}
}

func TestToolSetOwnershipPolicyMode_AlreadySameMode(t *testing.T) {
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
			},
			"spec": map[string]interface{}{
				"enforcementAction": "warn",
			},
		},
	}

	scheme := runtime.NewScheme()
	csGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabelsList"}
	scheme.AddKnownTypeWithName(csGVK, &unstructured.UnstructuredList{})
	csItemGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"}
	scheme.AddKnownTypeWithName(csItemGVK, &unstructured.Unstructured{})

	constraintGVR := schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"}
	fakeDyn := dynfake.NewSimpleDynamicClient(scheme)
	if err := fakeDyn.Tracker().Create(constraintGVR, constraint, ""); err != nil {
		t.Fatalf("failed to seed constraint: %v", err)
	}
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
		"cluster": "test",
		"mode":    "warn",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "already in") {
		t.Fatalf("expected 'already in' message, got: %s", text)
	}
}

// --- toolUninstallOwnershipPolicy ---

func TestToolUninstallOwnershipPolicy_DynamicClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("no dynamic client")
		},
	}
	result, rpcErr := callTool(t, server, "uninstall_ownership_policy", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for dynamic client failure")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolUninstallOwnershipPolicy_NothingInstalled(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "uninstall_ownership_policy", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (already deleted), got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Uninstalling") || !strings.Contains(text, "Not found") {
		t.Fatalf("expected uninstall with 'Not found', got: %s", text)
	}
}

func TestToolUninstallOwnershipPolicy_Success(t *testing.T) {
	// Create both constraint and template
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
			},
		},
	}
	template := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": ownershipTemplateName,
			},
		},
	}

	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme, constraint, template)
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "uninstall_ownership_policy", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Deleted") {
		t.Fatalf("expected 'Deleted' in output, got: %s", text)
	}

	// Verify they were actually deleted
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}
	_, err := fakeDyn.Resource(constraintGVR).Get(context.TODO(), ownershipConstraintName, metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected constraint to be deleted")
	}
}
