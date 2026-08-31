package server

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Closes previously-uncovered error and all-namespaces arms in
// pkg/mcp/server/tools_workloads.go. Baseline coverage (from
// `go test -short -coverprofile ./pkg/mcp/server/...`):
//
//   toolGetDeployments   73.3%   (namespace-err, client-err,
//                                  all-ns branch, list-err all cov0)
//   toolGetServices      ~80%    (client-err + list-err cov0;
//                                  namespace-err cov0)
//
// Existing tests only exercise the happy path with an explicit
// namespace. The four error branches all return `IsError = true`
// with a distinct prefix, so a regression that silently swallowed
// them and returned an empty result would ship without any test
// signal.

// -----------------------------------------------------------------------
// toolGetDeployments
// -----------------------------------------------------------------------

func TestToolGetDeploymentsAllNamespacesSuccess(t *testing.T) {
	// namespace == "" arm — the tool must list Deployments across
	// every namespace via `.Deployments("")`. The fake client returns
	// all seeded objects when given the empty namespace selector.
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "jobs"},
				},
			), nil
		},
	}

	// No `namespace` key → extractAndValidateNamespace returns "",nil,
	// so we take the namespace == "" branch on tools_workloads.go:88.
	result, rpcErr := callTool(t, server, "get_deployments", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	// The tool JSON-marshals the DeploymentList — both seeded names
	// must appear, proving we walked the "all namespaces" arm.
	text := result.Content[0].Text
	if !strings.Contains(text, "\"api\"") || !strings.Contains(text, "\"worker\"") {
		t.Fatalf("expected both deployments in output, got: %s", text)
	}
}

func TestToolGetDeploymentsNamespaceValidationError(t *testing.T) {
	// A namespace that violates RFC1123 must be rejected by
	// extractAndValidateNamespace BEFORE any client is created.
	// Locks the "error: %v" prefix that upstream callers depend on
	// to distinguish validation errors from client / list errors.
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			t.Fatalf("clientFactory must not be called when validation fails")
			return nil, nil
		},
	}
	result, rpcErr := callTool(t, server, "get_deployments", map[string]interface{}{
		"namespace": "Invalid_NS",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	if !strings.HasPrefix(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetDeploymentsClientFactoryError(t *testing.T) {
	// getClientForCluster failure must surface as
	// "Failed to create client: ..." — a distinct prefix so callers
	// can tell it apart from a list-time failure.
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing for cluster")
		},
	}
	result, rpcErr := callTool(t, server, "get_deployments", map[string]interface{}{
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
		t.Fatalf("expected 'Failed to create client: kubeconfig missing...' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetDeploymentsListError(t *testing.T) {
	// PrependReactor on `list deployments` returns an error, so the
	// tool must surface it via the "Failed to list deployments" arm.
	// Guards against regressions that would (e.g.) swallow the error
	// and return the zero DeploymentList as valid JSON.
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "deployments",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("deployment list failed")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "get_deployments", map[string]interface{}{
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list deployments") ||
		!strings.Contains(text, "deployment list failed") {
		t.Fatalf("expected 'Failed to list deployments: deployment list failed', got: %s", text)
	}
}

// -----------------------------------------------------------------------
// toolGetServices
// -----------------------------------------------------------------------

func TestToolGetServicesNamespaceValidationError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			t.Fatalf("clientFactory must not be called when validation fails")
			return nil, nil
		},
	}
	result, rpcErr := callTool(t, server, "get_services", map[string]interface{}{
		"namespace": "Invalid_NS",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for invalid namespace, got success: %s", result.Content[0].Text)
	}
	if !strings.HasPrefix(result.Content[0].Text, "error:") {
		t.Fatalf("expected 'error:' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetServicesClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no such cluster")
		},
	}
	result, rpcErr := callTool(t, server, "get_services", map[string]interface{}{
		"namespace": "ingress",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got success: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") ||
		!strings.Contains(result.Content[0].Text, "no such cluster") {
		t.Fatalf("expected 'Failed to create client: no such cluster' prefix, got: %s", result.Content[0].Text)
	}
}

func TestToolGetServicesListError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "services",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("service list failed")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "get_services", map[string]interface{}{
		"namespace": "ingress",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list services") ||
		!strings.Contains(text, "service list failed") {
		t.Fatalf("expected 'Failed to list services: service list failed', got: %s", text)
	}
}

func TestToolGetServicesEmptyListReturnsFriendlyMessage(t *testing.T) {
	// The `len(services.Items) == 0` early-return arm at
	// tools_workloads.go:126 currently only fires when a client with
	// no services is queried in the happy path. Existing test only
	// exercises a non-empty list. Locks the exact "No services
	// found" copy that PMs use for docs/screenshots.
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			// Seed a Service in a different namespace so the
			// filter has something to filter *out*.
			return k8sfake.NewSimpleClientset(
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"},
				},
			), nil
		},
	}
	result, rpcErr := callTool(t, server, "get_services", map[string]interface{}{
		"namespace": "empty-ns",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (non-error IsError=false), got: %s", result.Content[0].Text)
	}
	if result.Content[0].Text != "No services found" {
		t.Fatalf("expected exactly 'No services found', got: %q", result.Content[0].Text)
	}
}
