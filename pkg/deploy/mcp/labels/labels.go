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

	results, targetClusters, successCount, err := runLabelMutation(ctx, d, params.Kind, params.Name, params.Namespace, params.Clusters,
		func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
			return AddLabelsInCluster(ctx, d, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
		},
		func(status string) bool { return status == "labeled" || status == "would-label" },
	)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"labels":         params.Labels,
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// runLabelMutation validates the sensitive-kind policy and namespace,
// resolves target clusters (falling back to DiscoverClusterNames when the
// caller specifies none), fans exec out across them via d.ExecuteOnSelected,
// and aggregates the per-cluster LabelResults into a flat slice plus a
// success count (per isSuccess). It is shared by HandleAddLabels and
// HandleRemoveLabels, which differ only in exec and which result status(es)
// count as success.
func runLabelMutation(ctx context.Context, d Deps, kind, name, namespace string, clusters []string, exec multicluster.ExecuteFunc, isSuccess func(status string) bool) (results []LabelResult, targetClusters []string, successCount int, err error) {
	if d.IsSensitiveKind(kind) {
		return nil, nil, 0, d.SensitiveKindError(kind)
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if namespace != "" {
		if err := nsval.ValidateNamespace(namespace); err != nil {
			return nil, nil, 0, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Get target clusters
	targetClusters = clusters
	if len(targetClusters) == 0 {
		names, err := d.DiscoverClusterNames()
		if err != nil {
			return nil, nil, 0, err
		}
		targetClusters = append(targetClusters, names...)
	}

	clusterResults, err := d.ExecuteOnSelected(ctx, targetClusters, exec)
	if err != nil {
		return nil, nil, 0, err
	}

	for _, result := range clusterResults {
		if result.Error != "" {
			results = append(results, LabelResult{
				Cluster: result.Cluster,
				Kind:    kind,
				Name:    name,
				Status:  "failed",
				Message: result.Error,
			})
		} else if lr, ok := result.Result.(LabelResult); ok {
			results = append(results, lr)
			if isSuccess(lr.Status) {
				successCount++
			}
		}
	}

	return results, targetClusters, successCount, nil
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

	supported, err := patchResourceByKind(ctx, client, kind, ns, name, patch)
	if !supported {
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

// patchResourceByKind dispatches a JSON merge patch to the client-go method
// set matching kind, patching name in namespace ns (cluster-scoped kinds
// ignore ns). supported reports whether kind is recognized; err carries the
// underlying API error, if any. Shared by AddLabelsInCluster and
// RemoveLabelsInCluster, which differ only in the patch bytes they build.
func patchResourceByKind(ctx context.Context, client *kubernetes.Clientset, kind, ns, name string, patch []byte) (supported bool, err error) {
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
		return false, nil
	}
	return true, err
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

	results, targetClusters, successCount, err := runLabelMutation(ctx, d, params.Kind, params.Name, params.Namespace, params.Clusters,
		func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
			return RemoveLabelsInCluster(ctx, d, client, clusterName, params.Kind, params.Name, params.Namespace, params.Labels, params.DryRun)
		},
		func(status string) bool { return status == "unlabeled" || status == "would-unlabel" },
	)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"labelKeys":      params.Labels,
		"results":        results,
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

	supported, err := patchResourceByKind(ctx, client, kind, ns, name, patch)
	if !supported {
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
