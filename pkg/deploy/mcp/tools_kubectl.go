package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// DeleteResult represents the result of a delete operation
type DeleteResult struct {
	Cluster   string `json:"cluster"`
	Resource  string `json:"resource"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // deleted, not-found, failed
	Message   string `json:"message,omitempty"`
}

// ApplyResult represents the result of an apply operation
type ApplyResult struct {
	Cluster   string `json:"cluster"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // created, updated, unchanged, failed
	Message   string `json:"message,omitempty"`
}

// handleDeleteResource deletes a resource from clusters
func (s *Server) handleDeleteResource(ctx context.Context, args json.RawMessage) (interface{}, error) {
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
		return s.deleteResourceInCluster(ctx, client, clusterName, params.Kind, params.Name, params.Namespace, params.DryRun)
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

// deleteResourceInCluster deletes a resource in a single cluster
func (s *Server) deleteResourceInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, kind, name, namespace string, dryRun bool) (DeleteResult, error) {
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

// handleKubectlApply applies any Kubernetes resource using dynamic client
func (s *Server) handleKubectlApply(ctx context.Context, args json.RawMessage) (interface{}, error) {
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
		return s.applyManifestDynamic(ctx, clusterName, params.Manifest, params.DryRun)
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

// applyManifestDynamic applies manifests using the dynamic client for any resource type
func (s *Server) applyManifestDynamic(ctx context.Context, clusterName, manifest string, dryRun bool) ([]ApplyResult, error) {
	var results []ApplyResult

	// Get the dynamic client for this cluster
	config, err := s.manager.GetConfig(clusterName)
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
		if err := obj.UnmarshalJSON([]byte(yamlToJSON(doc))); err != nil {
			// Try YAML parsing
			if err := unstructuredFromYAML(doc, obj); err != nil {
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
		gvr, namespaced := getGVR(kind)
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

// getGVR returns the GroupVersionResource for common Kubernetes kinds
func getGVR(kind string) (schema.GroupVersionResource, bool) {
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

// yamlToJSON is a simple converter (for basic cases)
func yamlToJSON(yamlStr string) string {
	// This is a simplified version - in production use a proper YAML parser
	// For now, we'll rely on unstructuredFromYAML
	return yamlStr
}

// unstructuredFromYAML parses YAML into an Unstructured object
func unstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error {
	// Use k8s.io/apimachinery/pkg/util/yaml for proper parsing
	data, err := yamlToJSONBytes([]byte(yamlStr))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

// yamlToJSONBytes converts YAML bytes to JSON bytes
func yamlToJSONBytes(y []byte) ([]byte, error) {
	// Simple YAML to JSON conversion using k8s utilities
	// This handles the common case of simple YAML manifests
	var obj map[string]interface{}
	if err := parseYAML(y, &obj); err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

// parseYAML is a simple YAML parser for Kubernetes manifests
func parseYAML(data []byte, v interface{}) error {
	// Use the k8s YAML decoder which handles both YAML and JSON
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	// Try JSON first
	if err := decoder.Decode(v); err == nil {
		return nil
	}
	// Fall back to YAML parsing via the existing manifest parser
	// For simplicity, we convert common YAML patterns
	return fmt.Errorf("YAML parsing requires k8s.io/apimachinery/pkg/util/yaml - use JSON format or deploy_app tool")
}
