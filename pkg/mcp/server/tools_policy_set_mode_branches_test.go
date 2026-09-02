package server

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Helpers for building a fake dynamic client seeded with the ownership
// gatekeeper constraint. The scheme registration mirrors the existing
// helper in tools_policy_coverage_test.go (kept local to avoid touching
// the older file's structure).
func newConstraintClient(t *testing.T, enforcementAction string) *dynfake.FakeDynamicClient {
	t.Helper()
	spec := map[string]interface{}{}
	if enforcementAction != "" {
		spec["enforcementAction"] = enforcementAction
	}
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
			},
			"spec": spec,
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
	return fakeDyn
}

// Lock the "current mode defaults to deny when enforcementAction is absent"
// arm — a constraint object whose spec has no enforcementAction key MUST
// be reported as previous-mode `deny`, and the switch to a new mode MUST
// succeed. Coverage: `if currentMode == "" { currentMode = "deny" }` +
// success path.
func TestToolSetOwnershipPolicyMode_EmptyCurrentModeDefaultsToDeny(t *testing.T) {
	fakeDyn := newConstraintClient(t, "")
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(_ string) (dynamic.Interface, error) {
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
	if !strings.Contains(text, "Previous Mode:** deny") {
		t.Fatalf("expected previous-mode default 'deny' in output, got: %s", text)
	}
	if !strings.Contains(text, "New Mode:** warn") {
		t.Fatalf("expected new mode 'warn', got: %s", text)
	}
}

// Lock the switch-arm for `mode == "dryrun"` — success output MUST mention
// that violations are logged but resources are NOT blocked, matching the
// dryrun contract expected by ops runbooks.
func TestToolSetOwnershipPolicyMode_DryrunSwitchArm(t *testing.T) {
	fakeDyn := newConstraintClient(t, "enforce")
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(_ string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
	result, rpcErr := callTool(t, server, "set_ownership_policy_mode", map[string]interface{}{
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
	if !strings.Contains(text, "logged but resources are **NOT blocked**") {
		t.Fatalf("expected dryrun switch-arm message, got: %s", text)
	}
	if !strings.Contains(text, "Previous Mode:** enforce") {
		t.Fatalf("expected previous mode 'enforce', got: %s", text)
	}
}

// Lock the switch-arm for `mode == "warn"` — success output MUST warn about
// user-visible warnings without blocking. Complements the AlreadySameMode
// test which short-circuits BEFORE reaching the switch.
func TestToolSetOwnershipPolicyMode_WarnSwitchArm(t *testing.T) {
	fakeDyn := newConstraintClient(t, "dryrun")
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(_ string) (dynamic.Interface, error) {
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
	if !strings.Contains(text, "Users will see warnings but resources are **NOT blocked**") {
		t.Fatalf("expected warn switch-arm message, got: %s", text)
	}
}

// Lock the Update-error path — when the dynamic client's Update call
// fails, the RPC MUST surface the failure with an isError result and a
// "Failed to update constraint" prefix. A silent success on Update
// failure would leave the cluster in an inconsistent state where the
// tool reports mode-changed but Gatekeeper is still enforcing the old
// mode.
func TestToolSetOwnershipPolicyMode_UpdateError(t *testing.T) {
	fakeDyn := newConstraintClient(t, "dryrun")
	fakeDyn.PrependReactor("update", "k8srequiredlabels", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("webhook denied update")
	})
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(_ string) (dynamic.Interface, error) {
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
	if !result.IsError {
		t.Fatalf("expected isError true on Update failure, got success: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to update constraint") {
		t.Fatalf("expected 'Failed to update constraint' in output, got: %s", text)
	}
	if !strings.Contains(text, "webhook denied update") {
		t.Fatalf("expected wrapped error message in output, got: %s", text)
	}
}
