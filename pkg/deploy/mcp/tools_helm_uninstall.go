package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
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

	if params.ReleaseName == "" {
		return nil, fmt.Errorf("release_name is required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := nsval.ValidateNamespace(params.Namespace); err != nil {
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
		// Find clusters where release exists
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
		result := s.helmUninstall(ctx, cluster, params.ReleaseName, params.Namespace, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "uninstalled" || r.Status == "would-uninstall" {
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

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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

// handleHelmList lists Helm releases across clusters
