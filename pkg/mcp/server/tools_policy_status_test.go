package server

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// newPolicyStatusTestServer wires a Server with a dynamic fake client
// and seeds it with the given objects at the correct GVRs.
//
// The stock newPolicyTestServer helper relies on the fake client's
// UnsafeGuessKindToResource pluralisation, which turns
// "K8sRequiredLabels" into "k8srequiredlabelses" — but the production
// code under test queries the resource as "k8srequiredlabels". As a
// result, seeded constraint objects appear absent to the code path.
// Here we seed via the fake tracker's Create at the exact GVR the
// production code will hit.
func newPolicyStatusTestServer(t *testing.T, template *unstructured.Unstructured, constraint *unstructured.Unstructured) *Server {
	t.Helper()
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "templates.gatekeeper.sh", Version: "v1", Resource: "constrainttemplates"}: "ConstraintTemplateList",
		{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"}: "K8sRequiredLabelsList",
	}
	fakeDyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(dynamicScheme, gvrToListKind)
	tmplGVR := schema.GroupVersionResource{Group: "templates.gatekeeper.sh", Version: "v1", Resource: "constrainttemplates"}
	consGVR := schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"}
	if template != nil {
		if err := fakeDyn.Tracker().Create(tmplGVR, template, ""); err != nil {
			t.Fatalf("seed ConstraintTemplate: %v", err)
		}
	}
	if constraint != nil {
		if err := fakeDyn.Tracker().Create(consGVR, constraint, ""); err != nil {
			t.Fatalf("seed K8sRequiredLabels constraint: %v", err)
		}
	}
	return &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
}

// These tests target the previously-uncovered installed-and-configured
// branches of toolGetOwnershipPolicyStatus in tools_policy.go. The existing
// tools_policy_test.go suite covers only the client-error and template-
// missing paths (Get returns NotFound); this file covers the paths where
// the ConstraintTemplate and the K8sRequiredLabels constraint actually
// exist in the cluster, exercising:
//
//   * template status.created rendering,
//   * "Constraint: Not created" when template is present but constraint
//     is absent,
//   * enforcementAction defaulting to "deny" when the spec omits it,
//   * required-labels, excludedNamespaces, and totalViolations rendering,
//   * a template with no status.created field.

var (
	constraintTemplateGVK = schema.GroupVersionKind{
		Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate",
	}
	requiredLabelsGVK = schema.GroupVersionKind{
		Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels",
	}
)

func ownershipTemplate(created bool, setStatus bool) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(constraintTemplateGVK)
	obj.SetName(ownershipTemplateName)
	if setStatus {
		_ = unstructured.SetNestedField(obj.Object, created, "status", "created")
	}
	return obj
}

func ownershipConstraint(mods ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(requiredLabelsGVK)
	obj.SetName(ownershipConstraintName)
	for _, m := range mods {
		m(obj)
	}
	return obj
}

func withEnforcementAction(action string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(u.Object, action, "spec", "enforcementAction")
	}
}

func withRequiredLabels(labels ...string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		anyLabels := make([]interface{}, len(labels))
		for i, l := range labels {
			anyLabels[i] = l
		}
		_ = unstructured.SetNestedSlice(u.Object, anyLabels, "spec", "parameters", "labels")
	}
}

func withExcludedNamespaces(namespaces ...string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		anyNS := make([]interface{}, len(namespaces))
		for i, n := range namespaces {
			anyNS[i] = n
		}
		_ = unstructured.SetNestedSlice(u.Object, anyNS, "spec", "match", "excludedNamespaces")
	}
}

func withTotalViolations(count int64) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(u.Object, count, "status", "totalViolations")
	}
}

func TestToolGetOwnershipPolicyStatus_TemplatePresent_ConstraintMissing(t *testing.T) {
	server := newPolicyStatusTestServer(t, ownershipTemplate(true, true), nil)
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	for _, want := range []string{ownershipTemplateName, "created: true", "Not created"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output, got: %s", want, text)
		}
	}
}

func TestToolGetOwnershipPolicyStatus_ConstraintPresent_DefaultsEnforcementActionToDeny(t *testing.T) {
	// Constraint exists but spec.enforcementAction is empty — the code
	// under test defaults it to "deny".
	server := newPolicyStatusTestServer(t,
		ownershipTemplate(true, true),
		ownershipConstraint(),
	)
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "**Mode:** deny") {
		t.Fatalf("expected default enforcementAction 'deny', got: %s", text)
	}
	if strings.Contains(text, "Not created") {
		t.Fatalf("did not expect 'Not created' branch when constraint exists, got: %s", text)
	}
}

func TestToolGetOwnershipPolicyStatus_FullyConfigured(t *testing.T) {
	server := newPolicyStatusTestServer(t,
		ownershipTemplate(true, true),
		ownershipConstraint(
			withEnforcementAction("warn"),
			withRequiredLabels("owner", "team"),
			withExcludedNamespaces("kube-system", "kube-public"),
			withTotalViolations(7),
		),
	)
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	for _, want := range []string{
		"**Mode:** warn",
		"**Required Labels:** owner, team",
		"**Excluded Namespaces:** kube-system, kube-public",
		"**Total Violations:** 7",
		ownershipConstraintName,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output, got: %s", want, text)
		}
	}
}

func TestToolGetOwnershipPolicyStatus_TemplateWithoutStatusCreated(t *testing.T) {
	// A template that hasn't reported status.created yet — NestedBool
	// returns false, and the tool renders "created: false" without
	// erroring or entering the "Not installed" branch.
	server := newPolicyStatusTestServer(t, ownershipTemplate(false, false), nil)
	result, rpcErr := callTool(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "test-cluster"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "created: false") {
		t.Fatalf("expected 'created: false' in output, got: %s", text)
	}
	if strings.Contains(text, "Not installed") {
		t.Fatalf("did not expect 'Not installed' when the template exists but lacks status, got: %s", text)
	}
}
