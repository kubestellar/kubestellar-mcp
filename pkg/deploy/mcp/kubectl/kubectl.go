// Package kubectl implements the delete_resource/kubectl_apply MCP tools for
// the kubestellar-deploy server. It was extracted from the flat
// pkg/deploy/mcp package (epic #983, phase 1) so this domain can be built
// and tested in isolation. See Deps for the narrow surface this package
// needs from *mcp.Server.
package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// Deps is the narrow set of *mcp.Server capabilities the kubectl tools need:
// cluster discovery, fan-out execution across selected clusters, cluster
// config lookup for the dynamic client, and the shared sensitive-kind /
// manifest-parsing helpers (defined in pkg/deploy/mcp/manifest_util.go,
// which stays in the root package per the epic's non-goals since it is also
// used by the kustomize domain).
type Deps interface {
	// DiscoverClusterNames returns the names of every cluster the manager
	// currently knows about, used as the fallback target set when the
	// caller does not specify `clusters`.
	DiscoverClusterNames() ([]string, error)
	// ExecuteOnSelected runs fn against each named cluster, fanning out
	// concurrently and collecting per-cluster results/errors.
	ExecuteOnSelected(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
	// GetConfig returns the rest.Config for the named cluster, used to
	// build the dynamic client for kubectl_apply.
	GetConfig(clusterName string) (*rest.Config, error)
	// IsSensitiveKind reports whether kind is on the sensitive-kind
	// blocklist (Secret, ServiceAccount, RBAC, etc.).
	IsSensitiveKind(kind string) bool
	// SensitiveKindError builds the standard error returned when a
	// sensitive kind is blocked.
	SensitiveKindError(kind string) error
	// IsNamespaceKind reports whether kind refers to a (cluster-scoped)
	// Namespace resource, so name (not the namespace field) is validated.
	IsNamespaceKind(kind string) bool
	// ManifestSensitiveKind parses doc and reports its kind plus whether
	// that kind is blocked.
	ManifestSensitiveKind(doc string) (string, bool)
	// YAMLToJSON converts a YAML or JSON manifest string to JSON.
	YAMLToJSON(yamlStr string) string
	// UnstructuredFromYAML parses a YAML manifest into obj.
	UnstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error
}

// DeleteResult represents the result of a delete operation.
type DeleteResult struct {
	Cluster   string `json:"cluster"`
	Resource  string `json:"resource"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // deleted, not-found, failed
	Message   string `json:"message,omitempty"`
}

// ApplyResult represents the result of an apply operation.
type ApplyResult struct {
	Cluster   string `json:"cluster"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // created, updated, unchanged, failed
	Message   string `json:"message,omitempty"`
}

// HandleDeleteResource deletes a resource from clusters.
func HandleDeleteResource(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
	var params struct {
		Kind      string   `json:"kind"`
		Name      string   `json:"name"`
		Namespace string   `json:"namespace"`
		Clusters  []string `json:"clusters"`
		DryRun    bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Kind == "" || params.Name == "" {
		return nil, fmt.Errorf("kind and name are required")
	}

	if d.IsSensitiveKind(params.Kind) {
		return nil, d.SensitiveKindError(params.Kind)
	}

	// Validate namespace to prevent access to system namespaces (#377).
	// For kind Namespace the protected value is name (cluster-scoped), not the
	// namespace field — otherwise deleting kube-system etc. would be allowed.
	if d.IsNamespaceKind(params.Kind) {
		if err := nsval.ValidateNamespace(params.Name); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	} else if params.Namespace != "" {
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
		targetClusters = names
	}

	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return DeleteResourceInCluster(ctx, client, clusterName, params.Kind, params.Name, params.Namespace, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var deleteResults []DeleteResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			deleteResults = append(deleteResults, DeleteResult{
				Cluster:  result.Cluster,
				Resource: params.Kind,
				Name:     params.Name,
				Status:   "failed",
				Message:  result.Error,
			})
		} else if dr, ok := result.Result.(DeleteResult); ok {
			deleteResults = append(deleteResults, dr)
			if dr.Status == "deleted" || dr.Status == "would-delete" {
				successCount++
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        deleteResults,
		"dryRun":         params.DryRun,
	}, nil
}

// HandleKubectlApply applies any Kubernetes resource using the dynamic client.
func HandleKubectlApply(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
	var params struct {
		Manifest string   `json:"manifest"`
		Clusters []string `json:"clusters"`
		DryRun   bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Manifest == "" {
		return nil, fmt.Errorf("manifest is required")
	}

	for _, doc := range strings.Split(params.Manifest, "---") {
		if kind, blocked := d.ManifestSensitiveKind(doc); blocked {
			return nil, d.SensitiveKindError(kind)
		}
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		names, err := d.DiscoverClusterNames()
		if err != nil {
			return nil, err
		}
		targetClusters = names
	}

	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return ApplyManifestDynamic(ctx, d, clusterName, params.Manifest, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var applyResults []ApplyResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			applyResults = append(applyResults, ApplyResult{
				Cluster: result.Cluster,
				Status:  "failed",
				Message: result.Error,
			})
		} else if ar, ok := result.Result.([]ApplyResult); ok {
			applyResults = append(applyResults, ar...)
			for _, r := range ar {
				if r.Status == "created" || r.Status == "updated" || r.Status == "would-apply" {
					successCount++
				}
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        applyResults,
		"dryRun":         params.DryRun,
	}, nil
}

// DeleteResourceInCluster deletes a resource in a single cluster.
// The client parameter uses kubernetes.Interface (not *kubernetes.Clientset)
// so callers can pass a fake clientset from k8s.io/client-go/kubernetes/fake in tests.
func DeleteResourceInCluster(ctx context.Context, client kubernetes.Interface, clusterName, kind, name, namespace string, dryRun bool) (DeleteResult, error) {
	result := DeleteResult{
		Cluster:   clusterName,
		Resource:  kind,
		Name:      name,
		Namespace: namespace,
	}

	if dryRun {
		result.Status = "would-delete"
		result.Message = fmt.Sprintf("Would delete %s/%s", kind, name)
		return result, nil
	}

	var err error
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		err = client.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "service", "services", "svc":
		err = client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "configmap", "configmaps", "cm":
		err = client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "secret", "secrets":
		err = client.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "pod", "pods":
		err = client.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "statefulset", "statefulsets", "sts":
		err = client.AppsV1().StatefulSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "daemonset", "daemonsets", "ds":
		err = client.AppsV1().DaemonSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "job", "jobs":
		err = client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "cronjob", "cronjobs":
		err = client.BatchV1().CronJobs(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "ingress", "ingresses", "ing":
		err = client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		err = client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "namespace", "namespaces", "ns":
		err = client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	case "serviceaccount", "serviceaccounts", "sa":
		err = client.CoreV1().ServiceAccounts(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "role", "roles":
		err = client.RbacV1().Roles(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "rolebinding", "rolebindings":
		err = client.RbacV1().RoleBindings(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "clusterrole", "clusterroles":
		err = client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
	case "clusterrolebinding", "clusterrolebindings":
		err = client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
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
		result.Status = "deleted"
	}

	return result, nil
}

// ApplyManifestDynamic applies manifests using the dynamic client for any resource type.
func ApplyManifestDynamic(ctx context.Context, d Deps, clusterName, manifest string, dryRun bool) ([]ApplyResult, error) {
	var results []ApplyResult

	// Get the dynamic client for this cluster
	config, err := d.GetConfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get config for cluster %s: %w", clusterName, err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Parse YAML documents
	docs := strings.Split(manifest, "---")
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Parse as unstructured
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(d.YAMLToJSON(doc))); err != nil {
			// Try YAML parsing
			if err := d.UnstructuredFromYAML(doc, obj); err != nil {
				results = append(results, ApplyResult{
					Cluster: clusterName,
					Status:  "failed",
					Message: fmt.Sprintf("failed to parse manifest: %v", err),
				})
				continue
			}
		}

		kind := obj.GetKind()
		name := obj.GetName()
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = "default"
		}

		// Validate namespace from manifest to prevent access to system namespaces (#377).
		// For kind Namespace the protected value is metadata.name (cluster-scoped).
		// Append failure and continue rather than returning early, so prior results are preserved (#626).
		if d.IsNamespaceKind(kind) {
			if err := nsval.ValidateNamespace(name); err != nil {
				results = append(results, ApplyResult{Cluster: clusterName, Status: "failed",
					Message: fmt.Sprintf("invalid namespace in manifest: %v", err)})
				continue
			}
		} else if namespace != "" {
			if err := nsval.ValidateNamespace(namespace); err != nil {
				results = append(results, ApplyResult{Cluster: clusterName, Status: "failed",
					Message: fmt.Sprintf("invalid namespace in manifest: %v", err)})
				continue
			}
		}

		result := ApplyResult{
			Cluster:   clusterName,
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		}

		if dryRun {
			result.Status = "would-apply"
			result.Message = fmt.Sprintf("Would apply %s/%s to namespace %s", kind, name, namespace)
			results = append(results, result)
			continue
		}

		// Get the GVR for this resource
		gvr, namespaced := GetGVR(kind)
		if gvr.Resource == "" {
			result.Status = "failed"
			result.Message = fmt.Sprintf("unknown resource kind: %s", kind)
			results = append(results, result)
			continue
		}

		// Apply the resource
		var resourceClient dynamic.ResourceInterface
		if namespaced {
			resourceClient = dynClient.Resource(gvr).Namespace(namespace)
		} else {
			resourceClient = dynClient.Resource(gvr)
		}

		// Try to get existing
		existing, err := resourceClient.Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			// Update
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, err = resourceClient.Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				result.Status = "failed"
				result.Message = err.Error()
			} else {
				result.Status = "updated"
			}
		} else {
			// Create
			_, err = resourceClient.Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				result.Status = "failed"
				result.Message = err.Error()
			} else {
				result.Status = "created"
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// GetGVR returns the GroupVersionResource for common Kubernetes kinds.
func GetGVR(kind string) (schema.GroupVersionResource, bool) {
	kindLower := strings.ToLower(kind)
	switch kindLower {
	// Core v1
	case "pod", "pods":
		return schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true
	case "service", "services":
		return schema.GroupVersionResource{Version: "v1", Resource: "services"}, true
	case "configmap", "configmaps":
		return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true
	case "secret", "secrets":
		return schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true
	case "namespace", "namespaces":
		return schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, false
	case "serviceaccount", "serviceaccounts":
		return schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, true
	case "persistentvolumeclaim", "persistentvolumeclaims":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, true
	case "persistentvolume", "persistentvolumes":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, false

	// Apps v1
	case "deployment", "deployments":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true
	case "statefulset", "statefulsets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true
	case "daemonset", "daemonsets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true
	case "replicaset", "replicasets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, true

	// Batch v1
	case "job", "jobs":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true
	case "cronjob", "cronjobs":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true

	// Networking v1
	case "ingress", "ingresses":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true
	case "networkpolicy", "networkpolicies":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, true

	// RBAC v1
	case "role", "roles":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true
	case "rolebinding", "rolebindings":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true
	case "clusterrole", "clusterroles":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false
	case "clusterrolebinding", "clusterrolebindings":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false

	// HPA
	case "horizontalpodautoscaler", "horizontalpodautoscalers", "hpa":
		return schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, true

	default:
		return schema.GroupVersionResource{}, false
	}
}
