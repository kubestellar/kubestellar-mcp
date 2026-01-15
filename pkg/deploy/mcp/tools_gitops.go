package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/klaude/pkg/gitops"
)

// GitOpsDriftResult aggregates drift results from multiple clusters
type GitOpsDriftResult struct {
	Source       gitops.ManifestSource `json:"source"`
	TotalDrifts  int                   `json:"totalDrifts"`
	ClusterCount int                   `json:"clusterCount"`
	Drifts       []gitops.DriftResult  `json:"drifts"`
}

// GitOpsSyncResult aggregates sync results from multiple clusters
type GitOpsSyncResult struct {
	Source    gitops.ManifestSource   `json:"source"`
	DryRun    bool                    `json:"dryRun"`
	Summaries []gitops.SyncSummary    `json:"summaries"`
}

// handleDetectDrift detects drift between git and clusters
func (s *Server) handleDetectDrift(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Repo     string   `json:"repo"`
		Path     string   `json:"path"`
		Branch   string   `json:"branch"`
		Clusters []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	source := gitops.ManifestSource{
		Repo:   params.Repo,
		Path:   params.Path,
		Branch: params.Branch,
	}

	// Read manifests from git
	reader := gitops.NewManifestReader()
	defer reader.Cleanup()

	manifests, err := reader.ReadFromGit(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifests from git: %w", err)
	}

	if len(manifests) == 0 {
		return map[string]interface{}{
			"message": "No manifests found in repository",
			"source":  source,
		}, nil
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

	// Detect drift on each cluster
	result := &GitOpsDriftResult{
		Source:       source,
		ClusterCount: len(targetClusters),
	}

	var allDrifts []gitops.DriftResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, clusterName := range targetClusters {
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()

			config, err := s.manager.GetConfig(cluster)
			if err != nil {
				mu.Lock()
				allDrifts = append(allDrifts, gitops.DriftResult{
					Cluster:     cluster,
					DriftType:   gitops.DriftTypeMissing,
					Differences: []string{fmt.Sprintf("Failed to get config: %v", err)},
				})
				mu.Unlock()
				return
			}

			detector, err := gitops.NewDriftDetector(config)
			if err != nil {
				mu.Lock()
				allDrifts = append(allDrifts, gitops.DriftResult{
					Cluster:     cluster,
					DriftType:   gitops.DriftTypeMissing,
					Differences: []string{fmt.Sprintf("Failed to create detector: %v", err)},
				})
				mu.Unlock()
				return
			}

			drifts, err := detector.DetectDrift(ctx, manifests, cluster)
			if err != nil {
				mu.Lock()
				allDrifts = append(allDrifts, gitops.DriftResult{
					Cluster:     cluster,
					DriftType:   gitops.DriftTypeMissing,
					Differences: []string{fmt.Sprintf("Failed to detect drift: %v", err)},
				})
				mu.Unlock()
				return
			}

			mu.Lock()
			allDrifts = append(allDrifts, drifts...)
			mu.Unlock()
		}(clusterName)
	}

	wg.Wait()

	result.Drifts = allDrifts
	result.TotalDrifts = len(allDrifts)

	return result, nil
}

// handleSyncFromGit syncs manifests from git to clusters
func (s *Server) handleSyncFromGit(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Repo      string   `json:"repo"`
		Path      string   `json:"path"`
		Branch    string   `json:"branch"`
		Clusters  []string `json:"clusters"`
		DryRun    bool     `json:"dry_run"`
		Namespace string   `json:"namespace"`
		Include   []string `json:"include"`
		Exclude   []string `json:"exclude"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	source := gitops.ManifestSource{
		Repo:   params.Repo,
		Path:   params.Path,
		Branch: params.Branch,
	}

	// Read manifests from git
	reader := gitops.NewManifestReader()
	defer reader.Cleanup()

	manifests, err := reader.ReadFromGit(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifests from git: %w", err)
	}

	if len(manifests) == 0 {
		return map[string]interface{}{
			"message": "No manifests found in repository",
			"source":  source,
		}, nil
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

	// Sync to each cluster
	result := &GitOpsSyncResult{
		Source: source,
		DryRun: params.DryRun,
	}

	opts := gitops.SyncOptions{
		DryRun:    params.DryRun,
		Namespace: params.Namespace,
		Include:   params.Include,
		Exclude:   params.Exclude,
	}

	var summaries []gitops.SyncSummary
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, clusterName := range targetClusters {
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()

			config, err := s.manager.GetConfig(cluster)
			if err != nil {
				mu.Lock()
				summaries = append(summaries, gitops.SyncSummary{
					Cluster: cluster,
					Failed:  1,
					Results: []gitops.SyncResult{{
						Cluster: cluster,
						Action:  gitops.SyncActionFailed,
						Message: fmt.Sprintf("Failed to get config: %v", err),
					}},
				})
				mu.Unlock()
				return
			}

			syncer, err := gitops.NewSyncer(config)
			if err != nil {
				mu.Lock()
				summaries = append(summaries, gitops.SyncSummary{
					Cluster: cluster,
					Failed:  1,
					Results: []gitops.SyncResult{{
						Cluster: cluster,
						Action:  gitops.SyncActionFailed,
						Message: fmt.Sprintf("Failed to create syncer: %v", err),
					}},
				})
				mu.Unlock()
				return
			}

			summary, err := syncer.Sync(ctx, manifests, cluster, opts)
			if err != nil {
				mu.Lock()
				summaries = append(summaries, gitops.SyncSummary{
					Cluster: cluster,
					Failed:  1,
					Results: []gitops.SyncResult{{
						Cluster: cluster,
						Action:  gitops.SyncActionFailed,
						Message: fmt.Sprintf("Failed to sync: %v", err),
					}},
				})
				mu.Unlock()
				return
			}

			mu.Lock()
			summaries = append(summaries, *summary)
			mu.Unlock()
		}(clusterName)
	}

	wg.Wait()

	result.Summaries = summaries
	return result, nil
}

// handleReconcile brings clusters back in sync with git
func (s *Server) handleReconcile(ctx context.Context, args json.RawMessage) (interface{}, error) {
	// Reconcile is just sync without dry_run
	var params struct {
		Repo      string   `json:"repo"`
		Path      string   `json:"path"`
		Branch    string   `json:"branch"`
		Clusters  []string `json:"clusters"`
		Namespace string   `json:"namespace"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Build sync args
	syncArgs, _ := json.Marshal(map[string]interface{}{
		"repo":      params.Repo,
		"path":      params.Path,
		"branch":    params.Branch,
		"clusters":  params.Clusters,
		"namespace": params.Namespace,
		"dry_run":   false,
	})

	return s.handleSyncFromGit(ctx, syncArgs)
}

// handlePreviewChanges shows what would change without applying
func (s *Server) handlePreviewChanges(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Repo      string   `json:"repo"`
		Path      string   `json:"path"`
		Branch    string   `json:"branch"`
		Clusters  []string `json:"clusters"`
		Namespace string   `json:"namespace"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Build sync args with dry_run=true
	syncArgs, _ := json.Marshal(map[string]interface{}{
		"repo":      params.Repo,
		"path":      params.Path,
		"branch":    params.Branch,
		"clusters":  params.Clusters,
		"namespace": params.Namespace,
		"dry_run":   true,
	})

	return s.handleSyncFromGit(ctx, syncArgs)
}

// Unused but kept for interface compatibility
var _ = func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
	return nil, nil
}
