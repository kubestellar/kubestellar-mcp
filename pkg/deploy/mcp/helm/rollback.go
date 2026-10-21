package helm

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Server) handleHelmRollback(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ReleaseName string   `json:"release_name"`
		Namespace   string   `json:"namespace"`
		Revision    int      `json:"revision"`
		DryRun      bool     `json:"dry_run"`
		Clusters    []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	namespace, targetClusters, err := s.prepareHelmReleaseRequest(ctx, params.ReleaseName, params.Namespace, params.Clusters)
	if err != nil {
		return nil, err
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmRollback(ctx, cluster, params.ReleaseName, namespace, params.Revision, params.DryRun)
		results = append(results, result)
	}

	return aggregateHelmResults(targetClusters, results, params.DryRun, "rolled-back", "would-rollback"), nil
}

// helmRollback runs helm rollback for a single cluster
func (s *Server) helmRollback(ctx context.Context, cluster, releaseName, namespace string, revision int, dryRun bool) HelmResult {
	cmdArgs := []string{"rollback", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	if revision > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("%d", revision))
	}

	if dryRun {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	stdout, stderr, err := execHelm(ctx, nil, cmdArgs...)

	if dryRun && err == nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-rollback",
			Message:     stdout.String(),
		}
	}

	if err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     stderr.String(),
		}
	}

	return HelmResult{
		Cluster:     cluster,
		ReleaseName: releaseName,
		Namespace:   namespace,
		Status:      "rolled-back",
		Message:     stdout.String(),
	}
}
