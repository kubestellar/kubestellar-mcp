package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Cluster type constants
const (
	ClusterTypeOpenShift = "openshift"
	ClusterTypeEKS       = "eks"
	ClusterTypeGKE       = "gke"
	ClusterTypeAKS       = "aks"
	ClusterTypeKubeadm   = "kubeadm"
	ClusterTypeK3s       = "k3s"
	ClusterTypeKind      = "kind"
	ClusterTypeMinikube  = "minikube"
	ClusterTypeUnknown   = "unknown"
)

// GVRs for upgrade-related CRDs
var (
	clusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}
	clusterOperatorGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}
	subscriptionGVR = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}
	csvGVR = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}
	machineConfigPoolGVR = schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}
)

// toolDetectClusterType detects the Kubernetes distribution type
func (s *Server) toolDetectClusterType(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Cluster Type Detection\n\n")

	// Get server version
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Sprintf("Failed to get server version: %v", err), true
	}
	sb.WriteString(fmt.Sprintf("**Kubernetes Version:** %s\n", version.GitVersion))

	// Check for OpenShift first (ClusterVersion CRD)
	_, err = dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err == nil {
		sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeOpenShift))
		sb.WriteString("**Detection Method:** ClusterVersion CRD found (config.openshift.io/v1)\n")
		return sb.String(), false
	}

	// Get nodes to check labels
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeUnknown))
		sb.WriteString(fmt.Sprintf("**Note:** Unable to list nodes: %v\n", err))
		return sb.String(), false
	}

	if len(nodes.Items) == 0 {
		sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeUnknown))
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
				sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeEKS))
				sb.WriteString("**Detection Method:** Node labels contain eks.amazonaws.com\n")
				sb.WriteString(fmt.Sprintf("**Provider ID:** %s\n", providerID))
				return sb.String(), false
			}
		}
	}

	// Check for GKE
	if strings.Contains(providerID, "gce") {
		for label := range labels {
			if strings.Contains(label, "cloud.google.com/gke") {
				sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeGKE))
				sb.WriteString("**Detection Method:** Node labels contain cloud.google.com/gke\n")
				sb.WriteString(fmt.Sprintf("**Provider ID:** %s\n", providerID))
				return sb.String(), false
			}
		}
		// GKE might not have specific labels but has gce provider
		sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeGKE))
		sb.WriteString("**Detection Method:** Provider ID contains gce://\n")
		sb.WriteString(fmt.Sprintf("**Provider ID:** %s\n", providerID))
		return sb.String(), false
	}

	// Check for AKS
	if strings.Contains(providerID, "azure") {
		for label := range labels {
			if strings.Contains(label, "kubernetes.azure.com") {
				sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeAKS))
				sb.WriteString("**Detection Method:** Node labels contain kubernetes.azure.com\n")
				sb.WriteString(fmt.Sprintf("**Provider ID:** %s\n", providerID))
				return sb.String(), false
			}
		}
	}

	// Check for kind
	for label := range labels {
		if strings.Contains(label, "io.x-k8s.kind") {
			sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeKind))
			sb.WriteString("**Detection Method:** Node labels contain io.x-k8s.kind\n")
			return sb.String(), false
		}
	}

	// Check for minikube
	for label := range labels {
		if strings.Contains(label, "minikube.k8s.io") {
			sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeMinikube))
			sb.WriteString("**Detection Method:** Node labels contain minikube.k8s.io\n")
			return sb.String(), false
		}
	}

	// Check for k3s
	if strings.Contains(version.GitVersion, "k3s") {
		sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeK3s))
		sb.WriteString("**Detection Method:** Server version contains k3s\n")
		return sb.String(), false
	}

	// Check for kubeadm
	if annotations != nil {
		for key := range annotations {
			if strings.Contains(key, "kubeadm") {
				sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeKubeadm))
				sb.WriteString("**Detection Method:** Node annotations contain kubeadm\n")
				return sb.String(), false
			}
		}
	}

	// Default to unknown
	sb.WriteString(fmt.Sprintf("**Cluster Type:** %s\n", ClusterTypeUnknown))
	sb.WriteString("**Detection Method:** No specific distribution markers found\n")
	sb.WriteString("**Note:** This appears to be a vanilla Kubernetes cluster\n")

	return sb.String(), false
}

// toolGetClusterVersionInfo gets current cluster version and available upgrades
func (s *Server) toolGetClusterVersionInfo(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Cluster Version Information\n\n")

	// Get server version
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Sprintf("Failed to get server version: %v", err), true
	}

	// Check if OpenShift
	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err == nil {
		// OpenShift cluster
		return s.getOpenShiftVersionInfo(ctx, cv, &sb)
	}

	// Vanilla Kubernetes
	sb.WriteString("**Cluster Type:** Kubernetes\n")
	sb.WriteString(fmt.Sprintf("**Current Version:** %s\n", version.GitVersion))
	sb.WriteString(fmt.Sprintf("**Platform:** %s\n", version.Platform))
	sb.WriteString(fmt.Sprintf("**Build Date:** %s\n\n", version.BuildDate))

	// Check node versions
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
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
				node.Name,
				node.Status.NodeInfo.KubeletVersion,
				status))
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

func (s *Server) getOpenShiftVersionInfo(ctx context.Context, cv *unstructured.Unstructured, sb *strings.Builder) (string, bool) {
	sb.WriteString("**Cluster Type:** OpenShift\n")

	// Get current version
	desiredVersion, _, _ := unstructured.NestedString(cv.Object, "status", "desired", "version")
	sb.WriteString(fmt.Sprintf("**Current Version:** %s\n", desiredVersion))

	// Get channel
	channel, _, _ := unstructured.NestedString(cv.Object, "spec", "channel")
	sb.WriteString(fmt.Sprintf("**Update Channel:** %s\n", channel))

	// Get cluster ID
	clusterID, _, _ := unstructured.NestedString(cv.Object, "spec", "clusterID")
	if clusterID != "" {
		sb.WriteString(fmt.Sprintf("**Cluster ID:** %s\n", clusterID))
	}

	// Get upgrade status
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
			sb.WriteString(fmt.Sprintf("\n**Upgrade Status:** In Progress\n"))
			sb.WriteString(fmt.Sprintf("**Progress:** %s\n", message))
		}
	}

	// Get available updates
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
			// Truncate image for display
			if len(image) > 60 {
				image = image[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("| %s | %s |\n", ver, image))
		}
	} else {
		sb.WriteString("\n**Available Updates:** None (cluster is at latest version for this channel)\n")
	}

	// Get upgrade history
	history, _, _ := unstructured.NestedSlice(cv.Object, "status", "history")
	if len(history) > 0 {
		sb.WriteString("\n## Upgrade History\n\n")
		sb.WriteString("| Version | State | Completion Time |\n")
		sb.WriteString("|---------|-------|------------------|\n")

		// Show last 5 entries
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
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", ver, state, completionTime))
		}
	}

	return sb.String(), false
}

// toolCheckOLMOperatorUpgrades checks OLM operators for available upgrades
func (s *Server) toolCheckOLMOperatorUpgrades(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# OLM Operator Upgrades\n\n")

	// List subscriptions
	var subscriptions *unstructured.UnstructuredList
	if namespace == "" {
		subscriptions, err = dynClient.Resource(subscriptionGVR).Namespace("").List(ctx, metav1.ListOptions{})
	} else {
		subscriptions, err = dynClient.Resource(subscriptionGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		if strings.Contains(err.Error(), "could not find the requested resource") ||
			strings.Contains(err.Error(), "no matches for kind") {
			sb.WriteString("**OLM Status:** Not installed\n\n")
			sb.WriteString("Operator Lifecycle Manager (OLM) is not installed on this cluster.\n")
			sb.WriteString("OLM is required for managing operators through subscriptions.\n\n")
			sb.WriteString("To install OLM, visit: https://olm.operatorframework.io/docs/getting-started/\n")
			return sb.String(), false
		}
		return fmt.Sprintf("Failed to list subscriptions: %v", err), true
	}

	if len(subscriptions.Items) == 0 {
		sb.WriteString("**OLM Status:** Installed\n")
		sb.WriteString("**Subscriptions Found:** 0\n\n")
		sb.WriteString("No operator subscriptions found.\n")
		return sb.String(), false
	}

	sb.WriteString("**OLM Status:** Installed\n")
	sb.WriteString(fmt.Sprintf("**Subscriptions Found:** %d\n\n", len(subscriptions.Items)))

	sb.WriteString("| Operator | Namespace | Current CSV | Channel | Auto-Update | Status |\n")
	sb.WriteString("|----------|-----------|-------------|---------|-------------|--------|\n")

	upgradesPending := 0
	for _, sub := range subscriptions.Items {
		name := sub.GetName()
		ns := sub.GetNamespace()

		spec, _, _ := unstructured.NestedMap(sub.Object, "spec")
		channel, _, _ := unstructured.NestedString(spec, "channel")
		installPlanApproval, _, _ := unstructured.NestedString(spec, "installPlanApproval")
		autoUpdate := installPlanApproval == "Automatic"

		status, _, _ := unstructured.NestedMap(sub.Object, "status")
		currentCSV, _, _ := unstructured.NestedString(status, "currentCSV")
		state, _, _ := unstructured.NestedString(status, "state")

		statusEmoji := ""
		switch state {
		case "AtLatestKnown":
			statusEmoji = "Up to date"
		case "UpgradePending":
			statusEmoji = "Upgrade pending"
			upgradesPending++
		case "UpgradeAvailable":
			statusEmoji = "Upgrade available"
			upgradesPending++
		default:
			statusEmoji = state
		}

		autoUpdateStr := "Manual"
		if autoUpdate {
			autoUpdateStr = "Auto"
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			name, ns, currentCSV, channel, autoUpdateStr, statusEmoji))
	}

	sb.WriteString("\n")
	if upgradesPending > 0 {
		sb.WriteString(fmt.Sprintf("**Upgrades Available:** %d operator(s) have pending upgrades\n", upgradesPending))
	} else {
		sb.WriteString("**Upgrades Available:** All operators are at their latest known version\n")
	}

	return sb.String(), false
}

// HelmRelease represents a decoded Helm release
type HelmRelease struct {
	Name      string
	Namespace string
	Chart     string
	Version   string
	AppVer    string
	Status    string
	Revision  int
}

// toolCheckHelmReleaseUpgrades checks Helm releases in the cluster
func (s *Server) toolCheckHelmReleaseUpgrades(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Helm Releases\n\n")

	// List secrets with owner=helm label
	labelSelector := "owner=helm"
	var secrets *corev1.SecretList
	if namespace == "" {
		secrets, err = client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	} else {
		secrets, err = client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list Helm secrets: %v", err), true
	}

	if len(secrets.Items) == 0 {
		sb.WriteString("**Helm Releases Found:** 0\n\n")
		sb.WriteString("No Helm releases found in the cluster.\n")
		return sb.String(), false
	}

	// Parse releases - group by release name and get latest revision
	releases := make(map[string]HelmRelease)
	for _, secret := range secrets.Items {
		release := s.parseHelmSecret(&secret)
		if release == nil {
			continue
		}

		key := release.Namespace + "/" + release.Name
		existing, found := releases[key]
		if !found || release.Revision > existing.Revision {
			releases[key] = *release
		}
	}

	sb.WriteString(fmt.Sprintf("**Helm Releases Found:** %d\n\n", len(releases)))

	sb.WriteString("| Release | Namespace | Chart | Version | App Version | Status |\n")
	sb.WriteString("|---------|-----------|-------|---------|-------------|--------|\n")

	for _, rel := range releases {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			rel.Name, rel.Namespace, rel.Chart, rel.Version, rel.AppVer, rel.Status))
	}

	sb.WriteString("\n## Checking for Updates\n\n")
	sb.WriteString("To check for available chart updates, you need to:\n\n")
	sb.WriteString("1. Ensure Helm repos are added: `helm repo list`\n")
	sb.WriteString("2. Update repos: `helm repo update`\n")
	sb.WriteString("3. Search for updates: `helm search repo <chart-name>`\n\n")
	sb.WriteString("**Note:** This tool shows currently deployed releases. Checking for newer chart versions\n")
	sb.WriteString("requires access to Helm repositories which are typically configured on the client side.\n")

	return sb.String(), false
}

func (s *Server) parseHelmSecret(secret *corev1.Secret) *HelmRelease {
	if secret.Type != "helm.sh/release.v1" {
		return nil
	}

	releaseData, ok := secret.Data["release"]
	if !ok {
		return nil
	}

	// Decode base64 and decompress
	decoded, err := base64.StdEncoding.DecodeString(string(releaseData))
	if err != nil {
		// Try without base64 (some versions store raw gzipped data)
		decoded = releaseData
	}

	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil
	}

	var releaseObj map[string]interface{}
	if err := json.Unmarshal(decompressed, &releaseObj); err != nil {
		return nil
	}

	release := &HelmRelease{
		Namespace: secret.Namespace,
	}

	if name, ok := releaseObj["name"].(string); ok {
		release.Name = name
	}

	if info, ok := releaseObj["info"].(map[string]interface{}); ok {
		if status, ok := info["status"].(string); ok {
			release.Status = status
		}
	}

	if chart, ok := releaseObj["chart"].(map[string]interface{}); ok {
		if metadata, ok := chart["metadata"].(map[string]interface{}); ok {
			if name, ok := metadata["name"].(string); ok {
				release.Chart = name
			}
			if ver, ok := metadata["version"].(string); ok {
				release.Version = ver
			}
			if appVer, ok := metadata["appVersion"].(string); ok {
				release.AppVer = appVer
			}
		}
	}

	if version, ok := releaseObj["version"].(float64); ok {
		release.Revision = int(version)
	}

	return release
}

// toolGetUpgradePrerequisites checks prerequisites before upgrading
func (s *Server) toolGetUpgradePrerequisites(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Upgrade Prerequisites Check\n\n")

	passed := 0
	failed := 0
	warnings := 0

	// Check 1: All nodes ready
	sb.WriteString("## Node Health\n\n")
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		sb.WriteString(fmt.Sprintf("- [ ] Unable to check nodes: %v\n", err))
		failed++
	} else {
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
			sb.WriteString(fmt.Sprintf("- [x] All nodes ready (%d/%d)\n", readyNodes, len(nodes.Items)))
			passed++
		} else {
			sb.WriteString(fmt.Sprintf("- [ ] Some nodes not ready (%d/%d)\n", readyNodes, len(nodes.Items)))
			sb.WriteString(fmt.Sprintf("  - Not ready: %s\n", strings.Join(notReadyNodes, ", ")))
			failed++
		}
	}

	// Check 2: No pods in CrashLoopBackOff or ImagePullBackOff
	sb.WriteString("\n## Pod Health\n\n")
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		sb.WriteString(fmt.Sprintf("- [ ] Unable to check pods: %v\n", err))
		failed++
	} else {
		crashingPods := []string{}
		imagePullPods := []string{}
		pendingPods := []string{}

		for _, pod := range pods.Items {
			// Skip completed pods
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}

			if pod.Status.Phase == corev1.PodPending {
				pendingPods = append(pendingPods, pod.Namespace+"/"+pod.Name)
			}

			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					reason := cs.State.Waiting.Reason
					if reason == "CrashLoopBackOff" {
						crashingPods = append(crashingPods, pod.Namespace+"/"+pod.Name)
					} else if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
						imagePullPods = append(imagePullPods, pod.Namespace+"/"+pod.Name)
					}
				}
			}
		}

		if len(crashingPods) == 0 {
			sb.WriteString("- [x] No pods in CrashLoopBackOff\n")
			passed++
		} else {
			sb.WriteString(fmt.Sprintf("- [ ] %d pods in CrashLoopBackOff\n", len(crashingPods)))
			for _, p := range crashingPods[:min(5, len(crashingPods))] {
				sb.WriteString(fmt.Sprintf("  - %s\n", p))
			}
			if len(crashingPods) > 5 {
				sb.WriteString(fmt.Sprintf("  - ... and %d more\n", len(crashingPods)-5))
			}
			failed++
		}

		if len(imagePullPods) == 0 {
			sb.WriteString("- [x] No pods with image pull errors\n")
			passed++
		} else {
			sb.WriteString(fmt.Sprintf("- [ ] %d pods with image pull errors\n", len(imagePullPods)))
			for _, p := range imagePullPods[:min(5, len(imagePullPods))] {
				sb.WriteString(fmt.Sprintf("  - %s\n", p))
			}
			warnings++
		}

		if len(pendingPods) <= 5 {
			sb.WriteString(fmt.Sprintf("- [x] Few pending pods (%d)\n", len(pendingPods)))
			passed++
		} else {
			sb.WriteString(fmt.Sprintf("- [ ] Many pending pods (%d)\n", len(pendingPods)))
			warnings++
		}
	}

	// Check 3: OpenShift-specific checks
	_, err = dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err == nil {
		sb.WriteString("\n## OpenShift-Specific Checks\n\n")

		// Check ClusterOperators
		cos, err := dynClient.Resource(clusterOperatorGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			sb.WriteString(fmt.Sprintf("- [ ] Unable to check ClusterOperators: %v\n", err))
			failed++
		} else {
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
				sb.WriteString(fmt.Sprintf("- [ ] %d degraded ClusterOperators: %s\n", len(degradedOps), strings.Join(degradedOps, ", ")))
				failed++
			}

			if len(unavailableOps) == 0 {
				sb.WriteString("- [x] All ClusterOperators available\n")
				passed++
			} else {
				sb.WriteString(fmt.Sprintf("- [ ] %d unavailable ClusterOperators: %s\n", len(unavailableOps), strings.Join(unavailableOps, ", ")))
				failed++
			}

			if len(progressingOps) == 0 {
				sb.WriteString("- [x] No ClusterOperators progressing\n")
				passed++
			} else {
				sb.WriteString(fmt.Sprintf("- [ ] %d ClusterOperators progressing: %s\n", len(progressingOps), strings.Join(progressingOps, ", ")))
				warnings++
			}
		}

		// Check MachineConfigPools
		mcps, err := dynClient.Resource(machineConfigPoolGVR).List(ctx, metav1.ListOptions{})
		if err == nil {
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
				sb.WriteString(fmt.Sprintf("- [ ] %d MachineConfigPools updating: %s\n", len(updatingPools), strings.Join(updatingPools, ", ")))
				sb.WriteString("  - Wait for current updates to complete before upgrading\n")
				failed++
			}

			if len(degradedPools) == 0 {
				sb.WriteString("- [x] No MachineConfigPools degraded\n")
				passed++
			} else {
				sb.WriteString(fmt.Sprintf("- [ ] %d MachineConfigPools degraded: %s\n", len(degradedPools), strings.Join(degradedPools, ", ")))
				failed++
			}
		}
	}

	// Summary
	sb.WriteString("\n## Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **Passed:** %d\n", passed))
	sb.WriteString(fmt.Sprintf("- **Failed:** %d\n", failed))
	sb.WriteString(fmt.Sprintf("- **Warnings:** %d\n\n", warnings))

	if failed > 0 {
		sb.WriteString("**Recommendation:** Fix the failed checks before proceeding with the upgrade.\n")
	} else if warnings > 0 {
		sb.WriteString("**Recommendation:** Review warnings before proceeding. The upgrade can proceed but may encounter issues.\n")
	} else {
		sb.WriteString("**Recommendation:** All prerequisites passed. The cluster is ready for upgrade.\n")
	}

	return sb.String(), false
}

// toolTriggerOpenShiftUpgrade triggers an OpenShift cluster upgrade
func (s *Server) toolTriggerOpenShiftUpgrade(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	targetVersion, _ := args["target_version"].(string)
	confirm, _ := args["confirm"].(string)

	if targetVersion == "" {
		return "target_version is required", true
	}

	// Safety check - require explicit confirmation
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

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	// Get current ClusterVersion
	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get ClusterVersion: %v\nThis does not appear to be an OpenShift cluster.", err), true
	}

	// Validate target version is in availableUpdates
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
		sb.WriteString(fmt.Sprintf("Version `%s` is not in the list of available updates.\n\n", targetVersion))
		sb.WriteString("**Available versions:**\n")
		for _, update := range availableUpdates {
			updateMap, ok := update.(map[string]interface{})
			if !ok {
				continue
			}
			ver, _, _ := unstructured.NestedString(updateMap, "version")
			sb.WriteString(fmt.Sprintf("- %s\n", ver))
		}
		if len(availableUpdates) == 0 {
			sb.WriteString("- (none available - cluster may be at latest version)\n")
		}
		return sb.String(), false
	}

	// Set the desired update
	err = unstructured.SetNestedField(cv.Object, targetVersion, "spec", "desiredUpdate", "version")
	if err != nil {
		return fmt.Sprintf("Failed to set desired version: %v", err), true
	}

	// Apply the update
	_, err = dynClient.Resource(clusterVersionGVR).Update(ctx, cv, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to trigger upgrade: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Upgrade Initiated\n\n")
	sb.WriteString(fmt.Sprintf("**Target Version:** %s\n", targetVersion))
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

// toolGetUpgradeStatus monitors upgrade progress
func (s *Server) toolGetUpgradeStatus(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Upgrade Status\n\n")

	// Check if OpenShift
	cv, err := dynClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		// Not OpenShift - check node versions
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
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
				node.Name,
				node.Status.NodeInfo.KubeletVersion,
				status))
		}

		sb.WriteString("\n**Note:** For non-OpenShift clusters, detailed upgrade progress tracking\n")
		sb.WriteString("depends on your installation method (kubeadm, EKS, GKE, AKS, etc.)\n")
		return sb.String(), false
	}

	// OpenShift - detailed status
	sb.WriteString("**Cluster Type:** OpenShift\n\n")

	// Get version info
	desiredVersion, _, _ := unstructured.NestedString(cv.Object, "status", "desired", "version")
	sb.WriteString(fmt.Sprintf("**Target Version:** %s\n", desiredVersion))

	// Check conditions for progress
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
		sb.WriteString(fmt.Sprintf("**Progress:** %s\n\n", progressMessage))
	} else {
		sb.WriteString("**Status:** Not currently upgrading\n\n")
	}

	// ClusterOperator status
	cos, err := dynClient.Resource(clusterOperatorGVR).List(ctx, metav1.ListOptions{})
	if err == nil {
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

			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				co.GetName(), available, progressing, degraded))
		}
	}

	// MachineConfigPool status
	mcps, err := dynClient.Resource(machineConfigPoolGVR).List(ctx, metav1.ListOptions{})
	if err == nil {
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

			sb.WriteString(fmt.Sprintf("| %s | %d/%d | %d/%d | %s | %s |\n",
				mcp.GetName(), readyCount, machineCount, updatedCount, machineCount, updating, degraded))
		}
	}

	// History
	history, _, _ := unstructured.NestedSlice(cv.Object, "status", "history")
	if len(history) > 0 {
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

			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", ver, state, startTime, completionTime))
		}
	}

	return sb.String(), false
}
