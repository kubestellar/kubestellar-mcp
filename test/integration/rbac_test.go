//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/rbac"
)

// TestRBACRolesAndBindings exercises pkg/mcp/server/rbac's list/describe MCP
// tools end-to-end against a real kube-apiserver (envtest): a Role,
// ClusterRole, RoleBinding, and ClusterRoleBinding are created directly via
// the typed clientset, then each tool is invoked exactly as the real
// protocol server dispatches it — via rbac.Register into a handlers.Registry
// and handlers.Registry.Find(name) — to confirm the RbacV1 List/Get calls
// and output formatting work against a real API server rather than the
// fake.NewSimpleClientset the package's own unit tests use
// (get_roles_branches_test.go, analyze_branches_test.go, etc.).
//
// Covered tools:
//   - get_roles                 (rbac.go:22)
//   - get_cluster_roles         (rbac.go:62)
//   - get_role_bindings         (rbac.go:100)
//   - get_cluster_role_bindings (rbac.go:142)
//   - describe_role             (rbac.go:355, both Role and ClusterRole
//                                branches)
//
// can_i and analyze_subject_permissions are not exercised here: SelfSubject/
// SubjectAccessReview against the default envtest apiserver returns a
// short-circuit "allowed by RBAC authorizer bypass" answer that does not
// reflect the created Role/Binding, so the assertion surface would test the
// bypass rather than the tool's real behavior. audit_kubeconfig and
// find_resource_owners depend on a kubeconfig / dynamic-client shape that
// is out of scope for the envtest harness.
func TestRBACRolesAndBindings(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}

	const namespace = "mcp-integration-rbac"
	const roleName = "mcp-integration-role"
	const clusterRoleName = "mcp-integration-clusterrole"
	const roleBindingName = "mcp-integration-rolebinding"
	const clusterRoleBindingName = "mcp-integration-clusterrolebinding"
	const subjectName = "mcp-integration-sa"

	if _, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list"},
			},
		},
	}
	if _, err := clientset.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create role: %v", err)
	}

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				NonResourceURLs: []string{"/healthz"},
				Verbs:           []string{"get"},
			},
		},
	}
	if _, err := clientset.RbacV1().ClusterRoles().Create(ctx, clusterRole, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create cluster role: %v", err)
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: roleBindingName, Namespace: namespace},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Name: subjectName, Namespace: namespace},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}
	if _, err := clientset.RbacV1().RoleBindings(namespace).Create(ctx, roleBinding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create role binding: %v", err)
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Name: subjectName, Namespace: namespace},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
	}
	if _, err := clientset.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create cluster role binding: %v", err)
	}

	reg := handlers.NewRegistry()
	rbac.Register(reg)

	deps := &handlers.Deps{
		ClientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return clientset, nil
		},
	}

	t.Run("get_roles", func(t *testing.T) {
		handler := reg.Find("get_roles")
		if handler == nil {
			t.Fatal("get_roles tool not found in registry after rbac.Register")
		}
		output, isError := handler(ctx, deps, map[string]interface{}{
			"namespace": namespace,
		})
		if isError {
			t.Fatalf("get_roles returned an error result: %s", output)
		}
		if !strings.Contains(output, roleName) {
			t.Fatalf("get_roles output does not mention role %q: %s", roleName, output)
		}
		if !strings.Contains(output, "Found 1 roles") {
			t.Fatalf("get_roles output does not report 1 role found: %s", output)
		}
	})

	t.Run("get_cluster_roles", func(t *testing.T) {
		handler := reg.Find("get_cluster_roles")
		if handler == nil {
			t.Fatal("get_cluster_roles tool not found in registry after rbac.Register")
		}
		// include_system defaults to false; our clusterrole name has no
		// system:/kubeadm: prefix so it must be listed.
		output, isError := handler(ctx, deps, map[string]interface{}{})
		if isError {
			t.Fatalf("get_cluster_roles returned an error result: %s", output)
		}
		if !strings.Contains(output, clusterRoleName) {
			t.Fatalf("get_cluster_roles output does not mention clusterrole %q: %s", clusterRoleName, output)
		}
		// The system-role filter must actually filter: every default envtest
		// cluster ships with system:* cluster roles, so their absence here
		// confirms the include_system=false branch is exercised end-to-end
		// against a real apiserver.
		if strings.Contains(output, "system:") {
			t.Fatalf("get_cluster_roles included a system: cluster role when include_system was not set: %s", output)
		}
	})

	t.Run("get_role_bindings", func(t *testing.T) {
		handler := reg.Find("get_role_bindings")
		if handler == nil {
			t.Fatal("get_role_bindings tool not found in registry after rbac.Register")
		}
		output, isError := handler(ctx, deps, map[string]interface{}{
			"namespace": namespace,
		})
		if isError {
			t.Fatalf("get_role_bindings returned an error result: %s", output)
		}
		if !strings.Contains(output, roleBindingName) {
			t.Fatalf("get_role_bindings output does not mention rolebinding %q: %s", roleBindingName, output)
		}
	})

	t.Run("get_cluster_role_bindings", func(t *testing.T) {
		handler := reg.Find("get_cluster_role_bindings")
		if handler == nil {
			t.Fatal("get_cluster_role_bindings tool not found in registry after rbac.Register")
		}
		output, isError := handler(ctx, deps, map[string]interface{}{})
		if isError {
			t.Fatalf("get_cluster_role_bindings returned an error result: %s", output)
		}
		if !strings.Contains(output, clusterRoleBindingName) {
			t.Fatalf("get_cluster_role_bindings output does not mention clusterrolebinding %q: %s", clusterRoleBindingName, output)
		}
	})

	t.Run("describe_role_namespaced", func(t *testing.T) {
		handler := reg.Find("describe_role")
		if handler == nil {
			t.Fatal("describe_role tool not found in registry after rbac.Register")
		}
		output, isError := handler(ctx, deps, map[string]interface{}{
			"name":      roleName,
			"namespace": namespace,
		})
		if isError {
			t.Fatalf("describe_role (Role) returned an error result: %s", output)
		}
		if !strings.Contains(output, "Role: "+namespace+"/"+roleName) {
			t.Fatalf("describe_role (Role) missing header for %s/%s: %s", namespace, roleName, output)
		}
		if !strings.Contains(output, "configmaps") || !strings.Contains(output, "Verbs: get, list") {
			t.Fatalf("describe_role (Role) missing rule details: %s", output)
		}
	})

	t.Run("describe_role_cluster", func(t *testing.T) {
		handler := reg.Find("describe_role")
		if handler == nil {
			t.Fatal("describe_role tool not found in registry after rbac.Register")
		}
		output, isError := handler(ctx, deps, map[string]interface{}{
			"name": clusterRoleName,
		})
		if isError {
			t.Fatalf("describe_role (ClusterRole) returned an error result: %s", output)
		}
		if !strings.Contains(output, "ClusterRole: "+clusterRoleName) {
			t.Fatalf("describe_role (ClusterRole) missing header for %s: %s", clusterRoleName, output)
		}
		// The core-group rewrite ("" -> "core") is a formatting decision
		// that only fires for ClusterRoles; assert it here so the branch
		// stays wired against a real apiserver.
		if !strings.Contains(output, "API Groups: core") {
			t.Fatalf("describe_role (ClusterRole) did not rewrite empty API group to 'core': %s", output)
		}
		if !strings.Contains(output, "Non-Resource URLs: /healthz") {
			t.Fatalf("describe_role (ClusterRole) missing non-resource URL rule: %s", output)
		}
	})
}
