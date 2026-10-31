package diagnostics

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	corev1 "k8s.io/api/core/v1"
)

func toolCheckSecurityIssues(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {

	pods, errMsg, isErr := listPodsForClusterArgs(ctx, d, args)
	if isErr {
		return errMsg, true
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
