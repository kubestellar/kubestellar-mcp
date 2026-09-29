package rbac

import (
	"strings"
	"testing"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// --- subjectMatches ---

func TestSubjectMatchesServiceAccount(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Name: "deployer", Namespace: "ci"},
		{Kind: "User", Name: "alice"},
	}

	tests := []struct {
		name      string
		kind      string
		sName     string
		namespace string
		want      bool
	}{
		{"exact SA match", "ServiceAccount", "deployer", "ci", true},
		{"SA name match empty ns filter", "ServiceAccount", "deployer", "", true},
		{"SA wrong namespace", "ServiceAccount", "deployer", "prod", false},
		{"SA wrong name", "ServiceAccount", "builder", "ci", false},
		{"User match ignores namespace", "User", "alice", "anything", true},
		{"User not found", "User", "bob", "", false},
		{"Kind mismatch", "Group", "deployer", "ci", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subjectMatches(subjects, tt.kind, tt.sName, tt.namespace)
			if got != tt.want {
				t.Errorf("subjectMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubjectMatchesEmptyList(t *testing.T) {
	if subjectMatches(nil, "User", "alice", "") {
		t.Error("nil subjects should never match")
	}
	if subjectMatches([]rbacv1.Subject{}, "User", "alice", "") {
		t.Error("empty subjects should never match")
	}
}

func TestSubjectMatchesGroupKind(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "Group", Name: "platform-admins"},
	}

	if !subjectMatches(subjects, "Group", "platform-admins", "") {
		t.Error("expected Group match")
	}
	if subjectMatches(subjects, "Group", "developers", "") {
		t.Error("expected no match for wrong group name")
	}
}

// --- toolAnalyzeSubjectPermissions ---

func TestToolAnalyzeSubjectPermissionsValidation(t *testing.T) {
	server := &testServer{discoverer: stubDiscoverer{}}

	// Missing subject_kind
	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_name": "deployer",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing subject_kind")
	}
	if !strings.Contains(result.Content[0].Text, "subject_kind and subject_name are required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	// Missing subject_name
	result, rpcErr = callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing subject_name")
	}
}

func TestToolAnalyzeSubjectPermissionsWithBindings(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-viewer"},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "alice-pod-viewer"},
					RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "pod-viewer", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "alice"},
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "alice-deploy-edit", Namespace: "apps"},
					RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "deploy-editor", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "alice"},
					},
				},
			), nil
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
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "RBAC Analysis for User: alice") {
		t.Fatalf("expected analysis header, got: %s", text)
	}
	if !strings.Contains(text, "pod-viewer") {
		t.Fatalf("expected cluster role name in output, got: %s", text)
	}
	if !strings.Contains(text, "get, list") {
		t.Fatalf("expected verbs in output, got: %s", text)
	}
	if !strings.Contains(text, "Namespace apps") {
		t.Fatalf("expected namespace-scoped binding output, got: %s", text)
	}
}

func TestToolAnalyzeSubjectPermissionsNoBindings(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "User",
		"subject_name": "unknown-user",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success (no bindings is not an error), got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "No RBAC bindings found") {
		t.Fatalf("expected 'No RBAC bindings found', got: %s", text)
	}
}

func TestToolAnalyzeSubjectPermissionsServiceAccountWithNamespace(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-role"},
					Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-crb"},
					RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "sa-role", APIGroup: "rbac.authorization.k8s.io"},
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount", Name: "deployer", Namespace: "ci"},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "analyze_subject_permissions", map[string]interface{}{
		"subject_kind": "ServiceAccount",
		"subject_name": "deployer",
		"namespace":    "ci",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "ServiceAccount: deployer") {
		t.Fatalf("expected SA header, got: %s", text)
	}
	if !strings.Contains(text, "(namespace: ci)") {
		t.Fatalf("expected namespace qualifier, got: %s", text)
	}
	if !strings.Contains(text, "sa-role") {
		t.Fatalf("expected role name, got: %s", text)
	}
}

// --- toolDescribeRole ---

func TestToolDescribeRoleValidation(t *testing.T) {
	server := &testServer{discoverer: stubDiscoverer{}}

	result, rpcErr := callTool(t, server, "describe_role", map[string]interface{}{})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(result.Content[0].Text, "name is required") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolDescribeRoleNamespaceScoped(t *testing.T) {
	now := metav1.NewTime(time.Date(2025, time.June, 1, 12, 0, 0, 0, time.UTC))
	server := &testServer{
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
						{
							APIGroups:     []string{""},
							Resources:     []string{"pods"},
							Verbs:         []string{"get", "list", "watch"},
							ResourceNames: []string{"specific-pod"},
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "describe_role", map[string]interface{}{
		"name":      "pod-reader",
		"namespace": "apps",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Role: apps/pod-reader") {
		t.Fatalf("expected role header, got: %s", text)
	}
	if !strings.Contains(text, "2025-06-01") {
		t.Fatalf("expected creation timestamp, got: %s", text)
	}
	if !strings.Contains(text, "Rule 1:") {
		t.Fatalf("expected rule number, got: %s", text)
	}
	if !strings.Contains(text, "Resources: pods") {
		t.Fatalf("expected resources, got: %s", text)
	}
	if !strings.Contains(text, "Resource Names: specific-pod") {
		t.Fatalf("expected resource names, got: %s", text)
	}
	if !strings.Contains(text, "get, list, watch") {
		t.Fatalf("expected verbs, got: %s", text)
	}
}

func TestToolDescribeClusterRole(t *testing.T) {
	now := metav1.NewTime(time.Date(2025, time.June, 1, 12, 0, 0, 0, time.UTC))
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "node-reader",
						CreationTimestamp: now,
					},
					AggregationRule: &rbacv1.AggregationRule{
						ClusterRoleSelectors: []metav1.LabelSelector{
							{MatchLabels: map[string]string{"rbac": "view"}},
						},
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups:       []string{""},
							Resources:       []string{"nodes"},
							Verbs:           []string{"get", "list"},
							NonResourceURLs: []string{"/healthz"},
						},
					},
				},
			), nil
		},
	}

	result, rpcErr := callTool(t, server, "describe_role", map[string]interface{}{
		"name": "node-reader",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "ClusterRole: node-reader") {
		t.Fatalf("expected cluster role header, got: %s", text)
	}
	if !strings.Contains(text, "Aggregation Rule: yes") {
		t.Fatalf("expected aggregation rule marker, got: %s", text)
	}
	if !strings.Contains(text, "API Groups: core") {
		t.Fatalf("expected 'core' for empty API group, got: %s", text)
	}
	if !strings.Contains(text, "Non-Resource URLs: /healthz") {
		t.Fatalf("expected non-resource URLs, got: %s", text)
	}
}

func TestToolDescribeRoleNotFound(t *testing.T) {
	server := &testServer{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return k8sfake.NewSimpleClientset(), nil
		},
	}

	// Namespaced role not found
	result, rpcErr := callTool(t, server, "describe_role", map[string]interface{}{
		"name":      "nonexistent",
		"namespace": "default",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent role")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to get role") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	// ClusterRole not found
	result, rpcErr = callTool(t, server, "describe_role", map[string]interface{}{
		"name": "nonexistent-cr",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent cluster role")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to get cluster role") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestSubjectFormattingHelpers(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Namespace: "apps", Name: "builder"},
		{Kind: "User", Name: "alice"},
		{Kind: "Group", Name: "admins"},
	}
	formattedSubjects := formatSubjects(subjects)
	for _, want := range []string{"SA:apps/builder", "User:alice", "Group:admins"} {
		if !strings.Contains(formattedSubjects, want) {
			t.Fatalf("formatSubjects() missing %q in %q", want, formattedSubjects)
		}
	}

	if !subjectMatches(subjects, "ServiceAccount", "builder", "apps") {
		t.Fatal("subjectMatches() should match service account with namespace")
	}
	if subjectMatches(subjects, "ServiceAccount", "builder", "other") {
		t.Fatal("subjectMatches() should reject service account in wrong namespace")
	}
	if !subjectMatches(subjects, "User", "alice", "") {
		t.Fatal("subjectMatches() should match user without namespace")
	}
}
