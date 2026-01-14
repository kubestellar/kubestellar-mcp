package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func (s *Server) toolListClusters(args map[string]interface{}) (string, bool) {
	source := "all"
	if v, ok := args["source"].(string); ok {
		source = v
	}

	clusters, err := s.discoverer.DiscoverClusters(source)
	if err != nil {
		return fmt.Sprintf("Failed to discover clusters: %v", err), true
	}

	if len(clusters) == 0 {
		return "No clusters found", false
	}

	var sb strings.Builder
	sb.WriteString("Discovered clusters:\n\n")

	for _, c := range clusters {
		current := ""
		if c.Current {
			current = " (current)"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", c.Name, current))
		sb.WriteString(fmt.Sprintf("  Source: %s\n", c.Source))
		sb.WriteString(fmt.Sprintf("  Server: %s\n", c.Server))
		if c.Status != "" {
			sb.WriteString(fmt.Sprintf("  Status: %s\n", c.Status))
		}
		sb.WriteString("\n")
	}

	return sb.String(), false
}

func (s *Server) toolGetClusterHealth(args map[string]interface{}) (string, bool) {
	clusterName, _ := args["cluster"].(string)

	clusters, err := s.discoverer.DiscoverClusters("all")
	if err != nil {
		return fmt.Sprintf("Failed to discover clusters: %v", err), true
	}

	var targetCluster *struct {
		Name    string
		Context string
		Server  string
		Current bool
	}

	for _, c := range clusters {
		if clusterName == "" && c.Current {
			targetCluster = &struct {
				Name    string
				Context string
				Server  string
				Current bool
			}{c.Name, c.Context, c.Server, c.Current}
			break
		}
		if c.Name == clusterName || c.Context == clusterName {
			targetCluster = &struct {
				Name    string
				Context string
				Server  string
				Current bool
			}{c.Name, c.Context, c.Server, c.Current}
			break
		}
	}

	if targetCluster == nil {
		if clusterName == "" {
			return "No current cluster context set", true
		}
		return fmt.Sprintf("Cluster %q not found", clusterName), true
	}

	// Check health
	health, err := s.discoverer.CheckHealthByContext(targetCluster.Context)
	if err != nil {
		return fmt.Sprintf("Failed to check health: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cluster: %s\n", targetCluster.Name))
	sb.WriteString(fmt.Sprintf("Status: %s\n", health.Status))
	sb.WriteString(fmt.Sprintf("API Server: %s\n", health.APIServerStatus))
	sb.WriteString(fmt.Sprintf("Nodes Ready: %s\n", health.NodesReady))
	if health.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", health.Error))
	}

	return sb.String(), false
}

func (s *Server) getClientForCluster(clusterName string) (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func (s *Server) toolGetPods(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	labelSelector, _ := args["label_selector"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	listOpts := metav1.ListOptions{}
	if labelSelector != "" {
		listOpts.LabelSelector = labelSelector
	}

	var pods *corev1.PodList
	if namespace == "" {
		pods, err = client.CoreV1().Pods("").List(ctx, listOpts)
	} else {
		pods, err = client.CoreV1().Pods(namespace).List(ctx, listOpts)
	}

	if err != nil {
		return fmt.Sprintf("Failed to list pods: %v", err), true
	}

	if len(pods.Items) == 0 {
		return "No pods found", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d pods:\n\n", len(pods.Items)))

	for _, pod := range pods.Items {
		status := string(pod.Status.Phase)
		ready := 0
		total := len(pod.Status.ContainerStatuses)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
		}

		sb.WriteString(fmt.Sprintf("%-50s %-12s %d/%d   %s\n",
			pod.Namespace+"/"+pod.Name,
			status,
			ready, total,
			pod.Status.StartTime.Format("2006-01-02 15:04:05")))
	}

	return sb.String(), false
}

func (s *Server) toolGetDeployments(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var deployments interface{}
	if namespace == "" {
		deployments, err = client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	} else {
		deployments, err = client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list deployments: %v", err), true
	}

	data, _ := json.MarshalIndent(deployments, "", "  ")
	return string(data), false
}

func (s *Server) toolGetServices(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var services *corev1.ServiceList
	if namespace == "" {
		services, err = client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	} else {
		services, err = client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list services: %v", err), true
	}

	if len(services.Items) == 0 {
		return "No services found", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d services:\n\n", len(services.Items)))

	for _, svc := range services.Items {
		sb.WriteString(fmt.Sprintf("%-40s %-15s %-20s %s\n",
			svc.Namespace+"/"+svc.Name,
			string(svc.Spec.Type),
			svc.Spec.ClusterIP,
			formatPorts(svc.Spec.Ports)))
	}

	return sb.String(), false
}

func formatPorts(ports []corev1.ServicePort) string {
	var parts []string
	for _, p := range ports {
		if p.NodePort > 0 {
			parts = append(parts, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
		} else {
			parts = append(parts, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
	}
	return strings.Join(parts, ",")
}

func (s *Server) toolGetNodes(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list nodes: %v", err), true
	}

	if len(nodes.Items) == 0 {
		return "No nodes found", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d nodes:\n\n", len(nodes.Items)))

	for _, node := range nodes.Items {
		status := "NotReady"
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				status = "Ready"
				break
			}
		}

		roles := []string{}
		for label := range node.Labels {
			if strings.HasPrefix(label, "node-role.kubernetes.io/") {
				role := strings.TrimPrefix(label, "node-role.kubernetes.io/")
				if role != "" {
					roles = append(roles, role)
				}
			}
		}
		roleStr := strings.Join(roles, ",")
		if roleStr == "" {
			roleStr = "<none>"
		}

		sb.WriteString(fmt.Sprintf("%-40s %-10s %-20s %s\n",
			node.Name,
			status,
			roleStr,
			node.Status.NodeInfo.KubeletVersion))
	}

	return sb.String(), false
}

func (s *Server) toolGetEvents(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	limit := int64(50)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	listOpts := metav1.ListOptions{
		Limit: limit,
	}

	var events *corev1.EventList
	if namespace == "" {
		events, err = client.CoreV1().Events("").List(ctx, listOpts)
	} else {
		events, err = client.CoreV1().Events(namespace).List(ctx, listOpts)
	}

	if err != nil {
		return fmt.Sprintf("Failed to list events: %v", err), true
	}

	if len(events.Items) == 0 {
		return "No events found", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d events:\n\n", len(events.Items)))

	for _, event := range events.Items {
		sb.WriteString(fmt.Sprintf("[%s] %s/%s: %s\n",
			event.Type,
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Message))
	}

	return sb.String(), false
}

func (s *Server) toolDescribePod(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return "Pod name is required", true
	}

	if namespace == "" {
		namespace = "default"
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get pod: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name: %s\n", pod.Name))
	sb.WriteString(fmt.Sprintf("Namespace: %s\n", pod.Namespace))
	sb.WriteString(fmt.Sprintf("Status: %s\n", pod.Status.Phase))
	sb.WriteString(fmt.Sprintf("Node: %s\n", pod.Spec.NodeName))
	sb.WriteString(fmt.Sprintf("IP: %s\n", pod.Status.PodIP))

	if pod.Status.StartTime != nil {
		sb.WriteString(fmt.Sprintf("Start Time: %s\n", pod.Status.StartTime.Format("2006-01-02 15:04:05")))
	}

	sb.WriteString("\nContainers:\n")
	for _, container := range pod.Spec.Containers {
		sb.WriteString(fmt.Sprintf("  - %s (image: %s)\n", container.Name, container.Image))
	}

	sb.WriteString("\nContainer Statuses:\n")
	for _, cs := range pod.Status.ContainerStatuses {
		ready := "not ready"
		if cs.Ready {
			ready = "ready"
		}
		sb.WriteString(fmt.Sprintf("  - %s: %s, restarts: %d\n", cs.Name, ready, cs.RestartCount))
	}

	sb.WriteString("\nConditions:\n")
	for _, cond := range pod.Status.Conditions {
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", cond.Type, cond.Status))
	}

	return sb.String(), false
}

func (s *Server) toolGetPodLogs(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return "Pod name is required", true
	}
	container, _ := args["container"].(string)
	tailLines := int64(100)
	if v, ok := args["tail_lines"].(float64); ok {
		tailLines = int64(v)
	}

	if namespace == "" {
		namespace = "default"
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}
	if container != "" {
		opts.Container = container
	}

	req := client.CoreV1().Pods(namespace).GetLogs(name, opts)
	logs, err := req.DoRaw(ctx)
	if err != nil {
		return fmt.Sprintf("Failed to get logs: %v", err), true
	}

	return string(logs), false
}

// RBAC Tools

func (s *Server) toolGetRoles(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

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
	sb.WriteString(fmt.Sprintf("Found %d roles:\n\n", len(roles.Items)))

	for _, role := range roles.Items {
		sb.WriteString(fmt.Sprintf("%-40s %-20s %d rules\n",
			role.Namespace+"/"+role.Name,
			role.CreationTimestamp.Format("2006-01-02"),
			len(role.Rules)))
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
		sb.WriteString(fmt.Sprintf("%-50s %d rules%s\n", cr.Name, len(cr.Rules), aggregationRule))
	}

	if count == 0 {
		return "No cluster roles found", false
	}

	header := fmt.Sprintf("Found %d cluster roles:\n\n", count)
	return header + sb.String(), false
}

func (s *Server) toolGetRoleBindings(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

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
	sb.WriteString(fmt.Sprintf("Found %d role bindings:\n\n", len(bindings.Items)))

	for _, rb := range bindings.Items {
		subjects := formatSubjects(rb.Subjects)
		sb.WriteString(fmt.Sprintf("%-40s -> %s/%s\n",
			rb.Namespace+"/"+rb.Name,
			rb.RoleRef.Kind,
			rb.RoleRef.Name))
		sb.WriteString(fmt.Sprintf("  Subjects: %s\n\n", subjects))
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
		sb.WriteString(fmt.Sprintf("%-50s -> %s\n", crb.Name, crb.RoleRef.Name))
		sb.WriteString(fmt.Sprintf("  Subjects: %s\n\n", subjects))
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
	namespace, _ := args["namespace"].(string)
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
	sb.WriteString(fmt.Sprintf("Can I %s %s", verb, resource))
	if subresource != "" {
		sb.WriteString(fmt.Sprintf("/%s", subresource))
	}
	if namespace != "" {
		sb.WriteString(fmt.Sprintf(" in namespace %s", namespace))
	}
	if name != "" {
		sb.WriteString(fmt.Sprintf(" (name: %s)", name))
	}
	sb.WriteString("?\n\n")

	if result.Status.Allowed {
		sb.WriteString("✓ Yes, access is allowed")
	} else {
		sb.WriteString("✗ No, access is denied")
		if result.Status.Reason != "" {
			sb.WriteString(fmt.Sprintf("\nReason: %s", result.Status.Reason))
		}
	}

	return sb.String(), false
}

func (s *Server) toolAnalyzeSubjectPermissions(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	subjectKind, _ := args["subject_kind"].(string)
	subjectName, _ := args["subject_name"].(string)
	subjectNamespace, _ := args["namespace"].(string)

	if subjectKind == "" || subjectName == "" {
		return "subject_kind and subject_name are required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("RBAC Analysis for %s: %s", subjectKind, subjectName))
	if subjectKind == "ServiceAccount" && subjectNamespace != "" {
		sb.WriteString(fmt.Sprintf(" (namespace: %s)", subjectNamespace))
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
				sb.WriteString(fmt.Sprintf("  - %s (error fetching: %v)\n", name, err))
				continue
			}
			sb.WriteString(fmt.Sprintf("  - %s:\n", name))
			for _, rule := range cr.Rules {
				sb.WriteString(fmt.Sprintf("      %s on %s\n",
					strings.Join(rule.Verbs, ", "),
					strings.Join(rule.Resources, ", ")))
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
			sb.WriteString(fmt.Sprintf("  Namespace %s: %s\n", ns, strings.Join(roles, ", ")))
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
	namespace, _ := args["namespace"].(string)

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

		sb.WriteString(fmt.Sprintf("Role: %s/%s\n", role.Namespace, role.Name))
		sb.WriteString(fmt.Sprintf("Created: %s\n\n", role.CreationTimestamp.Format("2006-01-02 15:04:05")))
		sb.WriteString("Rules:\n")
		for i, rule := range role.Rules {
			sb.WriteString(fmt.Sprintf("\n  Rule %d:\n", i+1))
			if len(rule.APIGroups) > 0 {
				sb.WriteString(fmt.Sprintf("    API Groups: %s\n", strings.Join(rule.APIGroups, ", ")))
			}
			if len(rule.Resources) > 0 {
				sb.WriteString(fmt.Sprintf("    Resources: %s\n", strings.Join(rule.Resources, ", ")))
			}
			if len(rule.ResourceNames) > 0 {
				sb.WriteString(fmt.Sprintf("    Resource Names: %s\n", strings.Join(rule.ResourceNames, ", ")))
			}
			sb.WriteString(fmt.Sprintf("    Verbs: %s\n", strings.Join(rule.Verbs, ", ")))
		}
	} else {
		// Get ClusterRole
		cr, err := client.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to get cluster role: %v", err), true
		}

		sb.WriteString(fmt.Sprintf("ClusterRole: %s\n", cr.Name))
		sb.WriteString(fmt.Sprintf("Created: %s\n", cr.CreationTimestamp.Format("2006-01-02 15:04:05")))
		if cr.AggregationRule != nil && len(cr.AggregationRule.ClusterRoleSelectors) > 0 {
			sb.WriteString("Aggregation Rule: yes\n")
		}
		sb.WriteString("\nRules:\n")
		for i, rule := range cr.Rules {
			sb.WriteString(fmt.Sprintf("\n  Rule %d:\n", i+1))
			if len(rule.APIGroups) > 0 {
				apiGroups := make([]string, len(rule.APIGroups))
				for j, g := range rule.APIGroups {
					if g == "" {
						apiGroups[j] = "core"
					} else {
						apiGroups[j] = g
					}
				}
				sb.WriteString(fmt.Sprintf("    API Groups: %s\n", strings.Join(apiGroups, ", ")))
			}
			if len(rule.Resources) > 0 {
				sb.WriteString(fmt.Sprintf("    Resources: %s\n", strings.Join(rule.Resources, ", ")))
			}
			if len(rule.ResourceNames) > 0 {
				sb.WriteString(fmt.Sprintf("    Resource Names: %s\n", strings.Join(rule.ResourceNames, ", ")))
			}
			if len(rule.NonResourceURLs) > 0 {
				sb.WriteString(fmt.Sprintf("    Non-Resource URLs: %s\n", strings.Join(rule.NonResourceURLs, ", ")))
			}
			sb.WriteString(fmt.Sprintf("    Verbs: %s\n", strings.Join(rule.Verbs, ", ")))
		}
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
		Context     string
		Cluster     string
		Server      string
		User        string
		Accessible  bool
		Error       string
		IsCurrent   bool
		ServerInfo  string
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

	sb.WriteString(fmt.Sprintf("**Total contexts:** %d\n", len(results)))
	sb.WriteString(fmt.Sprintf("**Accessible:** %d\n", accessible))
	sb.WriteString(fmt.Sprintf("**Inaccessible:** %d\n\n", inaccessible))

	// Accessible clusters
	if accessible > 0 {
		sb.WriteString("## Accessible Clusters\n\n")
		for _, r := range results {
			if r.Accessible {
				current := ""
				if r.IsCurrent {
					current = " **(current)**"
				}
				sb.WriteString(fmt.Sprintf("- **%s**%s\n", r.Context, current))
				sb.WriteString(fmt.Sprintf("  - Server: %s\n", r.Server))
				sb.WriteString(fmt.Sprintf("  - Version: %s\n", r.ServerInfo))
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
				sb.WriteString(fmt.Sprintf("- **%s**%s\n", r.Context, current))
				sb.WriteString(fmt.Sprintf("  - Server: %s\n", r.Server))
				sb.WriteString(fmt.Sprintf("  - Error: %s\n", r.Error))
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
				sb.WriteString(fmt.Sprintf("**Server:** `%s`\n", server))
				sb.WriteString("- Contexts: ")
				for i, ctx := range contexts {
					if i > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(fmt.Sprintf("`%s`", ctx))
				}
				sb.WriteString("\n")
				sb.WriteString(fmt.Sprintf("- Consider keeping one and removing %d duplicate(s)\n\n", len(contexts)-1))
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
				sb.WriteString(fmt.Sprintf("kubectl config delete-context %s\n", r.Context))
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
				sb.WriteString(fmt.Sprintf("kubectl config delete-cluster %s\n", cluster))
			}
			for user := range usersToDelete {
				sb.WriteString(fmt.Sprintf("kubectl config delete-user %s\n", user))
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
