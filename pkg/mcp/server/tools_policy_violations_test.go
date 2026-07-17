package server

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

// newViolationServer returns a *Server whose dynamic client already has the
// supplied ownership constraint seeded (or none if nil).
func newViolationServer(t *testing.T, constraint *unstructured.Unstructured) *Server {
	t.Helper()
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme)
	if constraint != nil {
		gvr := schema.GroupVersionResource{
			Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels",
		}
		if err := fakeDyn.Tracker().Create(gvr, constraint, ""); err != nil {
			t.Fatalf("seed constraint: %v", err)
		}
	}
	return &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
}

// makeOwnershipConstraint builds a K8sRequiredLabels constraint named
// `require-ownership-labels` with the supplied enforcement action and
// status.violations slice.
func makeOwnershipConstraint(enforcementAction string, totalViolations int64, violations []map[string]interface{}) *unstructured.Unstructured {
	spec := map[string]interface{}{}
	if enforcementAction != "" {
		spec["enforcementAction"] = enforcementAction
	}

	violIfaces := make([]interface{}, 0, len(violations))
	for _, v := range violations {
		violIfaces = append(violIfaces, v)
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata":   map[string]interface{}{"name": ownershipConstraintName},
			"spec":       spec,
			"status": map[string]interface{}{
				"totalViolations": totalViolations,
				"violations":      violIfaces,
			},
		},
	}
}

// --- toolListOwnershipViolations extended tests (was 18.8% coverage) ---

func TestToolListOwnershipViolations_InvalidNamespace(t *testing.T) {
	server := newViolationServer(t, nil)
	// kube-system is disallowed by ValidateNamespace.
	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{
		"cluster":   "test-cluster",
		"namespace": "kube-system",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for kube-system namespace, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolListOwnershipViolations_NoViolations(t *testing.T) {
	constraint := makeOwnershipConstraint("", 0, nil)
	server := newViolationServer(t, constraint)

	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{
		"cluster": "test-cluster",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	mustContain(t, text, "**Mode:** deny") // empty enforcementAction defaults to "deny"
	mustContain(t, text, "**No violations found!**")
}

func TestToolListOwnershipViolations_ManyViolationsWithLimit(t *testing.T) {
	// 4 violations across 2 namespaces + one with a very long message (>50 chars)
	// to hit the message-truncation branch.
	longMsg := "you must provide labels: " + strings.Repeat("x", 100)
	violations := []map[string]interface{}{
		{"kind": "Deployment", "name": "web", "namespace": "app-a", "message": "missing labels: owner, team"},
		{"kind": "Service", "name": "svc", "namespace": "app-a", "message": longMsg},
		{"kind": "Deployment", "name": "api", "namespace": "app-b", "message": "missing labels: owner"},
		{"kind": "StatefulSet", "name": "db", "namespace": "app-b", "message": "missing labels: team"},
	}
	constraint := makeOwnershipConstraint("dryrun", 4, violations)
	server := newViolationServer(t, constraint)

	// limit=2 -> only 2 rows rendered + truncation notice.
	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{
		"cluster": "test-cluster",
		"limit":   float64(2),
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text

	// Custom enforcement action preserved.
	mustContain(t, text, "**Mode:** dryrun")
	// Total from status.totalViolations.
	mustContain(t, text, "**Total Violations:** 4")
	// By-namespace summary rendered (map iteration order is random so just
	// verify both namespaces appear).
	mustContain(t, text, "## By Namespace")
	mustContain(t, text, "**app-a**: 2 violations")
	mustContain(t, text, "**app-b**: 2 violations")
	// Details table header.
	mustContain(t, text, "| Namespace | Kind | Name | Issue |")
	// Message truncated to 47 chars + "..." (long message case).
	mustContain(t, text, "...")
	// Truncation notice with correct counts.
	mustContain(t, text, "Showing 2 of 4 violations")
	// Table body must contain exactly 2 data rows — sanity check row count.
	tableStart := strings.Index(text, "|-----------|")
	if tableStart == -1 {
		t.Fatalf("could not find markdown table separator in output:\n%s", text)
	}
	rows := 0
	for _, line := range strings.Split(text[tableStart:], "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "|-") && !strings.HasPrefix(line, "| Namespace ") {
			rows++
		}
	}
	if rows != 2 {
		t.Fatalf("expected exactly 2 rendered violation rows (limit=2), got %d in:\n%s", rows, text)
	}
}

func TestToolListOwnershipViolations_NamespaceFilterMatch(t *testing.T) {
	violations := []map[string]interface{}{
		{"kind": "Deployment", "name": "web", "namespace": "app-a", "message": "missing labels"},
		{"kind": "Deployment", "name": "api", "namespace": "app-b", "message": "missing labels"},
	}
	constraint := makeOwnershipConstraint("warn", 2, violations)
	server := newViolationServer(t, constraint)

	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{
		"cluster":   "test-cluster",
		"namespace": "app-a",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text

	mustContain(t, text, "**Mode:** warn")
	// Only app-a rendered in the by-namespace section.
	mustContain(t, text, "**app-a**: 1 violations")
	if strings.Contains(text, "**app-b**:") {
		t.Fatalf("expected app-b filtered out, got:\n%s", text)
	}
	// app-a table row present, app-b not.
	mustContain(t, text, "| app-a | Deployment | web |")
	if strings.Contains(text, "| app-b |") {
		t.Fatalf("expected app-b row filtered out, got:\n%s", text)
	}
}

func TestToolListOwnershipViolations_NamespaceFilterNoMatch(t *testing.T) {
	violations := []map[string]interface{}{
		{"kind": "Deployment", "name": "web", "namespace": "app-a", "message": "missing labels"},
	}
	constraint := makeOwnershipConstraint("deny", 1, violations)
	server := newViolationServer(t, constraint)

	result, rpcErr := callTool(t, server, "list_ownership_violations", map[string]interface{}{
		"cluster":   "test-cluster",
		"namespace": "app-nothere",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	mustContain(t, text, "No violations in namespace `app-nothere`.")
	// The "## By Namespace" section must NOT be rendered when the filtered
	// list is empty (early-return branch).
	if strings.Contains(text, "## By Namespace") {
		t.Fatalf("did not expect By Namespace section when filter matches zero, got:\n%s", text)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in output:\n%s", needle, haystack)
	}
}
