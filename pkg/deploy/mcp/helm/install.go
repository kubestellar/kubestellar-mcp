package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func (s *Server) handleHelmInstall(ctx context.Context, args json.RawMessage) (interface{}, error) {
	params, targetClusters, err := s.parseHelmInstallArgs(args)
	if err != nil {
		return nil, err
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmInstall(ctx, cluster, params.ReleaseName, params.Chart, params.Namespace,
			params.Values, params.ValuesYAML, params.Version, params.Repo, params.Wait, params.Timeout, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "installed" || r.Status == "upgraded" || r.Status == "would-install" {
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

// helmInstallParams holds the parsed and validated arguments for a
// helm_install tool call.
type helmInstallParams struct {
	ReleaseName string            `json:"release_name"`
	Chart       string            `json:"chart"`
	Namespace   string            `json:"namespace"`
	Values      map[string]string `json:"values"`
	ValuesYAML  string            `json:"values_yaml"`
	Version     string            `json:"version"`
	Repo        string            `json:"repo"`
	Wait        bool              `json:"wait"`
	Timeout     string            `json:"timeout"`
	DryRun      bool              `json:"dry_run"`
	Clusters    []string          `json:"clusters"`
}

// parseHelmInstallArgs unmarshals and validates the raw arguments for
// handleHelmInstall, and resolves the target clusters to operate on. It
// centralizes all of the request validation so handleHelmInstall itself only
// has to worry about fan-out and result aggregation.
func (s *Server) parseHelmInstallArgs(args json.RawMessage) (helmInstallParams, []string, error) {
	var params helmInstallParams
	if err := json.Unmarshal(args, &params); err != nil {
		return params, nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ReleaseName == "" || params.Chart == "" {
		return params, nil, fmt.Errorf("release_name and chart are required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	if err := validateHelmInstallParams(params); err != nil {
		return params, nil, err
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.Access.DiscoverClusters()
		if err != nil {
			return params, nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	if len(targetClusters) == 0 {
		return params, nil, fmt.Errorf("no clusters available")
	}

	return params, targetClusters, nil
}

// helmInstall runs helm install/upgrade for a single cluster
func (s *Server) helmInstall(ctx context.Context, cluster, releaseName, chart, namespace string,
	values map[string]string, valuesYAML, version, repo string, wait bool, timeout string, dryRun bool) HelmResult {

	// Pre-exec DNS re-validation: re-resolve hostnames immediately before
	// exec to close the TOCTOU gap between validateHelmRepoURL/validateHelmChartRef
	// (which resolve during input validation) and the helm subprocess (which
	// resolves independently). If DNS has rebind to a blocked IP between
	// validation and now, abort. See #275.
	if err := revalidateHelmHosts(chart, repo); err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     fmt.Sprintf("pre-exec SSRF re-check failed (possible DNS rebinding): %v", err),
		}
	}

	cmdArgs := []string{"upgrade", "--install", releaseName, chart,
		"--namespace", namespace,
		"--create-namespace",
		"--kube-context", cluster,
	}

	// Add repo if specified (already validated by handleHelmInstall)
	if repo != "" {
		cmdArgs = append(cmdArgs, "--repo", repo)
	}

	// Add version if specified
	if version != "" {
		cmdArgs = append(cmdArgs, "--version", version)
	}

	// Add --set values
	for k, v := range values {
		cmdArgs = append(cmdArgs, "--set", fmt.Sprintf("%s=%s", k, v))
	}

	// Add values YAML if specified
	if valuesYAML != "" {
		cmdArgs = append(cmdArgs, "--values", "-")
	}

	if wait {
		cmdArgs = append(cmdArgs, "--wait")
	}

	if timeout != "" {
		cmdArgs = append(cmdArgs, "--timeout", timeout)
	}

	if dryRun {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if valuesYAML != "" {
		cmd.Stdin = strings.NewReader(valuesYAML)
	}

	err := cmd.Run()

	if dryRun && err == nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-install",
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

	// Determine if it was install or upgrade from output
	status := "installed"
	if strings.Contains(stdout.String(), "has been upgraded") {
		status = "upgraded"
	}

	return HelmResult{
		Cluster:     cluster,
		ReleaseName: releaseName,
		Namespace:   namespace,
		Status:      status,
		Message:     stdout.String(),
	}
}
