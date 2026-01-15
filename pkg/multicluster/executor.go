package multicluster

import (
	"context"
	"sync"

	"k8s.io/client-go/kubernetes"
)

// ClusterResult represents the result of an operation on a single cluster
type ClusterResult struct {
	Cluster string      `json:"cluster"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Executor handles multi-cluster operations
type Executor struct {
	manager *ClientManager
}

// NewExecutor creates a new multi-cluster executor
func NewExecutor(manager *ClientManager) *Executor {
	return &Executor{
		manager: manager,
	}
}

// ExecuteFunc is the function type for operations that run on a single cluster
type ExecuteFunc func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error)

// Execute runs an operation across clusters
// If clusterName is specified, runs on that cluster only
// If clusterName is empty, runs across all clusters in parallel
func (e *Executor) Execute(ctx context.Context, clusterName string, fn ExecuteFunc) ([]ClusterResult, error) {
	if clusterName != "" {
		// Single cluster mode
		return e.executeSingle(ctx, clusterName, fn)
	}

	// Multi-cluster mode
	return e.executeAll(ctx, fn)
}

// executeSingle runs the operation on a single cluster
func (e *Executor) executeSingle(ctx context.Context, clusterName string, fn ExecuteFunc) ([]ClusterResult, error) {
	client, err := e.manager.GetClient(clusterName)
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
func (e *Executor) executeAll(ctx context.Context, fn ExecuteFunc) ([]ClusterResult, error) {
	clusters, err := e.manager.DiscoverClusters()
	if err != nil {
		return nil, err
	}

	var results []ClusterResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, cluster := range clusters {
		wg.Add(1)
		go func(clusterName string) {
			defer wg.Done()

			client, err := e.manager.GetClient(clusterName)
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

// ExecuteOnSelected runs the operation on selected clusters
func (e *Executor) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn ExecuteFunc) ([]ClusterResult, error) {
	var results []ClusterResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, clusterName := range clusterNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			client, err := e.manager.GetClient(name)
			if err != nil {
				mu.Lock()
				results = append(results, ClusterResult{
					Cluster: name,
					Error:   err.Error(),
				})
				mu.Unlock()
				return
			}

			result, err := fn(ctx, client, name)
			mu.Lock()
			if err != nil {
				results = append(results, ClusterResult{
					Cluster: name,
					Error:   err.Error(),
				})
			} else {
				results = append(results, ClusterResult{
					Cluster: name,
					Result:  result,
				})
			}
			mu.Unlock()
		}(clusterName)
	}

	wg.Wait()
	return results, nil
}
