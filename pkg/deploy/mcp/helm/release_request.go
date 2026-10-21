package helm

import (
	"context"
	"fmt"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
)

// prepareHelmReleaseRequest validates the release_name/namespace/clusters
// arguments and resolves the target clusters for an operation that acts on
// an existing release (helm_rollback, helm_uninstall). When the caller does
// not specify clusters explicitly, it falls back to discovering the clusters
// where the release actually exists. This is shared because both handlers
// need identical validation and the same "find where this release lives"
// fallback; see #1317.
func (s *Server) prepareHelmReleaseRequest(ctx context.Context, releaseName, namespace string, clusters []string) (string, []string, error) {
	if releaseName == "" {
		return "", nil, fmt.Errorf("release_name is required")
	}

	if namespace == "" {
		namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := nsval.ValidateNamespace(namespace); err != nil {
		return "", nil, fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", releaseName); err != nil {
		return "", nil, err
	}
	if err := validateHelmIdentifier("namespace", namespace); err != nil {
		return "", nil, err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(clusters); err != nil {
		return "", nil, err
	}

	// Get target clusters
	targetClusters := clusters
	if len(targetClusters) == 0 {
		discovered, err := s.Access.DiscoverClusters()
		if err != nil {
			return "", nil, err
		}
		for _, c := range discovered {
			if s.helmReleaseExists(ctx, c.Name, releaseName, namespace) {
				targetClusters = append(targetClusters, c.Name)
			}
		}
	}

	if len(targetClusters) == 0 {
		return "", nil, fmt.Errorf("release %s not found in any cluster", releaseName)
	}

	return namespace, targetClusters, nil
}

// aggregateHelmResults builds the standard helm tool-handler response: the
// resolved target clusters, how many of results succeeded (per
// successStatuses), and the raw per-cluster results. Shared by
// handleHelmInstall, handleHelmRollback, and handleHelmUninstall, which only
// differ in which HelmResult.Status values count as a success; see #1317.
func aggregateHelmResults(targetClusters []string, results []HelmResult, dryRun bool, successStatuses ...string) map[string]interface{} {
	success := make(map[string]bool, len(successStatuses))
	for _, st := range successStatuses {
		success[st] = true
	}

	successCount := 0
	for _, r := range results {
		if success[r.Status] {
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         dryRun,
	}
}
