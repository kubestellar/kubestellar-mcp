// Package labels implements the add_labels/remove_labels MCP tools for the
// kubestellar-deploy server. It was extracted from the flat pkg/deploy/mcp
// package (epic #983, phase 1) so this domain can be built and tested in
// isolation. See Deps for the narrow surface this package needs from
// *mcp.Server.
package labels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// Deps is the narrow set of *mcp.Server capabilities the labels tools need:
// cluster discovery, fan-out execution across selected clusters, and the
// shared sensitive-kind policy (defined in pkg/deploy/mcp/manifest_util.go,
// which stays in the root package per the epic's non-goals).
type Deps interface {
	// DiscoverClusterNames returns the names of every cluster the manager
	// currently knows about, used as the fallback target set when the
	// caller does not specify `clusters`.
	DiscoverClusterNames() ([]string, error)
	// ExecuteOnSelected runs fn against each named cluster, fanning out
	// concurrently and collecting per-cluster results/errors.
	ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	// IsSensitiveKind reports whether kind is on the sensitive-kind
	// blocklist (Secret, ServiceAccount, RBAC, etc.).
	IsSensitiveKind(kind string) bool
	// SensitiveKindError builds the standard error returned when a
	// sensitive kind is blocked.
	SensitiveKindError(kind string) error
}

// LabelResult represents the result of a label operation.
type LabelResult struct {
	Cluster   string            `json:"cluster"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Status    string            `json:"status"` // labeled, unlabeled, failed, not-found
	Labels    map[string]string `json:"labels,omitempty"`
	Message   string            `json:"message,omitempty"`
}

// HandleAddLabels adds labels to resources.
func HandleAddLabels(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
	var params struct {
		Kind      string            `json:"kind"`
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
		Clusters  []string          `json:"clusters"`
		DryRun    bool              `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Kind == "" || params.Name == "" {
		return nil, fmt.Errorf("kind and name are required")
	}
	if len(params.Labels) == 0 {
		return nil, fmt.Errorf("labels are required")
	}
	if d.IsSensitiveKind(params.Kind) {
		return nil, d.SensitiveKindError(params.Kind)
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		names, err := d.DiscoverClusterNames()
		if err != nil {
			return nil, err
		}
		targetClusters = append(targetClusters, names...)
	}

	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return AddLabelsInCluster(ctx, d, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var labelResults []LabelResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			labelResults = append(labelResults, LabelResult{
				Cluster: result.Cluster,
				Kind:    params.Kind,
				Name:    params.Name,
				Status:  "failed",
				Message: result.Error,
			})
		} else if lr, ok := result.Result.(LabelResult); ok {
			labelResults = append(labelResults, lr)
			if lr.Status == "labeled" || lr.Status == "would-label" {
				successCount++
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"labels":         params.Labels,
		"results":        labelResults,
		"dryRun":         params.DryRun,
	}, nil
}

// AddLabelsInCluster adds labels to a resource in a single cluster.
func AddLabelsInCluster(ctx context.Context, d Deps, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labels map[string]string, dryRun bool) (LabelResult, error) {
	result := LabelResult{
		Cluster:   clusterName,
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		Labels:    labels,
	}

	if d.IsSensitiveKind(kind) {
		result.Status = "failed"
		result.Message = d.SensitiveKindError(kind).Error()
		return result, nil
	}

	if dryRun {
		result.Status = "would-label"
		result.Message = fmt.Sprintf("Would add labels to %s/%s", kind, name)
		return result, nil
	}

	// Build patch
	patch := BuildLabelPatch(labels, false)

	ns := namespace
	if ns == "" {
		ns = "default"
	}

	var err error
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		_, err = client.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "service", "services", "svc":
		_, err = client.CoreV1().Services(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "configmap", "configmaps", "cm":
		_, err = client.CoreV1().ConfigMaps(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "pod", "pods":
		_, err = client.CoreV1().Pods(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "statefulset", "statefulsets", "sts":
		_, err = client.AppsV1().StatefulSets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "daemonset", "daemonsets", "ds":
		_, err = client.AppsV1().DaemonSets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "namespace", "namespaces", "ns":
		_, err = client.CoreV1().Namespaces().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "node", "nodes":
		_, err = client.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "persistentvolume", "persistentvolumes", "pv":
		_, err = client.CoreV1().PersistentVolumes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "persistentvolumeclaim", "persistentvolumeclaims", "pvc":
		_, err = client.CoreV1().PersistentVolumeClaims(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	default:
		result.Status = "failed"
		result.Message = fmt.Sprintf("Unsupported resource kind: %s", kind)
		return result, nil
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			result.Status = "not-found"
			result.Message = fmt.Sprintf("%s/%s not found", kind, name)
		} else {
			result.Status = "failed"
			result.Message = err.Error()
		}
	} else {
		result.Status = "labeled"
	}

	return result, nil
}

// HandleRemoveLabels removes labels from resources.
func HandleRemoveLabels(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
	var params struct {
		Kind      string   `json:"kind"`
		Name      string   `json:"name"`
		Namespace string   `json:"namespace"`
		Labels    []string `json:"labels"` // Label keys to remove
		Clusters  []string `json:"clusters"`
		DryRun    bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Kind == "" || params.Name == "" {
		return nil, fmt.Errorf("kind and name are required")
	}
	if len(params.Labels) == 0 {
		return nil, fmt.Errorf("labels are required")
	}
	if d.IsSensitiveKind(params.Kind) {
		return nil, d.SensitiveKindError(params.Kind)
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		names, err := d.DiscoverClusterNames()
		if err != nil {
			return nil, err
		}
		targetClusters = append(targetClusters, names...)
	}

	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return RemoveLabelsInCluster(ctx, d, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var labelResults []LabelResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			labelResults = append(labelResults, LabelResult{
				Cluster: result.Cluster,
				Kind:    params.Kind,
				Name:    params.Name,
				Status:  "failed",
				Message: result.Error,
			})
		} else if lr, ok := result.Result.(LabelResult); ok {
			labelResults = append(labelResults, lr)
			if lr.Status == "unlabeled" || lr.Status == "would-unlabel" {
				successCount++
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"labelKeys":      params.Labels,
		"results":        labelResults,
		"dryRun":         params.DryRun,
	}, nil
}

// RemoveLabelsInCluster removes labels from a resource in a single cluster.
func RemoveLabelsInCluster(ctx context.Context, d Deps, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labelKeys []string, dryRun bool) (LabelResult, error) {
	result := LabelResult{
		Cluster:   clusterName,
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}

	if d.IsSensitiveKind(kind) {
		result.Status = "failed"
		result.Message = d.SensitiveKindError(kind).Error()
		return result, nil
	}

	if dryRun {
		result.Status = "would-unlabel"
		result.Message = fmt.Sprintf("Would remove labels %v from %s/%s", labelKeys, kind, name)
		return result, nil
	}

	// Build patch for removal (set to null)
	labelsToRemove := make(map[string]string)
	for _, key := range labelKeys {
		labelsToRemove[key] = "" // Will be converted to null in patch
	}
	patch := BuildLabelPatch(labelsToRemove, true)

	ns := namespace
	if ns == "" {
		ns = "default"
	}

	var err error
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		_, err = client.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "service", "services", "svc":
		_, err = client.CoreV1().Services(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "configmap", "configmaps", "cm":
		_, err = client.CoreV1().ConfigMaps(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "pod", "pods":
		_, err = client.CoreV1().Pods(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "statefulset", "statefulsets", "sts":
		_, err = client.AppsV1().StatefulSets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "daemonset", "daemonsets", "ds":
		_, err = client.AppsV1().DaemonSets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "namespace", "namespaces", "ns":
		_, err = client.CoreV1().Namespaces().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "node", "nodes":
		_, err = client.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "persistentvolume", "persistentvolumes", "pv":
		_, err = client.CoreV1().PersistentVolumes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	case "persistentvolumeclaim", "persistentvolumeclaims", "pvc":
		_, err = client.CoreV1().PersistentVolumeClaims(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	default:
		result.Status = "failed"
		result.Message = fmt.Sprintf("Unsupported resource kind: %s", kind)
		return result, nil
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			result.Status = "not-found"
			result.Message = fmt.Sprintf("%s/%s not found", kind, name)
		} else {
			result.Status = "failed"
			result.Message = err.Error()
		}
	} else {
		result.Status = "unlabeled"
	}

	return result, nil
}

// BuildLabelPatch creates a JSON merge patch for labels.
func BuildLabelPatch(labels map[string]string, remove bool) []byte {
	labelMap := make(map[string]interface{})
	for k, v := range labels {
		if remove {
			labelMap[k] = nil // null removes the key
		} else {
			labelMap[k] = v
		}
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": labelMap,
		},
	}

	data, _ := json.Marshal(patch)
	return data
}
