package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
)

func (s *Server) handleHelmList(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Namespace string   `json:"namespace"`
		AllNs     bool     `json:"all_namespaces"`
		Filter    string   `json:"filter"`
		Clusters  []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Validate namespace to prevent flag injection (#344).
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := server.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Validate filter to prevent flag injection (#344).
	if params.Filter != "" && strings.HasPrefix(params.Filter, "-") {
		return nil, fmt.Errorf("filter %q must not begin with '-' (possible flag injection)", params.Filter)
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

	allReleases := make(map[string][]HelmReleaseInfo)
	for _, cluster := range targetClusters {
		releases := s.helmList(ctx, cluster, params.Namespace, params.AllNs, params.Filter)
		if len(releases) > 0 {
			allReleases[cluster] = releases
		}
	}

	totalReleases := 0
	for _, releases := range allReleases {
		totalReleases += len(releases)
	}

	return map[string]interface{}{
		"clusters":      targetClusters,
		"releases":      allReleases,
		"totalReleases": totalReleases,
	}, nil
}

// helmList runs helm list for a single cluster
func (s *Server) helmList(ctx context.Context, cluster, namespace string, allNs bool, filter string) []HelmReleaseInfo {
	cmdArgs := []string{"list", "--kube-context", cluster, "-o", "json"}

	if allNs {
		cmdArgs = append(cmdArgs, "--all-namespaces")
	} else if namespace != "" {
		cmdArgs = append(cmdArgs, "--namespace", namespace)
	}

	if filter != "" {
		cmdArgs = append(cmdArgs, "--filter", filter)
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil
	}

	var releases []HelmReleaseInfo
	if err := json.Unmarshal(stdout.Bytes(), &releases); err != nil {
		return nil
	}
	return releases
}

// helmReleaseExists checks if a release exists in a cluster
func (s *Server) helmReleaseExists(ctx context.Context, cluster, releaseName, namespace string) bool {
	cmdArgs := []string{"status", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	return cmd.Run() == nil
}

// handleHelmRollback rolls back a Helm release to a previous revision
