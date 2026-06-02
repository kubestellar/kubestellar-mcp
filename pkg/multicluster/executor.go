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

const defaultMaxConcurrentClusterOperations = 20

// Executor handles multi-cluster operations
type Executor struct {
	manager        *ClientManager
	maxConcurrency int
}

// NewExecutor creates a new multi-cluster executor
func NewExecutor(manager *ClientManager) *Executor {
	return &Executor{
		manager:        manager,
		maxConcurrency: defaultMaxConcurrentClusterOperations,
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

	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.Name)
	}

	return e.executeAcrossClusters(ctx, clusterNames, fn), nil
}

// ExecuteOnSelected runs the operation on selected clusters
func (e *Executor) ExecuteOnSelected(ctx context.Context, clusterNames []string, fn ExecuteFunc) ([]ClusterResult, error) {
	return e.executeAcrossClusters(ctx, clusterNames, fn), nil
}

func (e *Executor) executeAcrossClusters(ctx context.Context, clusterNames []string, fn ExecuteFunc) []ClusterResult {
	results := make([]ClusterResult, 0, len(clusterNames))
	sem := make(chan struct{}, e.concurrencyLimit())
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, clusterName := range clusterNames {
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

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
	return results
}

func (e *Executor) concurrencyLimit() int {
	if e.maxConcurrency > 0 {
		return e.maxConcurrency
	}
	return defaultMaxConcurrentClusterOperations
}
