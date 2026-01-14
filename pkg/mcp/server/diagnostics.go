package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Diagnostic Tools

func (s *Server) toolFindPodIssues(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	includeCompleted := args["include_completed"] == "true"

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var pods *corev1.PodList
	if namespace == "" {
		pods, err = client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	} else {
		pods, err = client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list pods: %v", err), true
	}

	var sb strings.Builder
	issueCount := 0

	for _, pod := range pods.Items {
		issues := []string{}

		// Skip completed pods unless requested
		if !includeCompleted && (pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) {
			continue
		}

		// Check pod phase
		switch pod.Status.Phase {
		case corev1.PodPending:
			issues = append(issues, "Pod is Pending")
		case corev1.PodFailed:
			issues = append(issues, fmt.Sprintf("Pod Failed: %s", pod.Status.Reason))
		}

		// Check container statuses
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.RestartCount > 5 {
				issues = append(issues, fmt.Sprintf("Container %s has %d restarts", cs.Name, cs.RestartCount))
			}

			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				switch reason {
				case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName":
					msg := cs.State.Waiting.Message
					if len(msg) > 100 {
						msg = msg[:100] + "..."
					}
					issues = append(issues, fmt.Sprintf("Container %s: %s - %s", cs.Name, reason, msg))
				}
			}

			if cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled" {
				issues = append(issues, fmt.Sprintf("Container %s was OOMKilled", cs.Name))
			}

			if !cs.Ready && cs.State.Running != nil {
				issues = append(issues, fmt.Sprintf("Container %s running but not ready", cs.Name))
			}
		}

		// Check init container statuses
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				issues = append(issues, fmt.Sprintf("Init container %s waiting: %s", cs.Name, reason))
			}
		}

		// Check for unschedulable
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
				issues = append(issues, fmt.Sprintf("Unschedulable: %s", cond.Message))
			}
		}

		if len(issues) > 0 {
			issueCount++
			sb.WriteString(fmt.Sprintf("\n📛 %s/%s\n", pod.Namespace, pod.Name))
			for _, issue := range issues {
				sb.WriteString(fmt.Sprintf("   - %s\n", issue))
			}
		}
	}

	if issueCount == 0 {
		return "✅ No pod issues found", false
	}

	header := fmt.Sprintf("Found %d pods with issues:\n", issueCount)
	return header + sb.String(), false
}

func (s *Server) toolFindDeploymentIssues(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var deployments *appsv1.DeploymentList
	if namespace == "" {
		deployments, err = client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	} else {
		deployments, err = client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list deployments: %v", err), true
	}

	// Also get ReplicaSets to find hidden issues
	var replicaSets *appsv1.ReplicaSetList
	if namespace == "" {
		replicaSets, _ = client.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	} else {
		replicaSets, _ = client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	}

	// Build a map of deployment to latest replicaset
	rsMap := make(map[string]*appsv1.ReplicaSet)
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		for _, owner := range rs.OwnerReferences {
			if owner.Kind == "Deployment" {
				key := rs.Namespace + "/" + owner.Name
				if existing, ok := rsMap[key]; !ok || rs.CreationTimestamp.After(existing.CreationTimestamp.Time) {
					rsMap[key] = rs
				}
			}
		}
	}

	var sb strings.Builder
	issueCount := 0

	for _, deploy := range deployments.Items {
		issues := []string{}

		// Check replica status
		if deploy.Status.Replicas != deploy.Status.ReadyReplicas {
			issues = append(issues, fmt.Sprintf("Only %d/%d replicas ready",
				deploy.Status.ReadyReplicas, deploy.Status.Replicas))
		}

		if deploy.Status.UnavailableReplicas > 0 {
			issues = append(issues, fmt.Sprintf("%d replicas unavailable",
				deploy.Status.UnavailableReplicas))
		}

		// Check conditions
		for _, cond := range deploy.Status.Conditions {
			if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
				issues = append(issues, fmt.Sprintf("Rollout stuck: %s", cond.Message))
			}
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionFalse {
				issues = append(issues, fmt.Sprintf("Not available: %s", cond.Message))
			}
			if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
				issues = append(issues, fmt.Sprintf("Replica failure: %s", cond.Message))
			}
		}

		// Check ReplicaSet for hidden issues
		key := deploy.Namespace + "/" + deploy.Name
		if rs, ok := rsMap[key]; ok {
			for _, cond := range rs.Status.Conditions {
				if cond.Type == appsv1.ReplicaSetReplicaFailure && cond.Status == corev1.ConditionTrue {
					issues = append(issues, fmt.Sprintf("ReplicaSet error: %s", cond.Message))
				}
			}
		}

		if len(issues) > 0 {
			issueCount++
			sb.WriteString(fmt.Sprintf("\n📛 %s/%s\n", deploy.Namespace, deploy.Name))
			for _, issue := range issues {
				sb.WriteString(fmt.Sprintf("   - %s\n", issue))
			}
		}
	}

	if issueCount == 0 {
		return "✅ No deployment issues found", false
	}

	header := fmt.Sprintf("Found %d deployments with issues:\n", issueCount)
	return header + sb.String(), false
}

func (s *Server) toolCheckResourceLimits(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var pods *corev1.PodList
	if namespace == "" {
		pods, err = client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	} else {
		pods, err = client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list pods: %v", err), true
	}

	var sb strings.Builder
	issueCount := 0

	for _, pod := range pods.Items {
		// Skip completed/failed pods
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		containerIssues := []string{}

		for _, container := range pod.Spec.Containers {
			issues := []string{}

			if container.Resources.Limits.Cpu().IsZero() {
				issues = append(issues, "no CPU limit")
			}
			if container.Resources.Limits.Memory().IsZero() {
				issues = append(issues, "no memory limit")
			}
			if container.Resources.Requests.Cpu().IsZero() {
				issues = append(issues, "no CPU request")
			}
			if container.Resources.Requests.Memory().IsZero() {
				issues = append(issues, "no memory request")
			}

			if len(issues) > 0 {
				containerIssues = append(containerIssues,
					fmt.Sprintf("Container %s: %s", container.Name, strings.Join(issues, ", ")))
			}
		}

		if len(containerIssues) > 0 {
			issueCount++
			sb.WriteString(fmt.Sprintf("\n⚠️  %s/%s\n", pod.Namespace, pod.Name))
			for _, issue := range containerIssues {
				sb.WriteString(fmt.Sprintf("   - %s\n", issue))
			}
		}
	}

	if issueCount == 0 {
		return "✅ All pods have resource limits configured", false
	}

	header := fmt.Sprintf("Found %d pods without proper resource limits:\n", issueCount)
	return header + sb.String(), false
}

func (s *Server) toolCheckSecurityIssues(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var pods *corev1.PodList
	if namespace == "" {
		pods, err = client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	} else {
		pods, err = client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list pods: %v", err), true
	}

	var sb strings.Builder
	issueCount := 0

	for _, pod := range pods.Items {
		// Skip system namespaces by default
		if strings.HasPrefix(pod.Namespace, "kube-") {
			continue
		}
		// Skip completed/failed pods
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		issues := []string{}

		// Check pod-level security
		if pod.Spec.HostNetwork {
			issues = append(issues, "🔴 Uses host network")
		}
		if pod.Spec.HostPID {
			issues = append(issues, "🔴 Uses host PID namespace")
		}
		if pod.Spec.HostIPC {
			issues = append(issues, "🔴 Uses host IPC namespace")
		}

		// Check containers
		for _, container := range pod.Spec.Containers {
			sc := container.SecurityContext

			if sc != nil {
				if sc.Privileged != nil && *sc.Privileged {
					issues = append(issues, fmt.Sprintf("🔴 Container %s is privileged", container.Name))
				}
				if sc.RunAsUser != nil && *sc.RunAsUser == 0 {
					issues = append(issues, fmt.Sprintf("🟠 Container %s runs as root (UID 0)", container.Name))
				}
				if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
					issues = append(issues, fmt.Sprintf("🟡 Container %s allows privilege escalation", container.Name))
				}
				if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
					issues = append(issues, fmt.Sprintf("🟡 Container %s has writable root filesystem", container.Name))
				}
			} else {
				issues = append(issues, fmt.Sprintf("🟡 Container %s has no security context", container.Name))
			}

			// Check for sensitive mounts
			for _, mount := range container.VolumeMounts {
				if mount.MountPath == "/var/run/docker.sock" {
					issues = append(issues, fmt.Sprintf("🔴 Container %s mounts Docker socket", container.Name))
				}
			}
		}

		if len(issues) > 0 {
			issueCount++
			sb.WriteString(fmt.Sprintf("\n🔓 %s/%s\n", pod.Namespace, pod.Name))
			for _, issue := range issues {
				sb.WriteString(fmt.Sprintf("   - %s\n", issue))
			}
		}
	}

	if issueCount == 0 {
		return "✅ No obvious security issues found", false
	}

	header := fmt.Sprintf("Found %d pods with security concerns:\n🔴 Critical | 🟠 High | 🟡 Medium\n", issueCount)
	return header + sb.String(), false
}

func (s *Server) toolAnalyzeNamespace(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	if namespace == "" {
		return "namespace is required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 Namespace Analysis: %s\n\n", namespace))

	// Get namespace
	ns, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get namespace: %v", err), true
	}

	sb.WriteString(fmt.Sprintf("Status: %s\n", ns.Status.Phase))
	sb.WriteString(fmt.Sprintf("Created: %s\n\n", ns.CreationTimestamp.Format("2006-01-02 15:04:05")))

	// Get resource quotas
	quotas, _ := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if len(quotas.Items) > 0 {
		sb.WriteString("📋 Resource Quotas:\n")
		for _, quota := range quotas.Items {
			sb.WriteString(fmt.Sprintf("  %s:\n", quota.Name))
			for resource, hard := range quota.Status.Hard {
				used := quota.Status.Used[resource]
				sb.WriteString(fmt.Sprintf("    %s: %s / %s\n", resource, used.String(), hard.String()))
			}
		}
		sb.WriteString("\n")
	}

	// Get limit ranges
	limitRanges, _ := client.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if len(limitRanges.Items) > 0 {
		sb.WriteString("📏 Limit Ranges:\n")
		for _, lr := range limitRanges.Items {
			sb.WriteString(fmt.Sprintf("  %s\n", lr.Name))
		}
		sb.WriteString("\n")
	}

	// Get pods and check for issues
	pods, _ := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	runningPods := 0
	pendingPods := 0
	failedPods := 0
	crashingPods := 0
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			runningPods++
			// Check for crashlooping
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.RestartCount > 5 || (cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff") {
					crashingPods++
					break
				}
			}
		case corev1.PodPending:
			pendingPods++
		case corev1.PodFailed:
			failedPods++
		}
	}

	sb.WriteString("📦 Pods:\n")
	sb.WriteString(fmt.Sprintf("  Total: %d\n", len(pods.Items)))
	sb.WriteString(fmt.Sprintf("  Running: %d\n", runningPods))
	if pendingPods > 0 {
		sb.WriteString(fmt.Sprintf("  Pending: %d ⚠️\n", pendingPods))
	}
	if failedPods > 0 {
		sb.WriteString(fmt.Sprintf("  Failed: %d ❌\n", failedPods))
	}
	if crashingPods > 0 {
		sb.WriteString(fmt.Sprintf("  Crashing/Restarting: %d 🔄\n", crashingPods))
	}
	sb.WriteString("\n")

	// Get deployments and check health
	deployments, _ := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	unhealthyDeploys := 0
	for _, d := range deployments.Items {
		if d.Status.ReadyReplicas < d.Status.Replicas {
			unhealthyDeploys++
		}
	}
	sb.WriteString(fmt.Sprintf("🚀 Deployments: %d", len(deployments.Items)))
	if unhealthyDeploys > 0 {
		sb.WriteString(fmt.Sprintf(" (%d unhealthy ⚠️)", unhealthyDeploys))
	}
	sb.WriteString("\n")

	// Get services
	services, _ := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	sb.WriteString(fmt.Sprintf("🌐 Services: %d\n", len(services.Items)))

	// Get PVCs and check status
	pvcs, _ := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	pendingPVCs := 0
	for _, pvc := range pvcs.Items {
		if pvc.Status.Phase == corev1.ClaimPending {
			pendingPVCs++
		}
	}
	sb.WriteString(fmt.Sprintf("💾 PVCs: %d", len(pvcs.Items)))
	if pendingPVCs > 0 {
		sb.WriteString(fmt.Sprintf(" (%d pending ⚠️)", pendingPVCs))
	}
	sb.WriteString("\n")

	// Get configmaps and secrets
	configMaps, _ := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	secrets, _ := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	sb.WriteString(fmt.Sprintf("📄 ConfigMaps: %d\n", len(configMaps.Items)))
	sb.WriteString(fmt.Sprintf("🔐 Secrets: %d\n", len(secrets.Items)))

	// Check for warning events
	events, _ := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "type=Warning",
	})
	if len(events.Items) > 0 {
		sb.WriteString(fmt.Sprintf("\n⚠️  Recent Warnings: %d events\n", len(events.Items)))
	}

	return sb.String(), false
}

func (s *Server) toolGetWarningEvents(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)
	involvedObject, _ := args["involved_object"].(string)
	limit := int64(50)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	listOpts := metav1.ListOptions{
		FieldSelector: "type=Warning",
		Limit:         limit,
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

	var sb strings.Builder
	count := 0

	for _, event := range events.Items {
		// Filter by involved object if specified
		if involvedObject != "" && event.InvolvedObject.Name != involvedObject {
			continue
		}

		count++
		age := ""
		if event.LastTimestamp.Time.IsZero() {
			age = "unknown"
		} else {
			age = formatAge(event.LastTimestamp.Time)
		}

		sb.WriteString(fmt.Sprintf("⚠️  [%s] %s/%s\n", age, event.InvolvedObject.Kind, event.InvolvedObject.Name))
		sb.WriteString(fmt.Sprintf("   %s: %s\n", event.Reason, event.Message))
		if event.Count > 1 {
			sb.WriteString(fmt.Sprintf("   (occurred %d times)\n", event.Count))
		}
		sb.WriteString("\n")
	}

	if count == 0 {
		return "✅ No warning events found", false
	}

	header := fmt.Sprintf("Found %d warning events:\n\n", count)
	return header + sb.String(), false
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
