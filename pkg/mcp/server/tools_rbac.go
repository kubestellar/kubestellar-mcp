package server

import (
	"context"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) toolGetRoles(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var roles *rbacv1.RoleList
	if namespace == "" {
		roles, err = client.RbacV1().Roles("").List(ctx, metav1.ListOptions{})
	} else {
		roles, err = client.RbacV1().Roles(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list roles: %v", err), true
	}

	if len(roles.Items) == 0 {
		return "No roles found", false
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Found %d roles:\n\n", len(roles.Items))

	for _, role := range roles.Items {
		_, _ = fmt.Fprintf(&sb, "%-40s %-20s %d rules\n",
			role.Namespace+"/"+role.Name,
			role.CreationTimestamp.Format("2006-01-02"),
			len(role.Rules))
	}

	return sb.String(), false
}

func (s *Server) toolGetClusterRoles(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	includeSystem := args["include_system"] == "true"

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	clusterRoles, err := client.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list cluster roles: %v", err), true
	}

	var sb strings.Builder
	count := 0

	for _, cr := range clusterRoles.Items {
		// Skip system roles unless requested
		if !includeSystem && (strings.HasPrefix(cr.Name, "system:") || strings.HasPrefix(cr.Name, "kubeadm:")) {
			continue
		}
		count++
		aggregationRule := ""
		if cr.AggregationRule != nil {
			aggregationRule = " (aggregated)"
		}
		_, _ = fmt.Fprintf(&sb, "%-50s %d rules%s\n", cr.Name, len(cr.Rules), aggregationRule)
	}

	if count == 0 {
		return "No cluster roles found", false
	}

	header := fmt.Sprintf("Found %d cluster roles:\n\n", count)
	return header + sb.String(), false
}

func (s *Server) toolGetRoleBindings(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var bindings *rbacv1.RoleBindingList
	if namespace == "" {
		bindings, err = client.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	} else {
		bindings, err = client.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list role bindings: %v", err), true
	}

	if len(bindings.Items) == 0 {
		return "No role bindings found", false
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Found %d role bindings:\n\n", len(bindings.Items))

	for _, rb := range bindings.Items {
		subjects := formatSubjects(rb.Subjects)
		_, _ = fmt.Fprintf(&sb, "%-40s -> %s/%s\n",
			rb.Namespace+"/"+rb.Name,
			rb.RoleRef.Kind,
			rb.RoleRef.Name)
		_, _ = fmt.Fprintf(&sb, "  Subjects: %s\n\n", subjects)
	}

	return sb.String(), false
}

func (s *Server) toolGetClusterRoleBindings(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	includeSystem := args["include_system"] == "true"

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	bindings, err := client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list cluster role bindings: %v", err), true
	}

	var sb strings.Builder
	count := 0

	for _, crb := range bindings.Items {
		// Skip system bindings unless requested
		if !includeSystem && (strings.HasPrefix(crb.Name, "system:") || strings.HasPrefix(crb.Name, "kubeadm:")) {
			continue
		}
		count++
		subjects := formatSubjects(crb.Subjects)
		_, _ = fmt.Fprintf(&sb, "%-50s -> %s\n", crb.Name, crb.RoleRef.Name)
		_, _ = fmt.Fprintf(&sb, "  Subjects: %s\n\n", subjects)
	}

	if count == 0 {
		return "No cluster role bindings found", false
	}

	header := fmt.Sprintf("Found %d cluster role bindings:\n\n", count)
	return header + sb.String(), false
}

func formatSubjects(subjects []rbacv1.Subject) string {
	var parts []string
	for _, s := range subjects {
		switch s.Kind {
		case "ServiceAccount":
			parts = append(parts, fmt.Sprintf("SA:%s/%s", s.Namespace, s.Name))
		case "User":
			parts = append(parts, fmt.Sprintf("User:%s", s.Name))
		case "Group":
			parts = append(parts, fmt.Sprintf("Group:%s", s.Name))
		}
	}
	if len(parts) == 0 {
		return "<none>"
	}
	return strings.Join(parts, ", ")
}

func (s *Server) toolCanI(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	verb, _ := args["verb"].(string)
	resource, _ := args["resource"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	subresource, _ := args["subresource"].(string)
	name, _ := args["name"].(string)

	if verb == "" || resource == "" {
		return "verb and resource are required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	sar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace:   namespace,
				Verb:        verb,
				Resource:    resource,
				Subresource: subresource,
				Name:        name,
			},
		},
	}

	result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to check access: %v", err), true
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Can I %s %s", verb, resource)
	if subresource != "" {
		_, _ = fmt.Fprintf(&sb, "/%s", subresource)
	}
	if namespace != "" {
		_, _ = fmt.Fprintf(&sb, " in namespace %s", namespace)
	}
	if name != "" {
		_, _ = fmt.Fprintf(&sb, " (name: %s)", name)
	}
	sb.WriteString("?\n\n")

	if result.Status.Allowed {
		sb.WriteString("✓ Yes, access is allowed")
	} else {
		sb.WriteString("✗ No, access is denied")
		if result.Status.Reason != "" {
			_, _ = fmt.Fprintf(&sb, "\nReason: %s", result.Status.Reason)
		}
	}

	return sb.String(), false
}

func (s *Server) toolAnalyzeSubjectPermissions(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	subjectKind, _ := args["subject_kind"].(string)
	subjectName, _ := args["subject_name"].(string)
	subjectNamespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	if subjectKind == "" || subjectName == "" {
		return "subject_kind and subject_name are required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "RBAC Analysis for %s: %s", subjectKind, subjectName)
	if subjectKind == "ServiceAccount" && subjectNamespace != "" {
		_, _ = fmt.Fprintf(&sb, " (namespace: %s)", subjectNamespace)
	}
	sb.WriteString("\n\n")

	// Check ClusterRoleBindings
	crbs, err := client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list cluster role bindings: %v", err), true
	}

	clusterRoleNames := []string{}
	for _, crb := range crbs.Items {
		if subjectMatches(crb.Subjects, subjectKind, subjectName, subjectNamespace) {
			clusterRoleNames = append(clusterRoleNames, crb.RoleRef.Name)
		}
	}

	if len(clusterRoleNames) > 0 {
		sb.WriteString("Cluster-wide permissions via ClusterRoleBindings:\n")
		for _, name := range clusterRoleNames {
			cr, err := client.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				_, _ = fmt.Fprintf(&sb, "  - %s (error fetching: %v)\n", name, err)
				continue
			}
			_, _ = fmt.Fprintf(&sb, "  - %s:\n", name)
			for _, rule := range cr.Rules {
				_, _ = fmt.Fprintf(&sb, "      %s on %s\n",
					strings.Join(rule.Verbs, ", "),
					strings.Join(rule.Resources, ", "))
			}
		}
		sb.WriteString("\n")
	}

	// Check RoleBindings in all namespaces
	rbs, err := client.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list role bindings: %v", err), true
	}

	nsRoles := make(map[string][]string)
	for _, rb := range rbs.Items {
		if subjectMatches(rb.Subjects, subjectKind, subjectName, subjectNamespace) {
			nsRoles[rb.Namespace] = append(nsRoles[rb.Namespace], rb.RoleRef.Name+" ("+rb.RoleRef.Kind+")")
		}
	}

	if len(nsRoles) > 0 {
		sb.WriteString("Namespace-scoped permissions via RoleBindings:\n")
		for ns, roles := range nsRoles {
			_, _ = fmt.Fprintf(&sb, "  Namespace %s: %s\n", ns, strings.Join(roles, ", "))
		}
	}

	if len(clusterRoleNames) == 0 && len(nsRoles) == 0 {
		sb.WriteString("No RBAC bindings found for this subject.")
	}

	return sb.String(), false
}

func subjectMatches(subjects []rbacv1.Subject, kind, name, namespace string) bool {
	for _, s := range subjects {
		if s.Kind == kind && s.Name == name {
			if kind == "ServiceAccount" {
				if namespace != "" && s.Namespace != namespace {
					continue
				}
			}
			return true
		}
	}
	return false
}

func (s *Server) toolDescribeRole(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	name, _ := args["name"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	if name == "" {
		return "name is required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder

	if namespace != "" {
		// Get Role
		role, err := client.RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to get role: %v", err), true
		}

		_, _ = fmt.Fprintf(&sb, "Role: %s/%s\n", role.Namespace, role.Name)
		_, _ = fmt.Fprintf(&sb, "Created: %s\n\n", role.CreationTimestamp.Format("2006-01-02 15:04:05"))
		sb.WriteString("Rules:\n")
		for i, rule := range role.Rules {
			_, _ = fmt.Fprintf(&sb, "\n  Rule %d:\n", i+1)
			if len(rule.APIGroups) > 0 {
				_, _ = fmt.Fprintf(&sb, "    API Groups: %s\n", strings.Join(rule.APIGroups, ", "))
			}
			if len(rule.Resources) > 0 {
				_, _ = fmt.Fprintf(&sb, "    Resources: %s\n", strings.Join(rule.Resources, ", "))
			}
			if len(rule.ResourceNames) > 0 {
				_, _ = fmt.Fprintf(&sb, "    Resource Names: %s\n", strings.Join(rule.ResourceNames, ", "))
			}
			_, _ = fmt.Fprintf(&sb, "    Verbs: %s\n", strings.Join(rule.Verbs, ", "))
		}
	} else {
		// Get ClusterRole
		cr, err := client.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to get cluster role: %v", err), true
		}

		_, _ = fmt.Fprintf(&sb, "ClusterRole: %s\n", cr.Name)
		_, _ = fmt.Fprintf(&sb, "Created: %s\n", cr.CreationTimestamp.Format("2006-01-02 15:04:05"))
		if cr.AggregationRule != nil && len(cr.AggregationRule.ClusterRoleSelectors) > 0 {
			sb.WriteString("Aggregation Rule: yes\n")
		}
		sb.WriteString("\nRules:\n")
		for i, rule := range cr.Rules {
			_, _ = fmt.Fprintf(&sb, "\n  Rule %d:\n", i+1)
			if len(rule.APIGroups) > 0 {
				apiGroups := make([]string, len(rule.APIGroups))
				for j, g := range rule.APIGroups {
					if g == "" {
						apiGroups[j] = "core"
					} else {
						apiGroups[j] = g
					}
				}
				_, _ = fmt.Fprintf(&sb, "    API Groups: %s\n", strings.Join(apiGroups, ", "))
			}
			if len(rule.Resources) > 0 {
				_, _ = fmt.Fprintf(&sb, "    Resources: %s\n", strings.Join(rule.Resources, ", "))
			}
			if len(rule.ResourceNames) > 0 {
				_, _ = fmt.Fprintf(&sb, "    Resource Names: %s\n", strings.Join(rule.ResourceNames, ", "))
			}
			if len(rule.NonResourceURLs) > 0 {
				_, _ = fmt.Fprintf(&sb, "    Non-Resource URLs: %s\n", strings.Join(rule.NonResourceURLs, ", "))
			}
			_, _ = fmt.Fprintf(&sb, "    Verbs: %s\n", strings.Join(rule.Verbs, ", "))
		}
	}

	return sb.String(), false
}
