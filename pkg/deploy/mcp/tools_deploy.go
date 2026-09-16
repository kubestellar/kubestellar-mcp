package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
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
		"count":            len(clusters),
		"requirements":     req,
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

	if err := validateManifestDocs(params.Manifest); err != nil {
		return nil, err
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

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := server.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
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

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := server.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
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

// deployToolDefs returns the tool definitions handled by this file.
func (s *Server) deployToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "list_cluster_capabilities",
			Description: "List what each cluster can run: GPU availability, CPU/memory capacity, node labels. Use this to understand cluster resources.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Specific cluster (all clusters if not specified)",
					},
				},
			},
			Handler: s.handleListClusterCapabilities,
		},
		{
			Name:        "find_clusters_for_workload",
			Description: "Find clusters that can run a workload with specific requirements (GPU, memory, CPU, labels).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "GPU type required (e.g., nvidia.com/gpu)",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Minimum number of GPUs required",
					},
					"min_memory": map[string]interface{}{
						"type":        "string",
						"description": "Minimum memory required (e.g., 16Gi)",
					},
					"min_cpu": map[string]interface{}{
						"type":        "string",
						"description": "Minimum CPU required (e.g., 4)",
					},
					"labels": map[string]interface{}{
						"type":        "object",
						"description": "Required node labels",
					},
				},
			},
			Handler: s.handleFindClustersForWorkload,
		},
		{
			Name:        "deploy_app",
			Description: "Deploy an app to clusters. Can specify clusters explicitly or let kubestellar find matching clusters based on requirements.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"manifest": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes manifest (YAML)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all matching clusters if not specified)",
					},
					"gpu_type": map[string]interface{}{
						"type":        "string",
						"description": "Deploy to clusters with this GPU type",
					},
					"min_gpu": map[string]interface{}{
						"type":        "integer",
						"description": "Deploy to clusters with at least this many GPUs",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
				},
				"required": []string{"manifest"},
			},
			Handler: s.handleDeployApp,
		},
		{
			Name:        "scale_app",
			Description: "Scale an app across clusters. Can target specific clusters or all clusters where app runs.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Target replica count",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters where app runs if not specified)",
					},
				},
				"required": []string{"app", "replicas"},
			},
			Handler: s.handleScaleApp,
		},
		{
			Name:        "patch_app",
			Description: "Apply a patch to an app across clusters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "App name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace",
					},
					"patch": map[string]interface{}{
						"type":        "string",
						"description": "JSON or strategic merge patch",
					},
					"patch_type": map[string]interface{}{
						"type":        "string",
						"description": "Patch type: strategic, merge, or json (default: strategic)",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters",
					},
				},
				"required": []string{"app", "patch"},
			},
			Handler: s.handlePatchApp,
		},
	}
}
