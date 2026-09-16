package server

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) toolAnalyzeNamespace(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	if namespace == "" {
		return "namespace is required", true
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "📊 Namespace Analysis: %s\n\n", namespace)

	// Get namespace
	ns, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get namespace: %v", err), true
	}

	_, _ = fmt.Fprintf(&sb, "Status: %s\n", ns.Status.Phase)
	_, _ = fmt.Fprintf(&sb, "Created: %s\n\n", ns.CreationTimestamp.Format("2006-01-02 15:04:05"))

	// Get resource quotas
	quotas, _ := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if len(quotas.Items) > 0 {
		sb.WriteString("📋 Resource Quotas:\n")
		for _, quota := range quotas.Items {
			_, _ = fmt.Fprintf(&sb, "  %s:\n", quota.Name)
			for resource, hard := range quota.Status.Hard {
				used := quota.Status.Used[resource]
				_, _ = fmt.Fprintf(&sb, "    %s: %s / %s\n", resource, used.String(), hard.String())
			}
		}
		sb.WriteString("\n")
	}

	// Get limit ranges
	limitRanges, _ := client.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if len(limitRanges.Items) > 0 {
		sb.WriteString("📏 Limit Ranges:\n")
		for _, lr := range limitRanges.Items {
			_, _ = fmt.Fprintf(&sb, "  %s\n", lr.Name)
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
	_, _ = fmt.Fprintf(&sb, "  Total: %d\n", len(pods.Items))
	_, _ = fmt.Fprintf(&sb, "  Running: %d\n", runningPods)
	if pendingPods > 0 {
		_, _ = fmt.Fprintf(&sb, "  Pending: %d ⚠️\n", pendingPods)
	}
	if failedPods > 0 {
		_, _ = fmt.Fprintf(&sb, "  Failed: %d ❌\n", failedPods)
	}
	if crashingPods > 0 {
		_, _ = fmt.Fprintf(&sb, "  Crashing/Restarting: %d 🔄\n", crashingPods)
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
	_, _ = fmt.Fprintf(&sb, "🚀 Deployments: %d", len(deployments.Items))
	if unhealthyDeploys > 0 {
		_, _ = fmt.Fprintf(&sb, " (%d unhealthy ⚠️)", unhealthyDeploys)
	}
	sb.WriteString("\n")

	// Get services
	services, _ := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	_, _ = fmt.Fprintf(&sb, "🌐 Services: %d\n", len(services.Items))

	// Get PVCs and check status
	pvcs, _ := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	pendingPVCs := 0
	for _, pvc := range pvcs.Items {
		if pvc.Status.Phase == corev1.ClaimPending {
			pendingPVCs++
		}
	}
	_, _ = fmt.Fprintf(&sb, "💾 PVCs: %d", len(pvcs.Items))
	if pendingPVCs > 0 {
		_, _ = fmt.Fprintf(&sb, " (%d pending ⚠️)", pendingPVCs)
	}
	sb.WriteString("\n")

	// Get configmaps and secrets
	configMaps, _ := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	secrets, _ := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	_, _ = fmt.Fprintf(&sb, "📄 ConfigMaps: %d\n", len(configMaps.Items))
	_, _ = fmt.Fprintf(&sb, "🔐 Secrets: %d\n", len(secrets.Items))

	// Check for warning events
	events, _ := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "type=Warning",
	})
	if len(events.Items) > 0 {
		_, _ = fmt.Fprintf(&sb, "\n⚠️  Recent Warnings: %d events\n", len(events.Items))
	}

	return sb.String(), false
}
