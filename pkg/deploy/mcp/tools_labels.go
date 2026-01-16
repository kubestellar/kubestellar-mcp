package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// LabelResult represents the result of a label operation
type LabelResult struct {
	Cluster   string `json:"cluster"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // labeled, unlabeled, failed, not-found
	Labels    map[string]string `json:"labels,omitempty"`
	Message   string `json:"message,omitempty"`
}

// handleAddLabels adds labels to resources
func (s *Server) handleAddLabels(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.addLabelsInCluster(ctx, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
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

// addLabelsInCluster adds labels to a resource in a single cluster
func (s *Server) addLabelsInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labels map[string]string, dryRun bool) (LabelResult, error) {
	result := LabelResult{
		Cluster:   clusterName,
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		Labels:    labels,
	}

	if dryRun {
		result.Status = "would-label"
		result.Message = fmt.Sprintf("Would add labels to %s/%s", kind, name)
		return result, nil
	}

	// Build patch
	patch := buildLabelPatch(labels, false)

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
	case "secret", "secrets":
		_, err = client.CoreV1().Secrets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
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

// handleRemoveLabels removes labels from resources
func (s *Server) handleRemoveLabels(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.removeLabelsInCluster(ctx, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
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

// removeLabelsInCluster removes labels from a resource in a single cluster
func (s *Server) removeLabelsInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, kind, name, namespace string, labelKeys []string, dryRun bool) (LabelResult, error) {
	result := LabelResult{
		Cluster:   clusterName,
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
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
	patch := buildLabelPatch(labelsToRemove, true)

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
	case "secret", "secrets":
		_, err = client.CoreV1().Secrets(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
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

// buildLabelPatch creates a JSON merge patch for labels
func buildLabelPatch(labels map[string]string, remove bool) []byte {
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
