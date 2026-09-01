package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestToolInstallOwnershipPolicy_EnforceMode covers the default arm of the
// Next-Steps switch — anything other than dryrun/warn falls through to the
// enforce writeup (tools_policy.go: default: sb.WriteString("The policy is in
// **enforce** mode…")).
func TestToolInstallOwnershipPolicy_EnforceMode(t *testing.T) {
	server := newPolicyTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "enforce",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "**Mode:** enforce") {
		t.Errorf("expected enforce mode header, got: %s", text)
	}
	if !strings.Contains(text, "BLOCKED") {
		t.Errorf("expected 'BLOCKED' warning in enforce mode, got: %s", text)
	}
}

// TestToolInstallOwnershipPolicy_OpenShiftDetection covers the isOpenShift
// branch: when the "openshift" namespace exists, the excluded-namespace list
// is expanded with the openshift-* system namespaces.
func TestToolInstallOwnershipPolicy_OpenShiftDetection(t *testing.T) {
	openshiftNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift"},
	}
	server := newPolicyTestServer([]runtime.Object{openshiftNS}, nil)
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "ocp",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	// Default excludes (4) + 34 openshift-* excludes = 38.
	if !strings.Contains(text, "**Excluded Namespaces:** 38 namespaces") {
		t.Errorf("expected OpenShift namespaces to be appended (37 total), got: %s", text)
	}
}

// TestToolInstallOwnershipPolicy_ConstraintTemplateAlreadyExists covers the
// "already exists" branch on the ConstraintTemplate Create — the code falls
// through to Update() and reports "Updated ✓".
func TestToolInstallOwnershipPolicy_ConstraintTemplateAlreadyExists(t *testing.T) {
	// Preload a ConstraintTemplate object so Create() returns AlreadyExists.
	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintTemplateAPIVersion,
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": ownershipTemplateName,
			},
		},
	}
	server := newPolicyTestServer(nil, []runtime.Object{existing})
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success on existing template, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "**ConstraintTemplate:** Already exists (updating...)") {
		t.Errorf("expected 'Already exists (updating...)', got: %s", text)
	}
	if !strings.Contains(text, "**ConstraintTemplate:** Updated ✓") {
		t.Errorf("expected 'Updated ✓', got: %s", text)
	}
}

// TestToolInstallOwnershipPolicy_ConstraintTemplateCreateError covers the
// non-"already exists" error branch on ConstraintTemplate Create — the tool
// bails out with "Failed to create ConstraintTemplate: …".
func TestToolInstallOwnershipPolicy_ConstraintTemplateCreateError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme)
	// Fail create on templates.gatekeeper.sh/v1 constrainttemplates only.
	fakeDyn.PrependReactor("create", "constrainttemplates", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("webhook rejected")
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return fakeK8s, nil },
		dynamicClientFactory: func(string) (dynamic.Interface, error) { return fakeDyn, nil },
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error on ConstraintTemplate create failure")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create ConstraintTemplate") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

// TestToolInstallOwnershipPolicy_ConstraintCreateError covers the
// non-"already exists" error branch on Constraint Create (after the
// ConstraintTemplate succeeded).
func TestToolInstallOwnershipPolicy_ConstraintCreateError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme)
	fakeDyn.PrependReactor("create", "k8srequiredlabels", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("constraint create rejected")
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return fakeK8s, nil },
		dynamicClientFactory: func(string) (dynamic.Interface, error) { return fakeDyn, nil },
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error on Constraint create failure")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create Constraint") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

// TestToolInstallOwnershipPolicy_ConstraintAlreadyExists covers the Constraint
// "already exists" → get + update path. Use a reactor that returns an
// AlreadyExists-shaped error on Create; the code then Get()s and Update()s
// the constraint in place.
func TestToolInstallOwnershipPolicy_ConstraintAlreadyExists(t *testing.T) {
	// Preload the existing constraint so the subsequent Get() succeeds.
	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintAPIVersion,
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name":            ownershipConstraintName,
				"resourceVersion": "42",
			},
		},
	}
	fakeK8s := k8sfake.NewSimpleClientset()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme, existing)
	// The dynamic fake may or may not raise AlreadyExists on Create for an
	// already-preloaded item depending on the resource mapping. Force it
	// with a reactor whose error message contains the "already exists"
	// substring the production code checks for.
	fakeDyn.PrependReactor("create", "k8srequiredlabels", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(
			schema.GroupResource{Group: "constraints.gatekeeper.sh", Resource: "k8srequiredlabels"},
			ownershipConstraintName,
		)
	})
	// The subsequent Get must also succeed; the dynamic fake stores the
	// preloaded object but its resource-scope mapping is fragile — serve
	// it directly so the branch is exercised deterministically.
	fakeDyn.PrependReactor("get", "k8srequiredlabels", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, existing, nil
	})
	fakeDyn.PrependReactor("update", "k8srequiredlabels", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, existing, nil
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(string) (kubernetes.Interface, error) { return fakeK8s, nil },
		dynamicClientFactory: func(string) (dynamic.Interface, error) { return fakeDyn, nil },
	}
	result, rpcErr := callTool(t, server, "install_ownership_policy", map[string]interface{}{
		"cluster": "test",
		"mode":    "dryrun",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success on existing constraint, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "**Constraint:** Already exists (updating...)") {
		t.Errorf("expected 'Already exists (updating...)', got: %s", text)
	}
	if !strings.Contains(text, "**Constraint:** Updated ✓") {
		t.Errorf("expected 'Updated ✓', got: %s", text)
	}
}

// unusedForImportBalance keeps schema/apierrors imports referenced so future
// tests can extend this file without re-adding them.
var _ = schema.GroupVersionResource{}
var _ = apierrors.IsAlreadyExists
