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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubestellar/klaude/pkg/gitops"
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

func (s *Server) toolFindResourceOwners(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
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
	sb.WriteString(fmt.Sprintf("# Resource Ownership in namespace: %s\n\n", namespace))
	sb.WriteString(fmt.Sprintf("Found %d resources\n\n", len(owners)))

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
		sb.WriteString(fmt.Sprintf("### %s\n", manager))
		for _, ro := range resources {
			sb.WriteString(fmt.Sprintf("- **%s/%s**", ro.Kind, ro.Name))
			if ro.Owner != "" {
				sb.WriteString(fmt.Sprintf(" (owner: %s)", ro.Owner))
			}
			if ro.ManagedBy != "" {
				sb.WriteString(fmt.Sprintf(" [managed-by: %s]", ro.ManagedBy))
			}
			if ro.Team != "" {
				sb.WriteString(fmt.Sprintf(" [team: %s]", ro.Team))
			}
			if ro.CreatedBy != "" {
				sb.WriteString(fmt.Sprintf(" [created-by: %s]", ro.CreatedBy))
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
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			ro.Kind, ro.Name, manager, owner, managedBy, team, lastUpdate))
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

// OPA Gatekeeper Tools

const (
	gatekeeperNamespace          = "gatekeeper-system"
	ownershipTemplateName        = "k8srequiredlabels"
	ownershipConstraintName      = "require-ownership-labels"
	constraintTemplateAPIVersion = "templates.gatekeeper.sh/v1"
	constraintAPIVersion         = "constraints.gatekeeper.sh/v1beta1"
)

func (s *Server) getDynamicClientForCluster(clusterName string) (dynamic.Interface, error) {
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

	return dynamic.NewForConfig(config)
}

func (s *Server) toolCheckGatekeeper(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# OPA Gatekeeper Status\n\n")

	// Check if gatekeeper-system namespace exists
	_, err = client.CoreV1().Namespaces().Get(ctx, gatekeeperNamespace, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Status:** Not Installed\n\n")
		sb.WriteString("Gatekeeper namespace `gatekeeper-system` not found.\n\n")
		sb.WriteString("## Installation\n\n")
		sb.WriteString("To install Gatekeeper:\n")
		sb.WriteString("```bash\n")
		sb.WriteString("kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml\n")
		sb.WriteString("```\n\n")
		sb.WriteString("Or on OpenShift, install the Gatekeeper Operator from OperatorHub.\n")
		return sb.String(), false
	}

	// Check pods in gatekeeper-system
	pods, err := client.CoreV1().Pods(gatekeeperNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list Gatekeeper pods: %v", err), true
	}

	runningPods := 0
	totalPods := len(pods.Items)
	var podStatuses []string

	for _, pod := range pods.Items {
		status := string(pod.Status.Phase)
		ready := 0
		total := len(pod.Status.ContainerStatuses)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
		}
		if pod.Status.Phase == corev1.PodRunning && ready == total {
			runningPods++
		}
		podStatuses = append(podStatuses, fmt.Sprintf("- %s: %s (%d/%d ready)", pod.Name, status, ready, total))
	}

	if runningPods == totalPods && totalPods > 0 {
		sb.WriteString("**Status:** Installed and Healthy ✓\n\n")
	} else if totalPods > 0 {
		sb.WriteString("**Status:** Installed but Degraded ⚠\n\n")
	} else {
		sb.WriteString("**Status:** Namespace exists but no pods found\n\n")
	}

	sb.WriteString(fmt.Sprintf("**Pods:** %d/%d running\n\n", runningPods, totalPods))
	for _, status := range podStatuses {
		sb.WriteString(status + "\n")
	}

	// Check for ConstraintTemplates
	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		sb.WriteString("\nFailed to check ConstraintTemplates\n")
		return sb.String(), false
	}

	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	templates, err := dynClient.Resource(ctGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		sb.WriteString("\n**ConstraintTemplates:** Unable to list (may need permissions)\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n**ConstraintTemplates:** %d installed\n", len(templates.Items)))
		if len(templates.Items) > 0 {
			for _, t := range templates.Items {
				sb.WriteString(fmt.Sprintf("- %s\n", t.GetName()))
			}
		}
	}

	// Check if ownership policy is installed
	_, err = dynClient.Resource(ctGVR).Get(ctx, ownershipTemplateName, metav1.GetOptions{})
	if err == nil {
		sb.WriteString(fmt.Sprintf("\n**Ownership Policy:** Installed (template: %s)\n", ownershipTemplateName))
	} else {
		sb.WriteString("\n**Ownership Policy:** Not installed\n")
		sb.WriteString("Use `install_ownership_policy` to set up ownership label enforcement.\n")
	}

	return sb.String(), false
}

func (s *Server) toolGetOwnershipPolicyStatus(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Policy Status\n\n")

	// Check ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	template, err := dynClient.Resource(ctGVR).Get(ctx, ownershipTemplateName, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Template:** Not installed\n")
		sb.WriteString("\nThe ownership labels policy is not installed.\n")
		sb.WriteString("Use `install_ownership_policy` to set it up.\n")
		return sb.String(), false
	}

	// Get template status
	templateStatus, _, _ := unstructured.NestedMap(template.Object, "status")
	created, _, _ := unstructured.NestedBool(templateStatus, "created")
	sb.WriteString(fmt.Sprintf("**Template:** %s (created: %v)\n", ownershipTemplateName, created))

	// Check Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Constraint:** Not created\n")
		sb.WriteString("\nTemplate exists but no constraint is active.\n")
		return sb.String(), false
	}

	// Get constraint spec
	spec, _, _ := unstructured.NestedMap(constraint.Object, "spec")
	enforcementAction, _, _ := unstructured.NestedString(spec, "enforcementAction")
	if enforcementAction == "" {
		enforcementAction = "deny"
	}

	sb.WriteString(fmt.Sprintf("**Constraint:** %s\n", ownershipConstraintName))
	sb.WriteString(fmt.Sprintf("**Mode:** %s\n", enforcementAction))

	// Get required labels
	params, _, _ := unstructured.NestedMap(spec, "parameters")
	labels, _, _ := unstructured.NestedStringSlice(params, "labels")
	if len(labels) > 0 {
		sb.WriteString(fmt.Sprintf("**Required Labels:** %s\n", strings.Join(labels, ", ")))
	}

	// Get match configuration
	match, _, _ := unstructured.NestedMap(spec, "match")
	excludedNS, _, _ := unstructured.NestedStringSlice(match, "excludedNamespaces")
	if len(excludedNS) > 0 {
		sb.WriteString(fmt.Sprintf("**Excluded Namespaces:** %s\n", strings.Join(excludedNS, ", ")))
	}

	// Get violation count from status
	status, _, _ := unstructured.NestedMap(constraint.Object, "status")
	totalViolations, found, _ := unstructured.NestedInt64(status, "totalViolations")
	if found {
		sb.WriteString(fmt.Sprintf("\n**Total Violations:** %d\n", totalViolations))
	}

	return sb.String(), false
}

func (s *Server) toolListOwnershipViolations(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespaceFilter, _ := args["namespace"].(string)
	limit := int64(50)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	// Get Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		return "Ownership policy not installed. Use `install_ownership_policy` to set it up.", false
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Label Violations\n\n")

	// Get enforcement mode
	spec, _, _ := unstructured.NestedMap(constraint.Object, "spec")
	enforcementAction, _, _ := unstructured.NestedString(spec, "enforcementAction")
	if enforcementAction == "" {
		enforcementAction = "deny"
	}
	sb.WriteString(fmt.Sprintf("**Mode:** %s\n", enforcementAction))

	// Get violations from status
	status, _, _ := unstructured.NestedMap(constraint.Object, "status")
	violations, _, _ := unstructured.NestedSlice(status, "violations")

	if len(violations) == 0 {
		sb.WriteString("\n**No violations found!** All resources have required ownership labels.\n")
		return sb.String(), false
	}

	totalViolations, _, _ := unstructured.NestedInt64(status, "totalViolations")
	sb.WriteString(fmt.Sprintf("**Total Violations:** %d\n\n", totalViolations))

	// Group by namespace
	type violation struct {
		Kind      string
		Name      string
		Namespace string
		Message   string
	}
	var violationList []violation
	namespaceCount := make(map[string]int)

	for _, v := range violations {
		vMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		ns, _, _ := unstructured.NestedString(vMap, "namespace")
		if namespaceFilter != "" && ns != namespaceFilter {
			continue
		}

		kind, _, _ := unstructured.NestedString(vMap, "kind")
		name, _, _ := unstructured.NestedString(vMap, "name")
		message, _, _ := unstructured.NestedString(vMap, "message")

		violationList = append(violationList, violation{
			Kind:      kind,
			Name:      name,
			Namespace: ns,
			Message:   message,
		})
		namespaceCount[ns]++
	}

	if len(violationList) == 0 {
		sb.WriteString(fmt.Sprintf("\nNo violations in namespace `%s`.\n", namespaceFilter))
		return sb.String(), false
	}

	// Show summary by namespace
	sb.WriteString("## By Namespace\n\n")
	for ns, count := range namespaceCount {
		sb.WriteString(fmt.Sprintf("- **%s**: %d violations\n", ns, count))
	}

	// Show details
	sb.WriteString("\n## Violations\n\n")
	sb.WriteString("| Namespace | Kind | Name | Issue |\n")
	sb.WriteString("|-----------|------|------|-------|\n")

	shown := int64(0)
	for _, v := range violationList {
		if shown >= limit {
			break
		}
		// Truncate message for table
		msg := v.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", v.Namespace, v.Kind, v.Name, msg))
		shown++
	}

	if int64(len(violationList)) > limit {
		sb.WriteString(fmt.Sprintf("\n*Showing %d of %d violations. Use `limit` parameter to see more.*\n", limit, len(violationList)))
	}

	return sb.String(), false
}

func (s *Server) toolInstallOwnershipPolicy(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	// Parse parameters
	labels := []string{"owner", "team"}
	if v, ok := args["labels"].([]interface{}); ok && len(v) > 0 {
		labels = make([]string, len(v))
		for i, l := range v {
			labels[i], _ = l.(string)
		}
	}

	excludeNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "gatekeeper-system"}
	if v, ok := args["exclude_namespaces"].([]interface{}); ok && len(v) > 0 {
		excludeNamespaces = make([]string, len(v))
		for i, ns := range v {
			excludeNamespaces[i], _ = ns.(string)
		}
	}

	// Add openshift namespaces if on OpenShift
	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	// Check if this is OpenShift
	_, err = client.CoreV1().Namespaces().Get(ctx, "openshift", metav1.GetOptions{})
	isOpenShift := err == nil
	if isOpenShift {
		openshiftExcludes := []string{"openshift", "openshift-apiserver", "openshift-authentication",
			"openshift-cluster-samples-operator", "openshift-cluster-storage-operator",
			"openshift-config", "openshift-config-managed", "openshift-console",
			"openshift-controller-manager", "openshift-dns", "openshift-etcd",
			"openshift-image-registry", "openshift-infra", "openshift-ingress",
			"openshift-ingress-canary", "openshift-ingress-operator", "openshift-kube-apiserver",
			"openshift-kube-controller-manager", "openshift-kube-scheduler",
			"openshift-machine-api", "openshift-machine-config-operator",
			"openshift-marketplace", "openshift-monitoring", "openshift-multus",
			"openshift-network-diagnostics", "openshift-network-operator",
			"openshift-node", "openshift-oauth-apiserver", "openshift-operator-lifecycle-manager",
			"openshift-operators", "openshift-ovn-kubernetes", "openshift-sdn",
			"openshift-service-ca", "openshift-service-ca-operator"}
		excludeNamespaces = append(excludeNamespaces, openshiftExcludes...)
	}

	mode := "dryrun"
	if v, ok := args["mode"].(string); ok && v != "" {
		mode = v
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Installing Ownership Policy\n\n")

	// Create ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	constraintTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintTemplateAPIVersion,
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": ownershipTemplateName,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kubectl-claude",
				},
			},
			"spec": map[string]interface{}{
				"crd": map[string]interface{}{
					"spec": map[string]interface{}{
						"names": map[string]interface{}{
							"kind": "K8sRequiredLabels",
						},
						"validation": map[string]interface{}{
							"openAPIV3Schema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"labels": map[string]interface{}{
										"type":        "array",
										"description": "List of required labels",
										"items": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
				},
				"targets": []interface{}{
					map[string]interface{}{
						"target": "admission.k8s.gatekeeper.sh",
						"rego": `package k8srequiredlabels

violation[{"msg": msg, "details": {"missing_labels": missing}}] {
  provided := {label | input.review.object.metadata.labels[label]}
  required := {label | label := input.parameters.labels[_]}
  missing := required - provided
  count(missing) > 0
  msg := sprintf("Resource %v/%v is missing required labels: %v", [input.review.object.kind, input.review.object.metadata.name, missing])
}`,
					},
				},
			},
		},
	}

	_, err = dynClient.Resource(ctGVR).Create(ctx, constraintTemplate, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			sb.WriteString("**ConstraintTemplate:** Already exists (updating...)\n")
			_, err = dynClient.Resource(ctGVR).Update(ctx, constraintTemplate, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Sprintf("Failed to update ConstraintTemplate: %v", err), true
			}
			sb.WriteString("**ConstraintTemplate:** Updated ✓\n")
		} else {
			return fmt.Sprintf("Failed to create ConstraintTemplate: %v", err), true
		}
	} else {
		sb.WriteString("**ConstraintTemplate:** Created ✓\n")
	}

	// Wait a moment for the CRD to be available
	time.Sleep(2 * time.Second)

	// Create Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Build match kinds
	matchKinds := []interface{}{
		map[string]interface{}{
			"apiGroups": []interface{}{"apps"},
			"kinds":     []interface{}{"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"},
		},
		map[string]interface{}{
			"apiGroups": []interface{}{""},
			"kinds":     []interface{}{"Pod", "Service", "ConfigMap", "Secret"},
		},
		map[string]interface{}{
			"apiGroups": []interface{}{"batch"},
			"kinds":     []interface{}{"Job", "CronJob"},
		},
	}

	excludeNSInterface := make([]interface{}, len(excludeNamespaces))
	for i, ns := range excludeNamespaces {
		excludeNSInterface[i] = ns
	}

	labelsInterface := make([]interface{}, len(labels))
	for i, l := range labels {
		labelsInterface[i] = l
	}

	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintAPIVersion,
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kubectl-claude",
				},
			},
			"spec": map[string]interface{}{
				"enforcementAction": mode,
				"match": map[string]interface{}{
					"kinds":              matchKinds,
					"excludedNamespaces": excludeNSInterface,
				},
				"parameters": map[string]interface{}{
					"labels": labelsInterface,
				},
			},
		},
	}

	_, err = dynClient.Resource(constraintGVR).Create(ctx, constraint, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			sb.WriteString("**Constraint:** Already exists (updating...)\n")
			// Get existing to preserve resource version
			existing, getErr := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Sprintf("Failed to get existing constraint: %v", getErr), true
			}
			constraint.SetResourceVersion(existing.GetResourceVersion())
			_, err = dynClient.Resource(constraintGVR).Update(ctx, constraint, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Sprintf("Failed to update Constraint: %v", err), true
			}
			sb.WriteString("**Constraint:** Updated ✓\n")
		} else {
			return fmt.Sprintf("Failed to create Constraint: %v", err), true
		}
	} else {
		sb.WriteString("**Constraint:** Created ✓\n")
	}

	sb.WriteString(fmt.Sprintf("\n**Mode:** %s\n", mode))
	sb.WriteString(fmt.Sprintf("**Required Labels:** %s\n", strings.Join(labels, ", ")))
	sb.WriteString(fmt.Sprintf("**Excluded Namespaces:** %d namespaces\n", len(excludeNamespaces)))

	sb.WriteString("\n## Next Steps\n\n")
	if mode == "dryrun" {
		sb.WriteString("The policy is in **dryrun** mode. Violations are logged but resources are NOT blocked.\n\n")
		sb.WriteString("1. Use `list_ownership_violations` to see current violations\n")
		sb.WriteString("2. Fix violations by adding required labels to resources\n")
		sb.WriteString("3. Use `set_ownership_policy_mode` with mode=`warn` or `enforce` when ready\n")
	} else if mode == "warn" {
		sb.WriteString("The policy is in **warn** mode. Users will see warnings but resources are NOT blocked.\n\n")
		sb.WriteString("1. Use `list_ownership_violations` to see current violations\n")
		sb.WriteString("2. Use `set_ownership_policy_mode` with mode=`enforce` to start blocking\n")
	} else {
		sb.WriteString("The policy is in **enforce** mode. Resources without required labels will be **BLOCKED**.\n\n")
		sb.WriteString("⚠️ Users must add these labels to all new resources:\n")
		for _, l := range labels {
			sb.WriteString(fmt.Sprintf("- `%s`\n", l))
		}
	}

	return sb.String(), false
}

func (s *Server) toolSetOwnershipPolicyMode(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	mode, _ := args["mode"].(string)

	if mode == "" {
		return "mode is required (dryrun, warn, or enforce)", true
	}

	if mode != "dryrun" && mode != "warn" && mode != "enforce" {
		return "mode must be one of: dryrun, warn, enforce", true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Get existing constraint
	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		return "Ownership policy not installed. Use `install_ownership_policy` first.", false
	}

	// Get current mode
	currentMode, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	if currentMode == "" {
		currentMode = "deny"
	}

	if currentMode == mode {
		return fmt.Sprintf("Policy is already in `%s` mode.", mode), false
	}

	// Update mode
	err = unstructured.SetNestedField(constraint.Object, mode, "spec", "enforcementAction")
	if err != nil {
		return fmt.Sprintf("Failed to set mode: %v", err), true
	}

	_, err = dynClient.Resource(constraintGVR).Update(ctx, constraint, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to update constraint: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Policy Mode Updated\n\n")
	sb.WriteString(fmt.Sprintf("**Previous Mode:** %s\n", currentMode))
	sb.WriteString(fmt.Sprintf("**New Mode:** %s\n\n", mode))

	switch mode {
	case "dryrun":
		sb.WriteString("Violations are now logged but resources are **NOT blocked**.\n")
	case "warn":
		sb.WriteString("Users will see warnings but resources are **NOT blocked**.\n")
	case "enforce":
		sb.WriteString("⚠️ Resources without required labels will now be **BLOCKED**.\n")
	}

	return sb.String(), false
}

func (s *Server) toolUninstallOwnershipPolicy(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Uninstalling Ownership Policy\n\n")

	// Delete Constraint first
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	err = dynClient.Resource(constraintGVR).Delete(ctx, ownershipConstraintName, metav1.DeleteOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			sb.WriteString("**Constraint:** Not found (already deleted)\n")
		} else {
			return fmt.Sprintf("Failed to delete constraint: %v", err), true
		}
	} else {
		sb.WriteString("**Constraint:** Deleted ✓\n")
	}

	// Delete ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	err = dynClient.Resource(ctGVR).Delete(ctx, ownershipTemplateName, metav1.DeleteOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			sb.WriteString("**ConstraintTemplate:** Not found (already deleted)\n")
		} else {
			return fmt.Sprintf("Failed to delete ConstraintTemplate: %v", err), true
		}
	} else {
		sb.WriteString("**ConstraintTemplate:** Deleted ✓\n")
	}

	sb.WriteString("\nOwnership policy has been removed. Resources will no longer be checked for ownership labels.\n")

	return sb.String(), false
}

// GitOps Tools

func (s *Server) getRestConfigForCluster(clusterName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
}

func (s *Server) toolDetectDrift(ctx context.Context, args map[string]interface{}) (string, bool) {
	repoURL, _ := args["repo_url"].(string)
	path, _ := args["path"].(string)
	branch, _ := args["branch"].(string)
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	if repoURL == "" {
		return "repo_url is required", true
	}

	// Get REST config for the cluster
	restConfig, err := s.getRestConfigForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client config: %v", err), true
	}

	// Determine cluster name for output
	clusterName := cluster
	if clusterName == "" {
		clusterName = "current-context"
	}

	// Read manifests from git
	reader := gitops.NewManifestReader()
	defer reader.Cleanup()

	source := gitops.ManifestSource{
		Repo:   repoURL,
		Path:   path,
		Branch: branch,
	}

	manifests, err := reader.ReadFromGit(source)
	if err != nil {
		return fmt.Sprintf("Failed to read manifests from git: %v", err), true
	}

	if len(manifests) == 0 {
		return fmt.Sprintf("No manifests found in %s (path: %s)", repoURL, path), false
	}

	// Filter manifests by namespace if specified
	if namespace != "" {
		var filtered []gitops.Manifest
		for _, m := range manifests {
			if m.GetNamespace() == namespace || gitops.IsClusterScoped(m.Kind) {
				filtered = append(filtered, m)
			}
		}
		manifests = filtered
	}

	// Create drift detector
	detector, err := gitops.NewDriftDetector(restConfig)
	if err != nil {
		return fmt.Sprintf("Failed to create drift detector: %v", err), true
	}

	// Detect drift
	drifts, err := detector.DetectDrift(ctx, manifests, clusterName)
	if err != nil {
		return fmt.Sprintf("Failed to detect drift: %v", err), true
	}

	// Build response
	var sb strings.Builder
	sb.WriteString("# GitOps Drift Detection\n\n")
	sb.WriteString(fmt.Sprintf("**Repository:** %s\n", repoURL))
	if path != "" {
		sb.WriteString(fmt.Sprintf("**Path:** %s\n", path))
	}
	if branch != "" {
		sb.WriteString(fmt.Sprintf("**Branch:** %s\n", branch))
	}
	sb.WriteString(fmt.Sprintf("**Cluster:** %s\n", clusterName))
	sb.WriteString(fmt.Sprintf("**Manifests Found:** %d\n\n", len(manifests)))

	if len(drifts) == 0 {
		sb.WriteString("✅ **No drift detected** - cluster state matches Git manifests\n")

		// Also return JSON for programmatic parsing
		result := map[string]interface{}{
			"drifted":   false,
			"resources": []interface{}{},
			"summary": map[string]interface{}{
				"total":    len(manifests),
				"synced":   len(manifests),
				"drifted":  0,
				"missing":  0,
				"modified": 0,
			},
		}
		jsonBytes, _ := json.MarshalIndent(result, "", "  ")
		sb.WriteString("\n```json\n")
		sb.WriteString(string(jsonBytes))
		sb.WriteString("\n```\n")

		return sb.String(), false
	}

	// Count by drift type
	missing := 0
	modified := 0
	for _, d := range drifts {
		switch d.DriftType {
		case gitops.DriftTypeMissing:
			missing++
		case gitops.DriftTypeModified:
			modified++
		}
	}

	sb.WriteString(fmt.Sprintf("⚠️ **Drift detected**: %d resource(s) out of sync\n\n", len(drifts)))
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Missing from cluster: %d\n", missing))
	sb.WriteString(fmt.Sprintf("- Modified in cluster: %d\n", modified))
	sb.WriteString("\n## Details\n\n")

	// Build JSON resources array
	resources := make([]map[string]interface{}, 0, len(drifts))

	for _, d := range drifts {
		icon := "📝"
		if d.DriftType == gitops.DriftTypeMissing {
			icon = "❌"
		}

		sb.WriteString(fmt.Sprintf("### %s %s/%s\n", icon, d.Kind, d.Name))
		if d.Namespace != "" {
			sb.WriteString(fmt.Sprintf("**Namespace:** %s\n", d.Namespace))
		}
		sb.WriteString(fmt.Sprintf("**Type:** %s\n", d.DriftType))

		if len(d.Differences) > 0 {
			sb.WriteString("**Differences:**\n")
			for _, diff := range d.Differences {
				sb.WriteString(fmt.Sprintf("- %s\n", diff))
			}
		}
		sb.WriteString("\n")

		// Build resource for JSON output
		resource := map[string]interface{}{
			"kind":      d.Kind,
			"name":      d.Name,
			"namespace": d.Namespace,
			"driftType": string(d.DriftType),
		}
		if len(d.Differences) > 0 {
			resource["field"] = d.Differences[0]
			resource["differences"] = d.Differences
		}
		if d.GitValue != nil {
			resource["gitValue"] = fmt.Sprintf("%v", d.GitValue)
		}
		if d.ClusterValue != nil {
			resource["clusterValue"] = fmt.Sprintf("%v", d.ClusterValue)
		}
		resources = append(resources, resource)
	}

	// Add JSON for programmatic parsing
	result := map[string]interface{}{
		"drifted":   true,
		"resources": resources,
		"summary": map[string]interface{}{
			"total":    len(manifests),
			"synced":   len(manifests) - len(drifts),
			"drifted":  len(drifts),
			"missing":  missing,
			"modified": modified,
		},
	}
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	sb.WriteString("\n```json\n")
	sb.WriteString(string(jsonBytes))
	sb.WriteString("\n```\n")

	return sb.String(), false
}
