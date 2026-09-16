package upgrades

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetClusterVersionInfo gets current cluster version and available upgrades.
func GetClusterVersionInfo(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
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
	sb.WriteString("# Cluster Version Information\n\n")

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Sprintf("Failed to get server version: %v", err), true
	}

	// Check if OpenShift
	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err == nil {
		return getOpenShiftVersionInfo(ctx, cv, &sb)
	}

	// Vanilla Kubernetes
	sb.WriteString("**Cluster Type:** Kubernetes\n")
	_, _ = fmt.Fprintf(&sb, "**Current Version:** %s\n", version.GitVersion)
	_, _ = fmt.Fprintf(&sb, "**Platform:** %s\n", version.Platform)
	_, _ = fmt.Fprintf(&sb, "**Build Date:** %s\n\n", version.BuildDate)

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil && len(nodes.Items) > 0 {
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
			_, _ = fmt.Fprintf(&sb, "| %s | %s | %s |\n",
				node.Name,
				node.Status.NodeInfo.KubeletVersion,
				status)
		}
	}

	sb.WriteString("\n## Upgrade Information\n\n")
	sb.WriteString("For vanilla Kubernetes clusters, upgrade paths depend on your installation method:\n\n")
	sb.WriteString("- **kubeadm**: Use `kubeadm upgrade plan` to see available versions\n")
	sb.WriteString("- **EKS**: Check AWS Console or use `aws eks describe-addon-versions`\n")
	sb.WriteString("- **GKE**: Check Google Cloud Console or use `gcloud container get-server-config`\n")
	sb.WriteString("- **AKS**: Check Azure Portal or use `az aks get-upgrades`\n")

	return sb.String(), false
}

func getOpenShiftVersionInfo(_ context.Context, cv *unstructured.Unstructured, sb *strings.Builder) (string, bool) {
	sb.WriteString("**Cluster Type:** OpenShift\n")

	desiredVersion, _, _ := unstructured.NestedString(cv.Object, "status", "desired", "version")
	_, _ = fmt.Fprintf(sb, "**Current Version:** %s\n", desiredVersion)

	channel, _, _ := unstructured.NestedString(cv.Object, "spec", "channel")
	_, _ = fmt.Fprintf(sb, "**Update Channel:** %s\n", channel)

	clusterID, _, _ := unstructured.NestedString(cv.Object, "spec", "clusterID")
	if clusterID != "" {
		_, _ = fmt.Fprintf(sb, "**Cluster ID:** %s\n", clusterID)
	}

	conditions, _, _ := unstructured.NestedSlice(cv.Object, "status", "conditions")
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		if condType == "Progressing" && condStatus == "True" {
			message, _, _ := unstructured.NestedString(condMap, "message")
			sb.WriteString("\n**Upgrade Status:** In Progress\n")
			_, _ = fmt.Fprintf(sb, "**Progress:** %s\n", message)
		}
	}

	availableUpdates, _, _ := unstructured.NestedSlice(cv.Object, "status", "availableUpdates")
	if len(availableUpdates) > 0 {
		sb.WriteString("\n## Available Updates\n\n")
		sb.WriteString("| Version | Image |\n")
		sb.WriteString("|---------|-------|\n")

		for _, update := range availableUpdates {
			updateMap, ok := update.(map[string]interface{})
			if !ok {
				continue
			}
			ver, _, _ := unstructured.NestedString(updateMap, "version")
			image, _, _ := unstructured.NestedString(updateMap, "image")
			if len(image) > 60 {
				image = image[:57] + "..."
			}
			_, _ = fmt.Fprintf(sb, "| %s | %s |\n", ver, image)
		}
	} else {
		sb.WriteString("\n**Available Updates:** None (cluster is at latest version for this channel)\n")
	}

	history, _, _ := unstructured.NestedSlice(cv.Object, "status", "history")
	if len(history) > 0 {
		sb.WriteString("\n## Upgrade History\n\n")
		sb.WriteString("| Version | State | Completion Time |\n")
		sb.WriteString("|---------|-------|------------------|\n")

		limit := 5
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
			completionTime, _, _ := unstructured.NestedString(entry, "completionTime")
			if completionTime == "" {
				completionTime = "In progress"
			}
			_, _ = fmt.Fprintf(sb, "| %s | %s | %s |\n", ver, state, completionTime)
		}
	}

	return sb.String(), false
}
