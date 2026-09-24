// Package gitops implements the detect_drift/sync_from_git/reconcile/
// preview_changes MCP tools for the kubestellar-deploy server. It was
// extracted from pkg/deploy/mcp (tools_gitops.go) as part of the per-domain
// decomposition tracked by epic #983; behavior is unchanged.
package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	upstreamgitops "github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// maxConcurrentClusters bounds how many clusters are processed concurrently
// by runClusterTasks.
const maxConcurrentClusters = 20

// ClusterAccess is the narrow slice of *multicluster.ClientManager that the
// gitops handlers need: discovering clusters and resolving per-cluster
// kubeconfigs.
type ClusterAccess interface {
	DiscoverClusters() ([]multicluster.ClusterInfo, error)
	GetConfig(clusterName string) (*rest.Config, error)
}

// ManifestSyncer applies manifests to a cluster.
type ManifestSyncer interface {
	Sync(ctx context.Context, manifests []upstreamgitops.Manifest, clusterName string, opts upstreamgitops.SyncOptions) (*upstreamgitops.SyncSummary, error)
}

// DriftDetector detects drift between git manifests and cluster state.
type DriftDetector interface {
	DetectDrift(ctx context.Context, manifests []upstreamgitops.Manifest, clusterName string) ([]upstreamgitops.DriftResult, error)
}

// Server holds the dependencies the gitops tool handlers need. It is built
// by the root package's thin adapter (gitops_adapter.go), which wires Access
// and the factories to *deploy/mcp.Server's manager and factory fields.
type Server struct {
	Access ClusterAccess
	// NewManifestReader is a factory for creating manifest readers.
	NewManifestReader func() *upstreamgitops.ManifestReader
	// NewManifestSyncer is a factory for creating manifest syncers.
	NewManifestSyncer func(*rest.Config) (ManifestSyncer, error)
	// NewDriftDetector is a factory for creating drift detectors.
	NewDriftDetector func(*rest.Config) (DriftDetector, error)
}

// DriftResult aggregates drift results from multiple clusters.
type DriftResult struct {
	Source       upstreamgitops.ManifestSource `json:"source"`
	TotalDrifts  int                           `json:"totalDrifts"`
	ClusterCount int                           `json:"clusterCount"`
	Drifts       []upstreamgitops.DriftResult  `json:"drifts"`
}

// SyncResult aggregates sync results from multiple clusters.
type SyncResult struct {
	Source    upstreamgitops.ManifestSource `json:"source"`
	DryRun    bool                          `json:"dryRun"`
	Summaries []upstreamgitops.SyncSummary  `json:"summaries"`
}

func runClusterTasks(clusterNames []string, fn func(string)) {
	sem := make(chan struct{}, maxConcurrentClusters)
	var wg sync.WaitGroup

	for _, clusterName := range clusterNames {
		sem <- struct{}{}
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(cluster)
		}(clusterName)
	}

	wg.Wait()
}

// HandleDetectDrift detects drift between git and clusters.
func (s *Server) HandleDetectDrift(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	source := upstreamgitops.ManifestSource{
		Repo:   params.Repo,
		Path:   params.Path,
		Branch: params.Branch,
	}

	// Read manifests from git
	reader := s.NewManifestReader()
	defer reader.Cleanup()

	manifests, err := reader.ReadFromGit(ctx, source)
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
		clusters, err := s.Access.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	// Detect drift on each cluster
	result := &DriftResult{
		Source:       source,
		ClusterCount: len(targetClusters),
	}

	allDrifts := make([]upstreamgitops.DriftResult, 0)
	var mu sync.Mutex

	runClusterTasks(targetClusters, func(cluster string) {
		config, err := s.Access.GetConfig(cluster)
		if err != nil {
			mu.Lock()
			allDrifts = append(allDrifts, upstreamgitops.DriftResult{
				Cluster:     cluster,
				DriftType:   upstreamgitops.DriftTypeMissing,
				Differences: []string{fmt.Sprintf("Failed to get config: %v", err)},
			})
			mu.Unlock()
			return
		}

		detector, err := s.NewDriftDetector(config)
		if err != nil {
			mu.Lock()
			allDrifts = append(allDrifts, upstreamgitops.DriftResult{
				Cluster:     cluster,
				DriftType:   upstreamgitops.DriftTypeMissing,
				Differences: []string{fmt.Sprintf("Failed to create detector: %v", err)},
			})
			mu.Unlock()
			return
		}

		drifts, err := detector.DetectDrift(ctx, manifests, cluster)
		if err != nil {
			mu.Lock()
			allDrifts = append(allDrifts, upstreamgitops.DriftResult{
				Cluster:     cluster,
				DriftType:   upstreamgitops.DriftTypeMissing,
				Differences: []string{fmt.Sprintf("Failed to detect drift: %v", err)},
			})
			mu.Unlock()
			return
		}

		mu.Lock()
		allDrifts = append(allDrifts, drifts...)
		mu.Unlock()
	})

	result.Drifts = allDrifts
	result.TotalDrifts = len(allDrifts)

	return result, nil
}

// HandleSyncFromGit syncs manifests from git to clusters.
func (s *Server) HandleSyncFromGit(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	// Validate namespace override to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := nsval.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	source := upstreamgitops.ManifestSource{
		Repo:   params.Repo,
		Path:   params.Path,
		Branch: params.Branch,
	}

	// Read manifests from git
	reader := s.NewManifestReader()
	defer reader.Cleanup()

	manifests, err := reader.ReadFromGit(ctx, source)
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
		clusters, err := s.Access.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	// Sync to each cluster
	result := &SyncResult{
		Source: source,
		DryRun: params.DryRun,
	}

	opts := upstreamgitops.SyncOptions{
		DryRun:    params.DryRun,
		Namespace: params.Namespace,
		Include:   params.Include,
		Exclude:   params.Exclude,
	}

	summaries := make([]upstreamgitops.SyncSummary, 0, len(targetClusters))
	var mu sync.Mutex

	runClusterTasks(targetClusters, func(cluster string) {
		config, err := s.Access.GetConfig(cluster)
		if err != nil {
			mu.Lock()
			summaries = append(summaries, upstreamgitops.SyncSummary{
				Cluster: cluster,
				Failed:  1,
				Results: []upstreamgitops.SyncResult{{
					Cluster: cluster,
					Action:  upstreamgitops.SyncActionFailed,
					Message: fmt.Sprintf("Failed to get config: %v", err),
				}},
			})
			mu.Unlock()
			return
		}

		syncer, err := s.NewManifestSyncer(config)
		if err != nil {
			mu.Lock()
			summaries = append(summaries, upstreamgitops.SyncSummary{
				Cluster: cluster,
				Failed:  1,
				Results: []upstreamgitops.SyncResult{{
					Cluster: cluster,
					Action:  upstreamgitops.SyncActionFailed,
					Message: fmt.Sprintf("Failed to create syncer: %v", err),
				}},
			})
			mu.Unlock()
			return
		}

		summary, err := syncer.Sync(ctx, manifests, cluster, opts)
		if err != nil {
			mu.Lock()
			summaries = append(summaries, upstreamgitops.SyncSummary{
				Cluster: cluster,
				Failed:  1,
				Results: []upstreamgitops.SyncResult{{
					Cluster: cluster,
					Action:  upstreamgitops.SyncActionFailed,
					Message: fmt.Sprintf("Failed to sync: %v", err),
				}},
			})
			mu.Unlock()
			return
		}

		mu.Lock()
		summaries = append(summaries, *summary)
		mu.Unlock()
	})

	result.Summaries = summaries
	return result, nil
}

// HandleReconcile brings clusters back in sync with git.
func (s *Server) HandleReconcile(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	return s.HandleSyncFromGit(ctx, syncArgs)
}

// HandlePreviewChanges shows what would change without applying.
func (s *Server) HandlePreviewChanges(ctx context.Context, args json.RawMessage) (interface{}, error) {
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

	return s.HandleSyncFromGit(ctx, syncArgs)
}

// Unused but kept for interface compatibility
var _ = func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
	return nil, nil
}
