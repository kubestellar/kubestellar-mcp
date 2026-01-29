package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// DeployResult represents the result of a deployment operation
type DeployResult struct {
	Cluster  string `json:"cluster"`
	Resource string `json:"resource"`
	Status   string `json:"status"` // created, updated, unchanged, failed
	Message  string `json:"message,omitempty"`
}

// handleListClusterCapabilities returns cluster capabilities
func (s *Server) handleListClusterCapabilities(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Cluster string `json:"cluster"`
	}
	if args != nil {
		json.Unmarshal(args, &params)
	}

	if params.Cluster != "" {
		// Single cluster
		results, err := s.executor.Execute(ctx, params.Cluster, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
			return s.selector.GetCapabilitiesForCluster(ctx, client, clusterName)
		})
		if err != nil {
			return nil, err
		}
		if len(results) > 0 && results[0].Error == "" {
			return results[0].Result, nil
		}
		return nil, fmt.Errorf("failed to get capabilities for cluster %s", params.Cluster)
	}

	// All clusters
	return s.selector.GetClusterCapabilities(ctx)
}

// handleFindClustersForWorkload finds clusters matching requirements
func (s *Server) handleFindClustersForWorkload(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		GPUType   string            `json:"gpu_type"`
		MinGPU    int64             `json:"min_gpu"`
		MinMemory string            `json:"min_memory"`
		MinCPU    string            `json:"min_cpu"`
		Labels    map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	req := multicluster.WorkloadRequirements{
		GPUType:    params.GPUType,
		MinGPU:     params.MinGPU,
		MinMemory:  params.MinMemory,
		MinCPU:     params.MinCPU,
		NodeLabels: params.Labels,
	}

	clusters, err := s.selector.FindClustersForWorkload(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"matchingClusters": clusters,
		"count":           len(clusters),
		"requirements":    req,
	}, nil
}

// handleDeployApp deploys an app to clusters
func (s *Server) handleDeployApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Manifest string   `json:"manifest"`
		Clusters []string `json:"clusters"`
		GPUType  string   `json:"gpu_type"`
		MinGPU   int64    `json:"min_gpu"`
		DryRun   bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Determine target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		if params.GPUType != "" || params.MinGPU > 0 {
			// Find clusters matching GPU requirements
			req := multicluster.WorkloadRequirements{
				GPUType: params.GPUType,
				MinGPU:  params.MinGPU,
			}
			var err error
			targetClusters, err = s.selector.FindClustersForWorkload(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("failed to find matching clusters: %w", err)
			}
		} else {
			// All clusters
			clusters, err := s.manager.DiscoverClusters()
			if err != nil {
				return nil, err
			}
			for _, c := range clusters {
				targetClusters = append(targetClusters, c.Name)
			}
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("no clusters found matching requirements")
	}

	// Deploy to clusters
	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.applyManifest(ctx, client, clusterName, params.Manifest, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	// Summarize results
	var deployResults []DeployResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			deployResults = append(deployResults, DeployResult{
				Cluster: result.Cluster,
				Status:  "failed",
				Message: result.Error,
			})
		} else if dr, ok := result.Result.([]DeployResult); ok {
			deployResults = append(deployResults, dr...)
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        deployResults,
		"dryRun":         params.DryRun,
	}, nil
}

// applyManifest applies a manifest to a cluster
func (s *Server) applyManifest(ctx context.Context, client *kubernetes.Clientset, clusterName, manifest string, dryRun bool) ([]DeployResult, error) {
	var results []DeployResult

	// Split manifest into documents
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)

	for {
		var rawObj map[string]interface{}
		if err := decoder.Decode(&rawObj); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}

		if rawObj == nil {
			continue
		}

		kind, _ := rawObj["kind"].(string)
		metadata, _ := rawObj["metadata"].(map[string]interface{})
		name, _ := metadata["name"].(string)
		namespace, _ := metadata["namespace"].(string)
		if namespace == "" {
			namespace = "default"
		}

		resourceName := fmt.Sprintf("%s/%s", kind, name)

		// For dry run, just record what would happen
		if dryRun {
			results = append(results, DeployResult{
				Cluster:  clusterName,
				Resource: resourceName,
				Status:   "would-apply",
				Message:  fmt.Sprintf("Would apply %s to namespace %s", resourceName, namespace),
			})
			continue
		}

		// Apply based on kind
		var err error
		var status string

		switch kind {
		case "Deployment":
			status, err = s.applyDeployment(ctx, client, rawObj, namespace)
		case "Service":
			status, err = s.applyService(ctx, client, rawObj, namespace)
		case "ConfigMap":
			status, err = s.applyConfigMap(ctx, client, rawObj, namespace)
		case "Secret":
			status, err = s.applySecret(ctx, client, rawObj, namespace)
		default:
			status = "skipped"
			err = fmt.Errorf("unsupported kind: %s", kind)
		}

		if err != nil {
			results = append(results, DeployResult{
				Cluster:  clusterName,
				Resource: resourceName,
				Status:   "failed",
				Message:  err.Error(),
			})
		} else {
			results = append(results, DeployResult{
				Cluster:  clusterName,
				Resource: resourceName,
				Status:   status,
			})
		}
	}

	return results, nil
}

// applyDeployment creates or updates a deployment
func (s *Server) applyDeployment(ctx context.Context, client *kubernetes.Clientset, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var deployment appsv1.Deployment
	if err := json.Unmarshal(data, &deployment); err != nil {
		return "", err
	}

	// Try to get existing
	existing, err := client.AppsV1().Deployments(namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
	if err == nil {
		// Update
		deployment.ResourceVersion = existing.ResourceVersion
		_, err = client.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
		if err != nil {
			return "", err
		}
		return "updated", nil
	}

	// Create
	_, err = client.AppsV1().Deployments(namespace).Create(ctx, &deployment, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return "created", nil
}

// applyService creates or updates a service
func (s *Server) applyService(ctx context.Context, client *kubernetes.Clientset, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var service corev1.Service
	if err := json.Unmarshal(data, &service); err != nil {
		return "", err
	}

	// Try to get existing
	existing, err := client.CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err == nil {
		// Update - preserve clusterIP
		service.Spec.ClusterIP = existing.Spec.ClusterIP
		service.ResourceVersion = existing.ResourceVersion
		_, err = client.CoreV1().Services(namespace).Update(ctx, &service, metav1.UpdateOptions{})
		if err != nil {
			return "", err
		}
		return "updated", nil
	}

	// Create
	_, err = client.CoreV1().Services(namespace).Create(ctx, &service, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return "created", nil
}

// applyConfigMap creates or updates a configmap
func (s *Server) applyConfigMap(ctx context.Context, client *kubernetes.Clientset, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var cm corev1.ConfigMap
	if err := json.Unmarshal(data, &cm); err != nil {
		return "", err
	}

	// Try to get existing
	existing, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cm.Name, metav1.GetOptions{})
	if err == nil {
		// Update
		cm.ResourceVersion = existing.ResourceVersion
		_, err = client.CoreV1().ConfigMaps(namespace).Update(ctx, &cm, metav1.UpdateOptions{})
		if err != nil {
			return "", err
		}
		return "updated", nil
	}

	// Create
	_, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, &cm, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return "created", nil
}

// applySecret creates or updates a secret
func (s *Server) applySecret(ctx context.Context, client *kubernetes.Clientset, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var secret corev1.Secret
	if err := json.Unmarshal(data, &secret); err != nil {
		return "", err
	}

	// Try to get existing
	existing, err := client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err == nil {
		// Update
		secret.ResourceVersion = existing.ResourceVersion
		_, err = client.CoreV1().Secrets(namespace).Update(ctx, &secret, metav1.UpdateOptions{})
		if err != nil {
			return "", err
		}
		return "updated", nil
	}

	// Create
	_, err = client.CoreV1().Secrets(namespace).Create(ctx, &secret, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return "created", nil
}

// handleScaleApp scales an app across clusters
func (s *Server) handleScaleApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string   `json:"app"`
		Namespace string   `json:"namespace"`
		Replicas  int32    `json:"replicas"`
		Clusters  []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		// Find clusters where app runs
		instances, _ := s.handleGetAppInstances(ctx, args)
		if instanceMap, ok := instances.(map[string]interface{}); ok {
			if instList, ok := instanceMap["instances"].([]AppInstance); ok {
				clusterSet := make(map[string]bool)
				for _, inst := range instList {
					clusterSet[inst.Cluster] = true
				}
				for c := range clusterSet {
					targetClusters = append(targetClusters, c)
				}
			}
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("app %s not found in any cluster", params.App)
	}

	// Scale on each cluster
	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.scaleAppInCluster(ctx, client, clusterName, params.App, params.Namespace, params.Replicas)
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"app":      params.App,
		"replicas": params.Replicas,
		"results":  results,
	}, nil
}

// scaleAppInCluster scales an app in a single cluster
func (s *Server) scaleAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, replicas int32) (interface{}, error) {
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	// Find deployment
	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, d := range deployments.Items {
		if matchesApp(d.Name, d.Labels, appName) {
			scale := d.Spec.Replicas
			d.Spec.Replicas = &replicas
			_, err := client.AppsV1().Deployments(ns).Update(ctx, &d, metav1.UpdateOptions{})
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"cluster":      clusterName,
				"deployment":   d.Name,
				"oldReplicas":  *scale,
				"newReplicas":  replicas,
			}, nil
		}
	}

	return nil, fmt.Errorf("deployment %s not found in cluster %s", appName, clusterName)
}

// handlePatchApp patches an app across clusters
func (s *Server) handlePatchApp(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		App       string   `json:"app"`
		Namespace string   `json:"namespace"`
		Patch     string   `json:"patch"`
		PatchType string   `json:"patch_type"`
		Clusters  []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	patchType := types.StrategicMergePatchType
	switch params.PatchType {
	case "merge":
		patchType = types.MergePatchType
	case "json":
		patchType = types.JSONPatchType
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		// All clusters
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	// Patch on each cluster
	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.patchAppInCluster(ctx, client, clusterName, params.App, params.Namespace, []byte(params.Patch), patchType)
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"app":     params.App,
		"results": results,
	}, nil
}

// patchAppInCluster patches an app in a single cluster
func (s *Server) patchAppInCluster(ctx context.Context, client *kubernetes.Clientset, clusterName, appName, namespace string, patch []byte, patchType types.PatchType) (interface{}, error) {
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	// Find deployment
	deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, d := range deployments.Items {
		if matchesApp(d.Name, d.Labels, appName) {
			_, err := client.AppsV1().Deployments(ns).Patch(ctx, d.Name, patchType, patch, metav1.PatchOptions{})
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"cluster":    clusterName,
				"deployment": d.Name,
				"status":     "patched",
			}, nil
		}
	}

	return nil, fmt.Errorf("deployment %s not found in cluster %s", appName, clusterName)
}