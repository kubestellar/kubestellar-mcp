package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"k8s.io/client-go/rest"
)

type manifestReader interface {
	ReadFromGit(ctx context.Context, source gitops.ManifestSource) ([]gitops.Manifest, error)
	Cleanup()
}

type driftDetector interface {
	IsManifestClusterScoped(manifest gitops.Manifest) bool
	DetectDrift(ctx context.Context, manifests []gitops.Manifest, clusterName string) ([]gitops.DriftResult, error)
}

func (s *Server) newManifestReader() manifestReader {
	if s.manifestReaderFactory != nil {
		return s.manifestReaderFactory()
	}
	return gitops.NewManifestReader()
}

func (s *Server) newDriftDetector(config *rest.Config) (driftDetector, error) {
	if s.driftDetectorFactory != nil {
		return s.driftDetectorFactory(config)
	}
	return gitops.NewDriftDetector(config)
}

func (s *Server) toolDetectDrift(ctx context.Context, args map[string]interface{}) (string, bool) {
	repoURL, _ := args["repo_url"].(string)
	path, _ := args["path"].(string)
	branch, _ := args["branch"].(string)
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	if repoURL == "" {
		return "repo_url is required", true
	}

	// Get REST config for the cluster
	restConfig, err := s.getRestConfigForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client config: %v", err), true
	}

	// Determine cluster name for output
	clusterName := cluster
	if clusterName == "" {
		clusterName = "current-context"
	}

	// Read manifests from git
	reader := s.newManifestReader()
	defer reader.Cleanup()

	source := gitops.ManifestSource{
		Repo:   repoURL,
		Path:   path,
		Branch: branch,
	}

	manifests, err := reader.ReadFromGit(ctx, source)
	if err != nil {
		return fmt.Sprintf("Failed to read manifests from git: %v", err), true
	}

	if len(manifests) == 0 {
		return fmt.Sprintf("No manifests found in %s (path: %s)", repoURL, path), false
	}

	// Create drift detector
	detector, err := s.newDriftDetector(restConfig)
	if err != nil {
		return fmt.Sprintf("Failed to create drift detector: %v", err), true
	}

	// Filter manifests by namespace if specified
	if namespace != "" {
		var filtered []gitops.Manifest
		for _, m := range manifests {
			if m.GetNamespace() == namespace || detector.IsManifestClusterScoped(m) {
				filtered = append(filtered, m)
			}
		}
		manifests = filtered
	}

	// Detect drift
	drifts, err := detector.DetectDrift(ctx, manifests, clusterName)
	if err != nil {
		return fmt.Sprintf("Failed to detect drift: %v", err), true
	}

	// Build response
	var sb strings.Builder
	sb.WriteString("# GitOps Drift Detection\n\n")
	_, _ = fmt.Fprintf(&sb, "**Repository:** %s\n", repoURL)
	if path != "" {
		_, _ = fmt.Fprintf(&sb, "**Path:** %s\n", path)
	}
	if branch != "" {
		_, _ = fmt.Fprintf(&sb, "**Branch:** %s\n", branch)
	}
	_, _ = fmt.Fprintf(&sb, "**Cluster:** %s\n", clusterName)
	_, _ = fmt.Fprintf(&sb, "**Manifests Found:** %d\n\n", len(manifests))

	if len(drifts) == 0 {
		sb.WriteString("✅ **No drift detected** - cluster state matches Git manifests\n")

		// Also return JSON for programmatic parsing
		result := map[string]interface{}{
			"drifted":   false,
			"resources": []interface{}{},
			"summary": map[string]interface{}{
				"total":    len(manifests),
				"synced":   len(manifests),
				"drifted":  0,
				"missing":  0,
				"modified": 0,
			},
		}
		jsonBytes, _ := json.MarshalIndent(result, "", "  ")
		sb.WriteString("\n```json\n")
		sb.WriteString(string(jsonBytes))
		sb.WriteString("\n```\n")

		return sb.String(), false
	}

	// Count by drift type
	missing := 0
	modified := 0
	for _, d := range drifts {
		switch d.DriftType {
		case gitops.DriftTypeMissing:
			missing++
		case gitops.DriftTypeModified:
			modified++
		}
	}

	_, _ = fmt.Fprintf(&sb, "⚠️ **Drift detected**: %d resource(s) out of sync\n\n", len(drifts))
	sb.WriteString("## Summary\n\n")
	_, _ = fmt.Fprintf(&sb, "- Missing from cluster: %d\n", missing)
	_, _ = fmt.Fprintf(&sb, "- Modified in cluster: %d\n", modified)
	sb.WriteString("\n## Details\n\n")

	// Build JSON resources array
	resources := make([]map[string]interface{}, 0, len(drifts))

	for _, d := range drifts {
		icon := "📝"
		if d.DriftType == gitops.DriftTypeMissing {
			icon = "❌"
		}

		_, _ = fmt.Fprintf(&sb, "### %s %s/%s\n", icon, d.Kind, d.Name)
		if d.Namespace != "" {
			_, _ = fmt.Fprintf(&sb, "**Namespace:** %s\n", d.Namespace)
		}
		_, _ = fmt.Fprintf(&sb, "**Type:** %s\n", d.DriftType)

		if len(d.Differences) > 0 {
			sb.WriteString("**Differences:**\n")
			for _, diff := range d.Differences {
				_, _ = fmt.Fprintf(&sb, "- %s\n", diff)
			}
		}
		sb.WriteString("\n")

		// Build resource for JSON output
		resource := map[string]interface{}{
			"kind":      d.Kind,
			"name":      d.Name,
			"namespace": d.Namespace,
			"driftType": string(d.DriftType),
		}
		if len(d.Differences) > 0 {
			resource["field"] = d.Differences[0]
			resource["differences"] = d.Differences
		}
		if d.GitValue != nil {
			resource["gitValue"] = fmt.Sprintf("%v", d.GitValue)
		}
		if d.ClusterValue != nil {
			resource["clusterValue"] = fmt.Sprintf("%v", d.ClusterValue)
		}
		resources = append(resources, resource)
	}

	// Add JSON for programmatic parsing
	result := map[string]interface{}{
		"drifted":   true,
		"resources": resources,
		"summary": map[string]interface{}{
			"total":    len(manifests),
			"synced":   len(manifests) - len(drifts),
			"drifted":  len(drifts),
			"missing":  missing,
			"modified": modified,
		},
	}
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	sb.WriteString("\n```json\n")
	sb.WriteString(string(jsonBytes))
	sb.WriteString("\n```\n")

	return sb.String(), false
}
