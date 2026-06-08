package server

import (
	"strings"
	"testing"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestToolGetRolesSuccess(t *testing.T) {
	now := metav1.NewTime(time.Date(2024, time.March, 15, 10, 0, 0, 0, time.UTC))
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pod-reader",
						Namespace:         "apps",
						CreationTimestamp: now,
					},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "deploy-manager",
						Namespace:         "apps",
						CreationTimestamp: now,
					},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list", "update"}},
						{APIGroups: []string{"apps"}, Resources: []string{"replicasets"}, Verbs: []string{"get", "list"}},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_roles", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 roles") {
		t.Fatalf("expected 'Found 2 roles' in output, got: %s", text)
	}
	if !strings.Contains(text, "pod-reader") {
		t.Fatalf("expected 'pod-reader' in output, got: %s", text)
	}
	if !strings.Contains(text, "deploy-manager") {
		t.Fatalf("expected 'deploy-manager' in output, got: %s", text)
	}
	if !strings.Contains(text, "1 rules") {
		t.Fatalf("expected '1 rules' for pod-reader, got: %s", text)
	}
	if !strings.Contains(text, "2 rules") {
		t.Fatalf("expected '2 rules' for deploy-manager, got: %s", text)
	}
}

func TestToolGetClusterRolesSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-admin-custom"},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: "system:node"},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta:      metav1.ObjectMeta{Name: "aggregated-view"},
					AggregationRule: &rbacv1.AggregationRule{},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
					},
				},
			), nil
		},
	}

	// Without include_system — should skip system:node
	result, rpcErr := callTool(t, server, "get_cluster_roles", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "cluster-admin-custom") {
		t.Fatalf("expected 'cluster-admin-custom' in output, got: %s", text)
	}
	if strings.Contains(text, "system:node") {
		t.Fatalf("system:node should be filtered out without include_system, got: %s", text)
	}
	if !strings.Contains(text, "aggregated") {
		t.Fatalf("expected '(aggregated)' marker for aggregated-view, got: %s", text)
	}

	// With include_system — should include system:node
	result, rpcErr = callTool(t, server, "get_cluster_roles", map[string]interface{}{"include_system": "true"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	text = result.Content[0].Text
	if !strings.Contains(text, "system:node") {
		t.Fatalf("system:node should be included with include_system=true, got: %s", text)
	}
}

func TestToolGetRoleBindingsSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "read-pods", Namespace: "apps"},
					RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount", Name: "ci-bot", Namespace: "apps"},
						{Kind: "User", Name: "alice"},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "get_role_bindings", map[string]interface{}{"namespace": "apps"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Found 1 role bindings") {
		t.Fatalf("expected 'Found 1 role bindings' in output, got: %s", text)
	}
	if !strings.Contains(text, "Role/pod-reader") {
		t.Fatalf("expected 'Role/pod-reader' in output, got: %s", text)
	}
	if !strings.Contains(text, "SA:apps/ci-bot") {
		t.Fatalf("expected 'SA:apps/ci-bot' in subject list, got: %s", text)
	}
	if !strings.Contains(text, "User:alice") {
		t.Fatalf("expected 'User:alice' in subject list, got: %s", text)
	}
}

func TestToolGetClusterRoleBindingsSuccess(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "admin-binding"},
					RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: "platform-admins"},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "system:node-binding"},
					RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "system:node", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "Group", Name: "system:nodes"},
					},
				},
			), nil
		},
	}

	// Without include_system
	result, rpcErr := callTool(t, server, "get_cluster_role_bindings", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "admin-binding") {
		t.Fatalf("expected 'admin-binding' in output, got: %s", text)
	}
	if strings.Contains(text, "system:node-binding") {
		t.Fatalf("system:node-binding should be filtered without include_system, got: %s", text)
	}
	if !strings.Contains(text, "Group:platform-admins") {
		t.Fatalf("expected 'Group:platform-admins' in output, got: %s", text)
	}
}

func TestToolCanIAllowed(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			client := k8sfake.NewSimpleClientset()
			// k8sfake allows all SelfSubjectAccessReviews by default (status.allowed = false)
			// We test that the tool formats the response correctly
			return client, nil
		},
	}

	result, rpcErr := callTool(t, server, "can_i", map[string]interface{}{
		"verb":      "get",
		"resource":  "pods",
		"namespace": "default",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	// The fake client returns allowed=false by default for SelfSubjectAccessReviews
	if !strings.Contains(text, "get") || !strings.Contains(text, "pods") {
		t.Fatalf("expected verb and resource in output, got: %s", text)
	}
}

func TestToolCanIValidation(t *testing.T) {
	server := &Server{discoverer: stubDiscoverer{}}

	// Missing verb
	result, rpcErr := callTool(t, server, "can_i", map[string]interface{}{"resource": "pods"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing verb")
	}
	if !strings.Contains(result.Content[0].Text, "verb and resource are required") {
		t.Fatalf("unexpected error text: %s", result.Content[0].Text)
	}

	// Missing resource
	result, rpcErr = callTool(t, server, "can_i", map[string]interface{}{"verb": "get"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing resource")
	}
}

func TestFormatSubjectsVariants(t *testing.T) {
	tests := []struct {
		name     string
		subjects []rbacv1.Subject
		want     string
	}{
		{
			name:     "empty subjects",
			subjects: nil,
			want:     "<none>",
		},
		{
			name: "mixed subjects",
			subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Name: "deployer", Namespace: "ci"},
				{Kind: "User", Name: "bob"},
				{Kind: "Group", Name: "developers"},
			},
			want: "SA:ci/deployer, User:bob, Group:developers",
		},
		{
			name: "single user",
			subjects: []rbacv1.Subject{
				{Kind: "User", Name: "admin"},
			},
			want: "User:admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSubjects(tt.subjects)
			if got != tt.want {
				t.Fatalf("formatSubjects() = %q, want %q", got, tt.want)
			}
		})
	}
}
