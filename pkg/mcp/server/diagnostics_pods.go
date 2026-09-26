package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toolFindPodIssues(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	includeCompleted := args["include_completed"] == "true"

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
			_, _ = fmt.Fprintf(&sb, "\n📛 %s/%s\n", pod.Namespace, pod.Name)
			for _, issue := range issues {
				_, _ = fmt.Fprintf(&sb, "   - %s\n", issue)
			}
		}
	}

	if issueCount == 0 {
		return "✅ No pod issues found", false
	}

	header := fmt.Sprintf("Found %d pods with issues:\n", issueCount)
	return header + sb.String(), false
}
