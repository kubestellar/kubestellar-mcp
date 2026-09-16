package upgrades

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DetectClusterType detects the Kubernetes distribution type.
func DetectClusterType(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
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
	sb.WriteString("# Cluster Type Detection\n\n")

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Sprintf("Failed to get server version: %v", err), true
	}
	_, _ = fmt.Fprintf(&sb, "**Kubernetes Version:** %s\n", version.GitVersion)

	// Check for OpenShift first (ClusterVersion CRD)
	_, err = dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err == nil {
		_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeOpenShift)
		sb.WriteString("**Detection Method:** ClusterVersion CRD found (config.openshift.io/v1)\n")
		return sb.String(), false
	}

	// Get nodes to check labels
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeUnknown)
		_, _ = fmt.Fprintf(&sb, "**Note:** Unable to list nodes: %v\n", err)
		return sb.String(), false
	}

	if len(nodes.Items) == 0 {
		_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeUnknown)
		sb.WriteString("**Note:** No nodes found\n")
		return sb.String(), false
	}

	node := nodes.Items[0]
	labels := node.Labels
	annotations := node.Annotations
	providerID := node.Spec.ProviderID

	// Check for EKS
	if strings.Contains(providerID, "aws") {
		for label := range labels {
			if strings.Contains(label, "eks.amazonaws.com") {
				_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeEKS)
				sb.WriteString("**Detection Method:** Node labels contain eks.amazonaws.com\n")
				_, _ = fmt.Fprintf(&sb, "**Provider ID:** %s\n", providerID)
				return sb.String(), false
			}
		}
	}

	// Check for GKE
	if strings.Contains(providerID, "gce") {
		for label := range labels {
			if strings.Contains(label, "cloud.google.com/gke") {
				_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeGKE)
				sb.WriteString("**Detection Method:** Node labels contain cloud.google.com/gke\n")
				_, _ = fmt.Fprintf(&sb, "**Provider ID:** %s\n", providerID)
				return sb.String(), false
			}
		}
		_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeGKE)
		sb.WriteString("**Detection Method:** Provider ID contains gce://\n")
		_, _ = fmt.Fprintf(&sb, "**Provider ID:** %s\n", providerID)
		return sb.String(), false
	}

	// Check for AKS
	if strings.Contains(providerID, "azure") {
		for label := range labels {
			if strings.Contains(label, "kubernetes.azure.com") {
				_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeAKS)
				sb.WriteString("**Detection Method:** Node labels contain kubernetes.azure.com\n")
				_, _ = fmt.Fprintf(&sb, "**Provider ID:** %s\n", providerID)
				return sb.String(), false
			}
		}
	}

	// Check for kind
	for label := range labels {
		if strings.Contains(label, "io.x-k8s.kind") {
			_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeKind)
			sb.WriteString("**Detection Method:** Node labels contain io.x-k8s.kind\n")
			return sb.String(), false
		}
	}

	// Check for minikube
	for label := range labels {
		if strings.Contains(label, "minikube.k8s.io") {
			_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeMinikube)
			sb.WriteString("**Detection Method:** Node labels contain minikube.k8s.io\n")
			return sb.String(), false
		}
	}

	// Check for k3s
	if strings.Contains(version.GitVersion, "k3s") {
		_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeK3s)
		sb.WriteString("**Detection Method:** Server version contains k3s\n")
		return sb.String(), false
	}

	// Check for kubeadm
	for key := range annotations {
		if strings.Contains(key, "kubeadm") {
			_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeKubeadm)
			sb.WriteString("**Detection Method:** Node annotations contain kubeadm\n")
			return sb.String(), false
		}
	}

	// Default to unknown
	_, _ = fmt.Fprintf(&sb, "**Cluster Type:** %s\n", ClusterTypeUnknown)
	sb.WriteString("**Detection Method:** No specific distribution markers found\n")
	sb.WriteString("**Note:** This appears to be a vanilla Kubernetes cluster\n")

	return sb.String(), false
}
