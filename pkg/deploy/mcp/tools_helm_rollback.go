package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
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

	if params.ReleaseName == "" {
		return nil, fmt.Errorf("release_name is required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := server.ValidateNamespace(params.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", params.ReleaseName); err != nil {
		return nil, err
	}
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			if s.helmReleaseExists(ctx, c.Name, params.ReleaseName, params.Namespace) {
				targetClusters = append(targetClusters, c.Name)
			}
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("release %s not found in any cluster", params.ReleaseName)
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmRollback(ctx, cluster, params.ReleaseName, params.Namespace, params.Revision, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "rolled-back" || r.Status == "would-rollback" {
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
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

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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

// helmToolDefs returns the tool definitions handled by this file.
