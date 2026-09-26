package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toolCheckSecurityIssues(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
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
			_, _ = fmt.Fprintf(&sb, "\n🔓 %s/%s\n", pod.Namespace, pod.Name)
			for _, issue := range issues {
				_, _ = fmt.Fprintf(&sb, "   - %s\n", issue)
			}
		}
	}

	if issueCount == 0 {
		return "✅ No obvious security issues found", false
	}

	header := fmt.Sprintf("Found %d pods with security concerns:\n🔴 Critical | 🟠 High | 🟡 Medium\n", issueCount)
	return header + sb.String(), false
}
