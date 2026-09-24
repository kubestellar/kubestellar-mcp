// Package deploy implements the "deploy" domain MCP tools
// (list_cluster_capabilities, find_clusters_for_workload, deploy_app,
// scale_app, patch_app) extracted from pkg/deploy/mcp as part of epic #983
// (decompose pkg/deploy/mcp into per-domain sub-packages).
//
// This package is behavior-preserving: it holds the exact logic that used
// to live in pkg/deploy/mcp/tools_deploy.go, deploy_apply.go, and
// deploy_cluster_ops.go, moved to functions/methods that take a narrow Deps
// struct instead of the monolithic *Server type. The root package
// (pkg/deploy/mcp) wires this package in via a thin adapter
// (deploy_adapter.go) that re-exports the identifiers still referenced by
// in-package tests, mirroring the pattern used by pkg/mcp/tools/upgrades and
// pkg/deploy/mcp/app.
package deploy

import (
	"context"
	"encoding/json"
	"fmt"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// DeployResult represents the result of a deployment operation
type DeployResult struct {
	Cluster  string `json:"cluster"`
	Resource string `json:"resource"`
	Status   string `json:"status"` // created, updated, unchanged, failed
	Message  string `json:"message,omitempty"`
}

// executorAdapter satisfies app.Executor by delegating to a Deps.Execute
// func field, so HandleScaleApp can reuse app.GetAppInstances for its
// "discover clusters where the app runs" fallback without the deploy
// package depending on the whole *Server.
type executorAdapter struct {
	execute func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error)
}

func (e executorAdapter) Execute(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	return e.execute(ctx, clusterName, fn)
}

// HandleListClusterCapabilities returns cluster capabilities
func HandleListClusterCapabilities(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
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
		results, err := d.Execute(ctx, params.Cluster, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
			return d.GetCapabilitiesForCluster(ctx, client, clusterName)
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
	return d.GetClusterCapabilities(ctx)
}

// HandleFindClustersForWorkload finds clusters matching requirements
func HandleFindClustersForWorkload(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
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

	clusters, err := d.FindClustersForWorkload(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"matchingClusters": clusters,
		"count":            len(clusters),
		"requirements":     req,
	}, nil
}

// HandleDeployApp deploys an app to clusters
func HandleDeployApp(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
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

	if err := d.ValidateManifestDocs(params.Manifest); err != nil {
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
			targetClusters, err = d.FindClustersForWorkload(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("failed to find matching clusters: %w", err)
			}
		} else {
			// All clusters
			clusters, err := d.DiscoverClusters()
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
	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return ApplyManifest(ctx, d, client, clusterName, params.Manifest, params.DryRun)
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

// HandleScaleApp scales an app across clusters
func HandleScaleApp(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
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
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		// Find clusters where app runs
		instances, _ := app.GetAppInstances(ctx, executorAdapter{execute: d.Execute}, args)
		if instanceMap, ok := instances.(map[string]interface{}); ok {
			if instList, ok := instanceMap["instances"].([]app.AppInstance); ok {
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
	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return ScaleAppInCluster(ctx, client, clusterName, params.App, params.Namespace, params.Replicas)
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

// HandlePatchApp patches an app across clusters
func HandlePatchApp(ctx context.Context, d Deps, args json.RawMessage) (interface{}, error) {
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
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
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
		clusters, err := d.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	// Patch on each cluster
	results, err := d.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return PatchAppInCluster(ctx, client, clusterName, params.App, params.Namespace, []byte(params.Patch), patchType)
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"app":     params.App,
		"results": results,
	}, nil
}
