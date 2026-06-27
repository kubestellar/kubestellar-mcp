package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

func (s *Server) toolFindResourceOwners(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	resourceType, _ := args["resource_type"].(string)

	if namespace == "" {
		return "namespace is required", true
	}

	if resourceType == "" {
		resourceType = "all"
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	type resourceOwner struct {
		Kind       string
		Name       string
		Namespace  string
		Manager    string
		CreatedBy  string
		ManagedBy  string
		Owner      string
		Team       string
		LastUpdate string
	}

	owners := []resourceOwner{}

	// Common ownership labels/annotations
	ownershipLabels := []string{
		"app.kubernetes.io/managed-by",
		"owner",
		"team",
		"created-by",
		"managed-by",
	}
	ownershipAnnotations := []string{
		"kubectl.kubernetes.io/last-applied-configuration",
		"meta.helm.sh/release-name",
		"deployment.kubernetes.io/revision",
	}
	_ = ownershipAnnotations // used for context in output

	// Helper to extract owner info from resource metadata
	extractOwnerInfo := func(kind, name, ns string, labels, annotations map[string]string, managedFields []metav1.ManagedFieldsEntry) resourceOwner {
		ro := resourceOwner{
			Kind:      kind,
			Name:      name,
			Namespace: ns,
		}

		// Check managed fields for last manager
		if len(managedFields) > 0 {
			lastField := managedFields[len(managedFields)-1]
			ro.Manager = lastField.Manager
			if lastField.Time != nil {
				ro.LastUpdate = lastField.Time.Format("2006-01-02 15:04:05")
			}
		}

		// Check labels
		for _, label := range ownershipLabels {
			if val, ok := labels[label]; ok {
				switch label {
				case "app.kubernetes.io/managed-by", "managed-by":
					ro.ManagedBy = val
				case "owner", "created-by":
					ro.CreatedBy = val
				case "team":
					ro.Team = val
				}
			}
		}

		// Check annotations
		if val, ok := annotations["meta.helm.sh/release-name"]; ok {
			ro.ManagedBy = "helm:" + val
		}

		return ro
	}

	// Get Pods
	if resourceType == "all" || resourceType == "pods" {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list pods: %v", err), true
		}
		for _, pod := range pods.Items {
			ro := extractOwnerInfo("Pod", pod.Name, pod.Namespace, pod.Labels, pod.Annotations, pod.ManagedFields)
			// Check owner references
			for _, ownerRef := range pod.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	// Get Deployments
	if resourceType == "all" || resourceType == "deployments" {
		deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list deployments: %v", err), true
		}
		for _, dep := range deployments.Items {
			ro := extractOwnerInfo("Deployment", dep.Name, dep.Namespace, dep.Labels, dep.Annotations, dep.ManagedFields)
			for _, ownerRef := range dep.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	// Get Services
	if resourceType == "all" || resourceType == "services" {
		services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list services: %v", err), true
		}
		for _, svc := range services.Items {
			ro := extractOwnerInfo("Service", svc.Name, svc.Namespace, svc.Labels, svc.Annotations, svc.ManagedFields)
			for _, ownerRef := range svc.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	if len(owners) == 0 {
		return "No resources found in namespace " + namespace, false
	}

	// Build output
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "# Resource Ownership in namespace: %s\n\n", namespace)
	_, _ = fmt.Fprintf(&sb, "Found %d resources\n\n", len(owners))

	// Group by manager
	managerGroups := make(map[string][]resourceOwner)
	for _, ro := range owners {
		manager := ro.Manager
		if manager == "" {
			manager = "(unknown)"
		}
		managerGroups[manager] = append(managerGroups[manager], ro)
	}

	sb.WriteString("## By Manager/Controller\n\n")
	for manager, resources := range managerGroups {
		_, _ = fmt.Fprintf(&sb, "### %s\n", manager)
		for _, ro := range resources {
			_, _ = fmt.Fprintf(&sb, "- **%s/%s**", ro.Kind, ro.Name)
			if ro.Owner != "" {
				_, _ = fmt.Fprintf(&sb, " (owner: %s)", ro.Owner)
			}
			if ro.ManagedBy != "" {
				_, _ = fmt.Fprintf(&sb, " [managed-by: %s]", ro.ManagedBy)
			}
			if ro.Team != "" {
				_, _ = fmt.Fprintf(&sb, " [team: %s]", ro.Team)
			}
			if ro.CreatedBy != "" {
				_, _ = fmt.Fprintf(&sb, " [created-by: %s]", ro.CreatedBy)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Summary table
	sb.WriteString("## Ownership Labels Summary\n\n")
	sb.WriteString("| Kind | Name | Manager | Owner | Managed-By | Team | Last Update |\n")
	sb.WriteString("|------|------|---------|-------|------------|------|-------------|\n")
	for _, ro := range owners {
		manager := ro.Manager
		if manager == "" {
			manager = "-"
		}
		owner := ro.Owner
		if owner == "" {
			owner = "-"
		}
		managedBy := ro.ManagedBy
		if managedBy == "" {
			managedBy = "-"
		}
		team := ro.Team
		if team == "" {
			team = "-"
		}
		lastUpdate := ro.LastUpdate
		if lastUpdate == "" {
			lastUpdate = "-"
		}
		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s |\n",
			ro.Kind, ro.Name, manager, owner, managedBy, team, lastUpdate)
	}

	return sb.String(), false
}

func (s *Server) toolAuditKubeconfig(ctx context.Context, args map[string]interface{}) (string, bool) {
	timeoutSeconds := 5
	if v, ok := args["timeout_seconds"].(float64); ok {
		timeoutSeconds = int(v)
	}

	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	config, err := loadingRules.Load()
	if err != nil {
		return fmt.Sprintf("Failed to load kubeconfig: %v", err), true
	}

	if len(config.Contexts) == 0 {
		return "No contexts found in kubeconfig", false
	}

	type clusterResult struct {
		Context    string
		Cluster    string
		Server     string
		User       string
		Accessible bool
		Error      string
		IsCurrent  bool
		ServerInfo string
	}

	results := make([]clusterResult, 0, len(config.Contexts))

	for contextName, contextInfo := range config.Contexts {
		result := clusterResult{
			Context:   contextName,
			Cluster:   contextInfo.Cluster,
			User:      contextInfo.AuthInfo,
			IsCurrent: contextName == config.CurrentContext,
		}

		// Get cluster info
		if clusterInfo, ok := config.Clusters[contextInfo.Cluster]; ok {
			result.Server = clusterInfo.Server
		}

		// Try to connect with timeout
		clientConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{
			CurrentContext: contextName,
		})

		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			result.Accessible = false
			result.Error = fmt.Sprintf("Config error: %v", err)
			results = append(results, result)
			continue
		}

		// Set timeout
		restConfig.Timeout = time.Duration(timeoutSeconds) * time.Second

		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			result.Accessible = false
			result.Error = fmt.Sprintf("Client error: %v", err)
			results = append(results, result)
			continue
		}

		// Try to get server version (lightweight API call)
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		version, err := clientset.Discovery().ServerVersion()
		cancel()
		_ = timeoutCtx // avoid unused variable

		if err != nil {
			result.Accessible = false
			// Simplify common error messages
			errStr := err.Error()
			if strings.Contains(errStr, "certificate") {
				result.Error = "Certificate error (expired or invalid)"
			} else if strings.Contains(errStr, "connection refused") {
				result.Error = "Connection refused (cluster may be down)"
			} else if strings.Contains(errStr, "no such host") {
				result.Error = "DNS resolution failed (host not found)"
			} else if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
				result.Error = "Connection timeout"
			} else if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "Unauthorized") {
				result.Error = "Unauthorized (credentials may be expired)"
			} else if strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "Forbidden") {
				result.Error = "Forbidden (insufficient permissions)"
			} else {
				result.Error = errStr
			}
		} else {
			result.Accessible = true
			result.ServerInfo = fmt.Sprintf("v%s", version.GitVersion)
		}

		results = append(results, result)
	}

	// Build report
	var sb strings.Builder
	sb.WriteString("# Kubeconfig Cluster Audit\n\n")

	// Summary
	accessible := 0
	inaccessible := 0
	for _, r := range results {
		if r.Accessible {
			accessible++
		} else {
			inaccessible++
		}
	}

	_, _ = fmt.Fprintf(&sb, "**Total contexts:** %d\n", len(results))
	_, _ = fmt.Fprintf(&sb, "**Accessible:** %d\n", accessible)
	_, _ = fmt.Fprintf(&sb, "**Inaccessible:** %d\n\n", inaccessible)

	// Accessible clusters
	if accessible > 0 {
		sb.WriteString("## Accessible Clusters\n\n")
		for _, r := range results {
			if r.Accessible {
				current := ""
				if r.IsCurrent {
					current = " **(current)**"
				}
				_, _ = fmt.Fprintf(&sb, "- **%s**%s\n", r.Context, current)
				_, _ = fmt.Fprintf(&sb, "  - Server: %s\n", r.Server)
				_, _ = fmt.Fprintf(&sb, "  - Version: %s\n", r.ServerInfo)
			}
		}
		sb.WriteString("\n")
	}

	// Inaccessible clusters
	if inaccessible > 0 {
		sb.WriteString("## Inaccessible Clusters\n\n")
		for _, r := range results {
			if !r.Accessible {
				current := ""
				if r.IsCurrent {
					current = " **(current)**"
				}
				_, _ = fmt.Fprintf(&sb, "- **%s**%s\n", r.Context, current)
				_, _ = fmt.Fprintf(&sb, "  - Server: %s\n", r.Server)
				_, _ = fmt.Fprintf(&sb, "  - Error: %s\n", r.Error)
			}
		}
		sb.WriteString("\n")
	}

	// Find duplicate contexts (same server URL)
	serverToContexts := make(map[string][]string)
	for _, r := range results {
		if r.Server != "" {
			serverToContexts[r.Server] = append(serverToContexts[r.Server], r.Context)
		}
	}

	// Check for consolidation opportunities
	hasDuplicates := false
	for _, contexts := range serverToContexts {
		if len(contexts) > 1 {
			hasDuplicates = true
			break
		}
	}

	if hasDuplicates {
		sb.WriteString("## Consolidation Suggestions\n\n")
		sb.WriteString("The following contexts point to the same cluster and could be consolidated:\n\n")
		for server, contexts := range serverToContexts {
			if len(contexts) > 1 {
				_, _ = fmt.Fprintf(&sb, "**Server:** `%s`\n", server)
				sb.WriteString("- Contexts: ")
				for i, ctx := range contexts {
					if i > 0 {
						sb.WriteString(", ")
					}
					_, _ = fmt.Fprintf(&sb, "`%s`", ctx)
				}
				sb.WriteString("\n")
				_, _ = fmt.Fprintf(&sb, "- Consider keeping one and removing %d duplicate(s)\n\n", len(contexts)-1)
			}
		}
	}

	// Cleanup recommendations for inaccessible clusters
	if inaccessible > 0 {
		sb.WriteString("## Delete Inaccessible Contexts\n\n")
		sb.WriteString("These contexts are unreachable and should be removed:\n\n")
		sb.WriteString("```bash\n")
		for _, r := range results {
			if !r.Accessible {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-context %s\n", r.Context)
			}
		}
		sb.WriteString("```\n\n")

		// Collect unique clusters and users from inaccessible contexts
		clustersToDelete := make(map[string]bool)
		usersToDelete := make(map[string]bool)
		for _, r := range results {
			if !r.Accessible {
				clustersToDelete[r.Cluster] = true
				usersToDelete[r.User] = true
			}
		}

		// Check if clusters/users are used by accessible contexts
		for _, r := range results {
			if r.Accessible {
				delete(clustersToDelete, r.Cluster)
				delete(usersToDelete, r.User)
			}
		}

		if len(clustersToDelete) > 0 || len(usersToDelete) > 0 {
			sb.WriteString("Also remove orphaned clusters and users:\n")
			sb.WriteString("```bash\n")
			for cluster := range clustersToDelete {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-cluster %s\n", cluster)
			}
			for user := range usersToDelete {
				_, _ = fmt.Fprintf(&sb, "kubectl config delete-user %s\n", user)
			}
			sb.WriteString("```\n")
		}
	}

	// Summary
	if inaccessible == 0 && !hasDuplicates {
		sb.WriteString("## All Good!\n\n")
		sb.WriteString("All clusters are accessible and no duplicates found.\n")
	}

	return sb.String(), false
}

// OPA Gatekeeper Tools

const (
	gatekeeperNamespace          = "gatekeeper-system"
	ownershipTemplateName        = "k8srequiredlabels"
	ownershipConstraintName      = "require-ownership-labels"
	constraintTemplateAPIVersion = "templates.gatekeeper.sh/v1"
	constraintAPIVersion         = "constraints.gatekeeper.sh/v1beta1"
)

