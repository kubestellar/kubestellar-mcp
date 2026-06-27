package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) toolGetPods(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
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
	_, _ = fmt.Fprintf(&sb, "Found %d pods:\n\n", len(pods.Items))

	for _, pod := range pods.Items {
		status := string(pod.Status.Phase)
		ready := 0
		total := len(pod.Status.ContainerStatuses)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
		}

		startTime := "<pending>"
		if pod.Status.StartTime != nil {
			startTime = pod.Status.StartTime.Format("2006-01-02 15:04:05")
		}

		_, _ = fmt.Fprintf(&sb, "%-50s %-12s %d/%d   %s\n",
			pod.Namespace+"/"+pod.Name,
			status,
			ready, total,
			startTime)
	}

	return sb.String(), false
}

func (s *Server) toolGetDeployments(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

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
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

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
	_, _ = fmt.Fprintf(&sb, "Found %d services:\n\n", len(services.Items))

	for _, svc := range services.Items {
		_, _ = fmt.Fprintf(&sb, "%-40s %-15s %-20s %s\n",
			svc.Namespace+"/"+svc.Name,
			string(svc.Spec.Type),
			svc.Spec.ClusterIP,
			formatPorts(svc.Spec.Ports))
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
	_, _ = fmt.Fprintf(&sb, "Found %d nodes:\n\n", len(nodes.Items))

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

		_, _ = fmt.Fprintf(&sb, "%-40s %-10s %-20s %s\n",
			node.Name,
			status,
			roleStr,
			node.Status.NodeInfo.KubeletVersion)
	}

	return sb.String(), false
}

func (s *Server) toolGetEvents(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
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
	_, _ = fmt.Fprintf(&sb, "Found %d events:\n\n", len(events.Items))

	for _, event := range events.Items {
		_, _ = fmt.Fprintf(&sb, "[%s] %s/%s: %s\n",
			event.Type,
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Message)
	}

	return sb.String(), false
}

func (s *Server) toolDescribePod(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
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
	_, _ = fmt.Fprintf(&sb, "Name: %s\n", pod.Name)
	_, _ = fmt.Fprintf(&sb, "Namespace: %s\n", pod.Namespace)
	_, _ = fmt.Fprintf(&sb, "Status: %s\n", pod.Status.Phase)
	_, _ = fmt.Fprintf(&sb, "Node: %s\n", pod.Spec.NodeName)
	_, _ = fmt.Fprintf(&sb, "IP: %s\n", pod.Status.PodIP)

	if pod.Status.StartTime != nil {
		_, _ = fmt.Fprintf(&sb, "Start Time: %s\n", pod.Status.StartTime.Format("2006-01-02 15:04:05"))
	}

	sb.WriteString("\nContainers:\n")
	for _, container := range pod.Spec.Containers {
		_, _ = fmt.Fprintf(&sb, "  - %s (image: %s)\n", container.Name, container.Image)
	}

	sb.WriteString("\nContainer Statuses:\n")
	for _, cs := range pod.Status.ContainerStatuses {
		ready := "not ready"
		if cs.Ready {
			ready = "ready"
		}
		_, _ = fmt.Fprintf(&sb, "  - %s: %s, restarts: %d\n", cs.Name, ready, cs.RestartCount)
	}

	sb.WriteString("\nConditions:\n")
	for _, cond := range pod.Status.Conditions {
		_, _ = fmt.Fprintf(&sb, "  - %s: %s\n", cond.Type, cond.Status)
	}

	return sb.String(), false
}

func (s *Server) toolGetPodLogs(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
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

