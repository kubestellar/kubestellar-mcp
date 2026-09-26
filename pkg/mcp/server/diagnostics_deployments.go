package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toolFindDeploymentIssues(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	client, err := d.GetClientForCluster(cluster)
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
			_, _ = fmt.Fprintf(&sb, "\n📛 %s/%s\n", deploy.Namespace, deploy.Name)
			for _, issue := range issues {
				_, _ = fmt.Fprintf(&sb, "   - %s\n", issue)
			}
		}
	}

	if issueCount == 0 {
		return "✅ No deployment issues found", false
	}

	header := fmt.Sprintf("Found %d deployments with issues:\n", issueCount)
	return header + sb.String(), false
}

func toolCheckResourceLimits(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}

	client, err := d.GetClientForCluster(cluster)
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
			_, _ = fmt.Fprintf(&sb, "\n⚠️  %s/%s\n", pod.Namespace, pod.Name)
			for _, issue := range containerIssues {
				_, _ = fmt.Fprintf(&sb, "   - %s\n", issue)
			}
		}
	}

	if issueCount == 0 {
		return "✅ All pods have resource limits configured", false
	}

	header := fmt.Sprintf("Found %d pods without proper resource limits:\n", issueCount)
	return header + sb.String(), false
}
