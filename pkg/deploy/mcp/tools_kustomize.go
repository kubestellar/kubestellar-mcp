package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KustomizeResult represents the result of a kustomize operation
type KustomizeResult struct {
	Cluster  string `json:"cluster"`
	Path     string `json:"path"`
	Status   string `json:"status"` // applied, failed, would-apply
	Resources int    `json:"resources"`
	Message  string `json:"message,omitempty"`
}

// handleKustomizeBuild builds kustomize output without applying
func (s *Server) handleKustomizeBuild(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Verify path exists and contains kustomization.yaml
	if _, err := os.Stat(filepath.Join(params.Path, "kustomization.yaml")); os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join(params.Path, "kustomization.yml")); os.IsNotExist(err) {
			return nil, fmt.Errorf("no kustomization.yaml or kustomization.yml found in %s", params.Path)
		}
	}

	// Run kustomize build
	cmd := exec.CommandContext(ctx, "kustomize", "build", params.Path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Try kubectl kustomize as fallback
		cmd = exec.CommandContext(ctx, "kubectl", "kustomize", params.Path)
		stdout.Reset()
		stderr.Reset()
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("kustomize build failed: %s", stderr.String())
		}
	}

	// Count resources in output
	resourceCount := strings.Count(stdout.String(), "kind:")

	return map[string]interface{}{
		"path":      params.Path,
		"output":    stdout.String(),
		"resources": resourceCount,
	}, nil
}

// handleKustomizeApply applies kustomize output to clusters
func (s *Server) handleKustomizeApply(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Path     string   `json:"path"`
		Clusters []string `json:"clusters"`
		DryRun   bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Build kustomize output first
	buildResult, err := s.handleKustomizeBuild(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("kustomize build failed: %w", err)
	}

	buildMap := buildResult.(map[string]interface{})
	manifest := buildMap["output"].(string)
	resourceCount := buildMap["resources"].(int)

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

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("no clusters available")
	}

	var results []KustomizeResult
	for _, cluster := range targetClusters {
		result := s.applyKustomize(ctx, cluster, params.Path, manifest, resourceCount, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "applied" || r.Status == "would-apply" {
			successCount++
		}
	}

	return map[string]interface{}{
		"path":           params.Path,
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// applyKustomize applies kustomize manifest to a single cluster
func (s *Server) applyKustomize(ctx context.Context, cluster, path, manifest string, resourceCount int, dryRun bool) KustomizeResult {
	result := KustomizeResult{
		Cluster:   cluster,
		Path:      path,
		Resources: resourceCount,
	}

	if dryRun {
		result.Status = "would-apply"
		result.Message = fmt.Sprintf("Would apply %d resources from %s", resourceCount, path)
		return result
	}

	// Apply using kubectl apply -f -
	cmdArgs := []string{"apply", "-f", "-", "--context", cluster}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	cmd.Stdin = strings.NewReader(manifest)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		result.Status = "failed"
		result.Message = stderr.String()
		return result
	}

	result.Status = "applied"
	result.Message = stdout.String()
	return result
}

// handleKustomizeDelete deletes resources from kustomize output
func (s *Server) handleKustomizeDelete(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Path     string   `json:"path"`
		Clusters []string `json:"clusters"`
		DryRun   bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Build kustomize output first
	buildResult, err := s.handleKustomizeBuild(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("kustomize build failed: %w", err)
	}

	buildMap := buildResult.(map[string]interface{})
	manifest := buildMap["output"].(string)
	resourceCount := buildMap["resources"].(int)

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

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("no clusters available")
	}

	var results []KustomizeResult
	for _, cluster := range targetClusters {
		result := s.deleteKustomize(ctx, cluster, params.Path, manifest, resourceCount, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "deleted" || r.Status == "would-delete" {
			successCount++
		}
	}

	return map[string]interface{}{
		"path":           params.Path,
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// deleteKustomize deletes resources from a single cluster
func (s *Server) deleteKustomize(ctx context.Context, cluster, path, manifest string, resourceCount int, dryRun bool) KustomizeResult {
	result := KustomizeResult{
		Cluster:   cluster,
		Path:      path,
		Resources: resourceCount,
	}

	if dryRun {
		result.Status = "would-delete"
		result.Message = fmt.Sprintf("Would delete %d resources from %s", resourceCount, path)
		return result
	}

	// Delete using kubectl delete -f -
	cmdArgs := []string{"delete", "-f", "-", "--context", cluster, "--ignore-not-found"}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	cmd.Stdin = strings.NewReader(manifest)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		result.Status = "failed"
		result.Message = stderr.String()
		return result
	}

	result.Status = "deleted"
	result.Message = stdout.String()
	return result
}
