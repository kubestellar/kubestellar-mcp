package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
)

// ClusterResult represents the result of an operation on a single cluster
type ClusterResult struct {
	Cluster string      `json:"cluster"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ExecuteFunc is a function that executes on a single cluster
type ExecuteFunc func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error)

// executeMultiCluster runs an operation across clusters
// If clusterName is specified, runs on that cluster only
// If clusterName is empty, runs across ALL clusters in kubeconfig
func (s *Server) executeMultiCluster(ctx context.Context, clusterName string, fn ExecuteFunc) ([]ClusterResult, error) {
	if clusterName != "" {
		// Single cluster mode
		return s.executeSingle(ctx, clusterName, fn)
	}

	// Multi-cluster mode - run across all clusters
	return s.executeAll(ctx, fn)
}

// executeSingle runs the operation on a single cluster
func (s *Server) executeSingle(ctx context.Context, clusterName string, fn ExecuteFunc) ([]ClusterResult, error) {
	client, err := s.getClientForCluster(clusterName)
	if err != nil {
		return []ClusterResult{{
			Cluster: clusterName,
			Error:   err.Error(),
		}}, nil
	}

	result, err := fn(ctx, client, clusterName)
	if err != nil {
		return []ClusterResult{{
			Cluster: clusterName,
			Error:   err.Error(),
		}}, nil
	}

	return []ClusterResult{{
		Cluster: clusterName,
		Result:  result,
	}}, nil
}

// executeAll runs the operation across all clusters in parallel
func (s *Server) executeAll(ctx context.Context, fn ExecuteFunc) ([]ClusterResult, error) {
	clusters, err := s.discoverer.DiscoverClusters("kubeconfig")
	if err != nil {
		return nil, fmt.Errorf("failed to discover clusters: %w", err)
	}

	if len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters found in kubeconfig")
	}

	var results []ClusterResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, cluster := range clusters {
		wg.Add(1)
		go func(clusterName string) {
			defer wg.Done()

			client, err := s.getClientForCluster(clusterName)
			if err != nil {
				mu.Lock()
				results = append(results, ClusterResult{
					Cluster: clusterName,
					Error:   err.Error(),
				})
				mu.Unlock()
				return
			}

			result, err := fn(ctx, client, clusterName)
			mu.Lock()
			if err != nil {
				results = append(results, ClusterResult{
					Cluster: clusterName,
					Error:   err.Error(),
				})
			} else {
				results = append(results, ClusterResult{
					Cluster: clusterName,
					Result:  result,
				})
			}
			mu.Unlock()
		}(cluster.Name)
	}

	wg.Wait()
	return results, nil
}

// formatMultiClusterResults formats results for display
func formatMultiClusterResults(results []ClusterResult) string {
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data)
}
