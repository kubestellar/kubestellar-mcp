package rbac

import (
	"errors"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// -----------------------------------------------------------------------
// toolAnalyzeSubjectPermissions
// -----------------------------------------------------------------------

// TestToolAnalyzeSubjectPermissionsClientError covers the
// "Failed to create client" arm — the factory error must surface with
// IsError=true rather than the RBAC-analysis being silently skipped.
func TestToolAnalyzeSubjectPermissionsClientError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("kubeconfig missing for cluster")
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
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

// TestToolAnalyzeSubjectPermissionsListCRBError covers the
// "Failed to list cluster role bindings" arm. The tool must not fall
// through to the RoleBindings list step when the CRB list fails, because
// the caller would see a misleading partial RBAC report.
func TestToolAnalyzeSubjectPermissionsListCRBError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "clusterrolebindings",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("crb list denied")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for CRB list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list cluster role bindings") ||
		!strings.Contains(text, "crb list denied") {
		t.Fatalf("expected 'Failed to list cluster role bindings: crb list denied', got: %s", text)
	}
}

// TestToolAnalyzeSubjectPermissionsListRBError covers the
// "Failed to list role bindings" arm. CRB list succeeds (empty) but the
// namespaced RB list fails; the tool must not silently emit "No RBAC
// bindings found".
func TestToolAnalyzeSubjectPermissionsListRBError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			client.PrependReactor("list", "rolebindings",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("rb list denied")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for RB list failure, got: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Failed to list role bindings") ||
		!strings.Contains(text, "rb list denied") {
		t.Fatalf("expected 'Failed to list role bindings: rb list denied', got: %s", text)
	}
}

// TestToolAnalyzeSubjectPermissionsClusterRoleGetError covers the
// "error fetching" per-role branch inside the CRB loop: a matching CRB
// references a ClusterRole whose Get() fails. The tool must skip the
// individual role (with an inline error note) and continue rather than
// aborting the whole analysis.
func TestToolAnalyzeSubjectPermissionsClusterRoleGetError(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "alice-binding"},
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "alice"},
					},
					RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "missing-role"},
				},
			)
			client.PrependReactor("get", "clusterroles",
				func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("cluster role fetch failed")
				})
			return client, nil
		},
	}
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "alice",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (soft-fail per role), got IsError: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "missing-role") ||
		!strings.Contains(text, "error fetching") ||
		!strings.Contains(text, "cluster role fetch failed") {
		t.Fatalf("expected inline '- missing-role (error fetching: cluster role fetch failed)', got: %s", text)
	}
}
