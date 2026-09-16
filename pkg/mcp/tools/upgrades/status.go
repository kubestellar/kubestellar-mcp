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

// TriggerOpenShiftUpgrade triggers an OpenShift cluster upgrade.
func TriggerOpenShiftUpgrade(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	targetVersion, _ := args["target_version"].(string)
	confirm, _ := args["confirm"].(string)

	if targetVersion == "" {
		return "target_version is required", true
	}

	if confirm != "yes-upgrade-now" {
		var sb strings.Builder
		sb.WriteString("# Safety Check Failed\n\n")
		sb.WriteString("**IMPORTANT:** Cluster upgrades are significant operations that will:\n")
		sb.WriteString("- Temporarily make the API server unavailable\n")
		sb.WriteString("- Rolling restart all nodes\n")
		sb.WriteString("- Potentially impact running workloads\n\n")
		sb.WriteString("To proceed with the upgrade, you must pass `confirm='yes-upgrade-now'`\n\n")
		sb.WriteString("**Before confirming:**\n")
		sb.WriteString("1. Run `get_upgrade_prerequisites` to verify cluster readiness\n")
		sb.WriteString("2. Ensure you have recent etcd backups\n")
		sb.WriteString("3. Notify relevant teams about the maintenance window\n")
		sb.WriteString("4. Verify the target version is in the available updates list\n")
		return sb.String(), false
	}

	dynClient, err := ca.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get ClusterVersion: %v\nThis does not appear to be an OpenShift cluster.", err), true
	}

	availableUpdates, _, _ := unstructured.NestedSlice(cv.Object, "status", "availableUpdates")
	validVersion := false
	for _, update := range availableUpdates {
		updateMap, ok := update.(map[string]interface{})
		if !ok {
			continue
		}
		ver, _, _ := unstructured.NestedString(updateMap, "version")
		if ver == targetVersion {
			validVersion = true
			break
		}
	}

	if !validVersion {
		var sb strings.Builder
		sb.WriteString("# Invalid Target Version\n\n")
		_, _ = fmt.Fprintf(&sb, "Version `%s` is not in the list of available updates.\n\n", targetVersion)
		sb.WriteString("**Available versions:**\n")
		for _, update := range availableUpdates {
			updateMap, ok := update.(map[string]interface{})
			if !ok {
				continue
			}
			ver, _, _ := unstructured.NestedString(updateMap, "version")
			_, _ = fmt.Fprintf(&sb, "- %s\n", ver)
		}
		if len(availableUpdates) == 0 {
			sb.WriteString("- (none available - cluster may be at latest version)\n")
		}
		return sb.String(), false
	}

	err = unstructured.SetNestedField(cv.Object, targetVersion, "spec", "desiredUpdate", "version")
	if err != nil {
		return fmt.Sprintf("Failed to set desired version: %v", err), true
	}

	_, err = dynClient.Resource(clusterVersionGVR).Update(ctx, cv, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to trigger upgrade: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Upgrade Initiated\n\n")
	_, _ = fmt.Fprintf(&sb, "**Target Version:** %s\n", targetVersion)
	sb.WriteString("**Status:** Upgrade has been triggered\n\n")
	sb.WriteString("The cluster will now begin the upgrade process. This typically takes:\n")
	sb.WriteString("- 30-60 minutes for control plane\n")
	sb.WriteString("- Additional time for worker nodes (depends on node count)\n\n")
	sb.WriteString("**Monitor progress with:**\n")
	sb.WriteString("- `get_upgrade_status` - Check overall progress\n")
	sb.WriteString("- `get_cluster_health` - Monitor cluster health\n")
	sb.WriteString("- `find_pod_issues` - Check for pod problems during upgrade\n\n")
	sb.WriteString("**Important:** Do not make additional changes to the cluster during the upgrade.\n")

	return sb.String(), false
}

// GetUpgradeStatus monitors upgrade progress.
func GetUpgradeStatus(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
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
	sb.WriteString("# Upgrade Status\n\n")

	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return writeKubernetesUpgradeStatus(ctx, client, &sb)
	}

	writeOpenShiftUpgradeStatus(ctx, dynClient, cv, &sb)

	return sb.String(), false
}

// writeKubernetesUpgradeStatus writes upgrade status for a non-OpenShift
// (vanilla Kubernetes) cluster based on node kubelet versions.
func writeKubernetesUpgradeStatus(ctx context.Context, client kubernetes.Interface, sb *strings.Builder) (string, bool) {
	sb.WriteString("**Cluster Type:** Kubernetes\n\n")

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list nodes: %v", err), true
	}

	sb.WriteString("## Node Versions\n\n")
	sb.WriteString("| Node | Kubelet Version | Status |\n")
	sb.WriteString("|------|-----------------|--------|\n")

	for _, node := range nodes.Items {
		status := "NotReady"
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				status = "Ready"
				break
			}
		}
		_, _ = fmt.Fprintf(sb, "| %s | %s | %s |\n",
			node.Name,
			node.Status.NodeInfo.KubeletVersion,
			status)
	}

	sb.WriteString("\n**Note:** For non-OpenShift clusters, detailed upgrade progress tracking\n")
	sb.WriteString("depends on your installation method (kubeadm, EKS, GKE, AKS, etc.)\n")
	return sb.String(), false
}

// writeOpenShiftUpgradeStatus writes upgrade status for an OpenShift cluster,
// including ClusterVersion progress, ClusterOperators, MachineConfigPools,
// and recent upgrade history.
func writeOpenShiftUpgradeStatus(ctx context.Context, dynClient dynamic.Interface, cv *unstructured.Unstructured, sb *strings.Builder) {
	sb.WriteString("**Cluster Type:** OpenShift\n\n")

	desiredVersion, _, _ := unstructured.NestedString(cv.Object, "status", "desired", "version")
	_, _ = fmt.Fprintf(sb, "**Target Version:** %s\n", desiredVersion)

	writeClusterVersionProgress(cv, sb)
	writeClusterOperatorStatusTable(ctx, dynClient, sb)
	writeMachineConfigPoolStatusTable(ctx, dynClient, sb)
	writeRecentUpgradeHistory(cv, sb)
}

// writeClusterVersionProgress reports whether the ClusterVersion is currently
// progressing through an upgrade.
func writeClusterVersionProgress(cv *unstructured.Unstructured, sb *strings.Builder) {
	conditions, _, _ := unstructured.NestedSlice(cv.Object, "status", "conditions")
	isProgressing := false
	progressMessage := ""

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		message, _, _ := unstructured.NestedString(condMap, "message")

		if condType == "Progressing" {
			if condStatus == "True" {
				isProgressing = true
				progressMessage = message
			}
		}
	}

	if isProgressing {
		sb.WriteString("**Status:** Upgrade in progress\n")
		_, _ = fmt.Fprintf(sb, "**Progress:** %s\n\n", progressMessage)
	} else {
		sb.WriteString("**Status:** Not currently upgrading\n\n")
	}
}

// writeClusterOperatorStatusTable renders a table of ClusterOperator
// availability, progressing, and degraded conditions.
func writeClusterOperatorStatusTable(ctx context.Context, dynClient dynamic.Interface, sb *strings.Builder) {
	cos, err := dynClient.Resource(clusterOperatorGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	sb.WriteString("## ClusterOperator Status\n\n")
	sb.WriteString("| Operator | Available | Progressing | Degraded |\n")
	sb.WriteString("|----------|-----------|-------------|----------|\n")

	for _, co := range cos.Items {
		available := "-"
		progressing := "-"
		degraded := "-"

		conditions, _, _ := unstructured.NestedSlice(co.Object, "status", "conditions")
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")

			switch condType {
			case "Available":
				available = condStatus
			case "Progressing":
				progressing = condStatus
			case "Degraded":
				degraded = condStatus
			}
		}

		_, _ = fmt.Fprintf(sb, "| %s | %s | %s | %s |\n",
			co.GetName(), available, progressing, degraded)
	}
}

// writeMachineConfigPoolStatusTable renders a table of MachineConfigPool
// rollout status.
func writeMachineConfigPoolStatusTable(ctx context.Context, dynClient dynamic.Interface, sb *strings.Builder) {
	mcps, err := dynClient.Resource(machineConfigPoolGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	sb.WriteString("\n## MachineConfigPool Status\n\n")
	sb.WriteString("| Pool | Ready | Updated | Updating | Degraded |\n")
	sb.WriteString("|------|-------|---------|----------|----------|\n")

	for _, mcp := range mcps.Items {
		status, _, _ := unstructured.NestedMap(mcp.Object, "status")
		machineCount, _, _ := unstructured.NestedInt64(status, "machineCount")
		readyCount, _, _ := unstructured.NestedInt64(status, "readyMachineCount")
		updatedCount, _, _ := unstructured.NestedInt64(status, "updatedMachineCount")

		updating := "False"
		degraded := "False"

		conditions, _, _ := unstructured.NestedSlice(mcp.Object, "status", "conditions")
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")

			if condType == "Updating" {
				updating = condStatus
			}
			if condType == "Degraded" {
				degraded = condStatus
			}
		}

		_, _ = fmt.Fprintf(sb, "| %s | %d/%d | %d/%d | %s | %s |\n",
			mcp.GetName(), readyCount, machineCount, updatedCount, machineCount, updating, degraded)
	}
}

// writeRecentUpgradeHistory renders the most recent ClusterVersion history
// entries.
func writeRecentUpgradeHistory(cv *unstructured.Unstructured, sb *strings.Builder) {
	history, _, _ := unstructured.NestedSlice(cv.Object, "status", "history")
	if len(history) == 0 {
		return
	}

	sb.WriteString("\n## Recent History\n\n")
	sb.WriteString("| Version | State | Started | Completed |\n")
	sb.WriteString("|---------|-------|---------|------------|\n")

	limit := 3
	if len(history) < limit {
		limit = len(history)
	}
	for i := 0; i < limit; i++ {
		entry, ok := history[i].(map[string]interface{})
		if !ok {
			continue
		}
		ver, _, _ := unstructured.NestedString(entry, "version")
		state, _, _ := unstructured.NestedString(entry, "state")
		startTime, _, _ := unstructured.NestedString(entry, "startedTime")
		completionTime, _, _ := unstructured.NestedString(entry, "completionTime")

		if completionTime == "" {
			completionTime = "In progress"
		}

		_, _ = fmt.Fprintf(sb, "| %s | %s | %s | %s |\n", ver, state, startTime, completionTime)
	}
}
