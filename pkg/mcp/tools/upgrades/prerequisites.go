package upgrades

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// GetUpgradePrerequisites checks prerequisites before upgrading.
func GetUpgradePrerequisites(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := ca.GetClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	dynClient, err := ca.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Upgrade Prerequisites Check\n\n")

	passed := 0
	failed := 0
	warnings := 0

	nodePassed, nodeFailed := checkNodeHealthPrerequisite(ctx, client, &sb)
	passed += nodePassed
	failed += nodeFailed

	podPassed, podFailed, podWarnings := checkPodHealthPrerequisite(ctx, client, &sb)
	passed += podPassed
	failed += podFailed
	warnings += podWarnings

	// OpenShift-specific checks only apply when the cluster exposes ClusterVersion.
	if _, err = dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{}); err == nil {
		sb.WriteString("\n## OpenShift-Specific Checks\n\n")

		osPassed, osFailed, osWarnings := checkOpenShiftClusterOperators(ctx, dynClient, &sb)
		passed += osPassed
		failed += osFailed
		warnings += osWarnings

		mcpPassed, mcpFailed := checkOpenShiftMachineConfigPools(ctx, dynClient, &sb)
		passed += mcpPassed
		failed += mcpFailed
	}

	writePrerequisitesSummary(&sb, passed, failed, warnings)

	return sb.String(), false
}

// checkNodeHealthPrerequisite verifies that all cluster nodes are Ready.
func checkNodeHealthPrerequisite(ctx context.Context, client kubernetes.Interface, sb *strings.Builder) (passed, failed int) {
	sb.WriteString("## Node Health\n\n")
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		_, _ = fmt.Fprintf(sb, "- [ ] Unable to check nodes: %v\n", err)
		return 0, 1
	}

	readyNodes := 0
	notReadyNodes := []string{}
	for _, node := range nodes.Items {
		ready := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if ready {
			readyNodes++
		} else {
			notReadyNodes = append(notReadyNodes, node.Name)
		}
	}

	if len(notReadyNodes) == 0 {
		_, _ = fmt.Fprintf(sb, "- [x] All nodes ready (%d/%d)\n", readyNodes, len(nodes.Items))
		return 1, 0
	}

	_, _ = fmt.Fprintf(sb, "- [ ] Some nodes not ready (%d/%d)\n", readyNodes, len(nodes.Items))
	_, _ = fmt.Fprintf(sb, "  - Not ready: %s\n", strings.Join(notReadyNodes, ", "))
	return 0, 1
}

// checkPodHealthPrerequisite verifies that no pods are crashing, stuck pulling
// images, or excessively pending.
func checkPodHealthPrerequisite(ctx context.Context, client kubernetes.Interface, sb *strings.Builder) (passed, failed, warnings int) {
	sb.WriteString("\n## Pod Health\n\n")
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		_, _ = fmt.Fprintf(sb, "- [ ] Unable to check pods: %v\n", err)
		return 0, 1, 0
	}

	crashingPods, imagePullPods, pendingPods := collectPodIssues(pods.Items)

	if len(crashingPods) == 0 {
		sb.WriteString("- [x] No pods in CrashLoopBackOff\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d pods in CrashLoopBackOff\n", len(crashingPods))
		for _, p := range crashingPods[:min(5, len(crashingPods))] {
			_, _ = fmt.Fprintf(sb, "  - %s\n", p)
		}
		if len(crashingPods) > 5 {
			_, _ = fmt.Fprintf(sb, "  - ... and %d more\n", len(crashingPods)-5)
		}
		failed++
	}

	if len(imagePullPods) == 0 {
		sb.WriteString("- [x] No pods with image pull errors\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d pods with image pull errors\n", len(imagePullPods))
		for _, p := range imagePullPods[:min(5, len(imagePullPods))] {
			_, _ = fmt.Fprintf(sb, "  - %s\n", p)
		}
		warnings++
	}

	if len(pendingPods) <= 5 {
		_, _ = fmt.Fprintf(sb, "- [x] Few pending pods (%d)\n", len(pendingPods))
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] Many pending pods (%d)\n", len(pendingPods))
		warnings++
	}

	return passed, failed, warnings
}

// collectPodIssues scans pods for crash loops, image pull failures, and pending state.
func collectPodIssues(pods []corev1.Pod) (crashingPods, imagePullPods, pendingPods []string) {
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		if pod.Status.Phase == corev1.PodPending {
			pendingPods = append(pendingPods, pod.Namespace+"/"+pod.Name)
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				switch reason {
				case "CrashLoopBackOff":
					crashingPods = append(crashingPods, pod.Namespace+"/"+pod.Name)
				case "ImagePullBackOff", "ErrImagePull":
					imagePullPods = append(imagePullPods, pod.Namespace+"/"+pod.Name)
				}
			}
		}
	}
	return crashingPods, imagePullPods, pendingPods
}

// checkOpenShiftClusterOperators verifies ClusterOperators are not degraded,
// unavailable, or progressing.
func checkOpenShiftClusterOperators(ctx context.Context, dynClient dynamic.Interface, sb *strings.Builder) (passed, failed, warnings int) {
	cos, err := dynClient.Resource(clusterOperatorGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		_, _ = fmt.Fprintf(sb, "- [ ] Unable to check ClusterOperators: %v\n", err)
		return 0, 1, 0
	}

	degradedOps := []string{}
	unavailableOps := []string{}
	progressingOps := []string{}

	for _, co := range cos.Items {
		conditions, _, _ := unstructured.NestedSlice(co.Object, "status", "conditions")
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")

			switch condType {
			case "Degraded":
				if condStatus == "True" {
					degradedOps = append(degradedOps, co.GetName())
				}
			case "Available":
				if condStatus == "False" {
					unavailableOps = append(unavailableOps, co.GetName())
				}
			case "Progressing":
				if condStatus == "True" {
					progressingOps = append(progressingOps, co.GetName())
				}
			}
		}
	}

	if len(degradedOps) == 0 {
		sb.WriteString("- [x] No degraded ClusterOperators\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d degraded ClusterOperators: %s\n", len(degradedOps), strings.Join(degradedOps, ", "))
		failed++
	}

	if len(unavailableOps) == 0 {
		sb.WriteString("- [x] All ClusterOperators available\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d unavailable ClusterOperators: %s\n", len(unavailableOps), strings.Join(unavailableOps, ", "))
		failed++
	}

	if len(progressingOps) == 0 {
		sb.WriteString("- [x] No ClusterOperators progressing\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d ClusterOperators progressing: %s\n", len(progressingOps), strings.Join(progressingOps, ", "))
		warnings++
	}

	return passed, failed, warnings
}

// checkOpenShiftMachineConfigPools verifies MachineConfigPools are not
// updating or degraded.
func checkOpenShiftMachineConfigPools(ctx context.Context, dynClient dynamic.Interface, sb *strings.Builder) (passed, failed int) {
	mcps, err := dynClient.Resource(machineConfigPoolGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0
	}

	updatingPools := []string{}
	degradedPools := []string{}

	for _, mcp := range mcps.Items {
		conditions, _, _ := unstructured.NestedSlice(mcp.Object, "status", "conditions")
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")

			if condType == "Updating" && condStatus == "True" {
				updatingPools = append(updatingPools, mcp.GetName())
			}
			if condType == "Degraded" && condStatus == "True" {
				degradedPools = append(degradedPools, mcp.GetName())
			}
		}
	}

	if len(updatingPools) == 0 {
		sb.WriteString("- [x] No MachineConfigPools updating\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d MachineConfigPools updating: %s\n", len(updatingPools), strings.Join(updatingPools, ", "))
		sb.WriteString("  - Wait for current updates to complete before upgrading\n")
		failed++
	}

	if len(degradedPools) == 0 {
		sb.WriteString("- [x] No MachineConfigPools degraded\n")
		passed++
	} else {
		_, _ = fmt.Fprintf(sb, "- [ ] %d MachineConfigPools degraded: %s\n", len(degradedPools), strings.Join(degradedPools, ", "))
		failed++
	}

	return passed, failed
}

// writePrerequisitesSummary writes the overall pass/fail/warning summary and
// recommendation for the prerequisites report.
func writePrerequisitesSummary(sb *strings.Builder, passed, failed, warnings int) {
	sb.WriteString("\n## Summary\n\n")
	_, _ = fmt.Fprintf(sb, "- **Passed:** %d\n", passed)
	_, _ = fmt.Fprintf(sb, "- **Failed:** %d\n", failed)
	_, _ = fmt.Fprintf(sb, "- **Warnings:** %d\n\n", warnings)

	if failed > 0 {
		sb.WriteString("**Recommendation:** Fix the failed checks before proceeding with the upgrade.\n")
	} else if warnings > 0 {
		sb.WriteString("**Recommendation:** Review warnings before proceeding. The upgrade can proceed but may encounter issues.\n")
	} else {
		sb.WriteString("**Recommendation:** All prerequisites passed. The cluster is ready for upgrade.\n")
	}
}
