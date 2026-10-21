package helm

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Server) handleHelmUninstall(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ReleaseName string   `json:"release_name"`
		Namespace   string   `json:"namespace"`
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
		result := s.helmUninstall(ctx, cluster, params.ReleaseName, namespace, params.DryRun)
		results = append(results, result)
	}

	return aggregateHelmResults(targetClusters, results, params.DryRun, "uninstalled", "would-uninstall"), nil
}

// helmUninstall runs helm uninstall for a single cluster
func (s *Server) helmUninstall(ctx context.Context, cluster, releaseName, namespace string, dryRun bool) HelmResult {
	if dryRun {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-uninstall",
			Message:     fmt.Sprintf("Would uninstall release %s from namespace %s", releaseName, namespace),
		}
	}

	cmdArgs := []string{"uninstall", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	stdout, stderr, err := execHelm(ctx, nil, cmdArgs...)

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
		Status:      "uninstalled",
		Message:     stdout.String(),
	}
}
