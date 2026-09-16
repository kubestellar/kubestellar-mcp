package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
	"k8s.io/client-go/kubernetes"
)

// DeleteResult represents the result of a delete operation
type DeleteResult struct {
	Cluster   string `json:"cluster"`
	Resource  string `json:"resource"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // deleted, not-found, failed
	Message   string `json:"message,omitempty"`
}

// ApplyResult represents the result of an apply operation
type ApplyResult struct {
	Cluster   string `json:"cluster"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"` // created, updated, unchanged, failed
	Message   string `json:"message,omitempty"`
}

// handleDeleteResource deletes a resource from clusters
func (s *Server) handleDeleteResource(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Kind      string   `json:"kind"`
		Name      string   `json:"name"`
		Namespace string   `json:"namespace"`
		Clusters  []string `json:"clusters"`
		DryRun    bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Kind == "" || params.Name == "" {
		return nil, fmt.Errorf("kind and name are required")
	}

	if isSensitiveKind(params.Kind) {
		return nil, sensitiveKindError(params.Kind)
	}

	// Validate namespace to prevent access to system namespaces (#377).
	// For kind Namespace the protected value is name (cluster-scoped), not the
	// namespace field — otherwise deleting kube-system etc. would be allowed.
	if isNamespaceKind(params.Kind) {
		if err := server.ValidateNamespace(params.Name); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	} else if params.Namespace != "" {
		if err := server.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
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

	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.deleteResourceInCluster(ctx, client, clusterName, params.Kind, params.Name, params.Namespace, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var deleteResults []DeleteResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			deleteResults = append(deleteResults, DeleteResult{
				Cluster:  result.Cluster,
				Resource: params.Kind,
				Name:     params.Name,
				Status:   "failed",
				Message:  result.Error,
			})
		} else if dr, ok := result.Result.(DeleteResult); ok {
			deleteResults = append(deleteResults, dr)
			if dr.Status == "deleted" || dr.Status == "would-delete" {
				successCount++
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        deleteResults,
		"dryRun":         params.DryRun,
	}, nil
}

// handleKubectlApply applies any Kubernetes resource using dynamic client
func (s *Server) handleKubectlApply(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Manifest string   `json:"manifest"`
		Clusters []string `json:"clusters"`
		DryRun   bool     `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Manifest == "" {
		return nil, fmt.Errorf("manifest is required")
	}

	for _, doc := range strings.Split(params.Manifest, "---") {
		if kind, blocked := manifestSensitiveKind(doc); blocked {
			return nil, sensitiveKindError(kind)
		}
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

	results, err := s.executor.ExecuteOnSelected(ctx, targetClusters, func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.applyManifestDynamic(ctx, clusterName, params.Manifest, params.DryRun)
	})
	if err != nil {
		return nil, err
	}

	var applyResults []ApplyResult
	successCount := 0
	for _, result := range results {
		if result.Error != "" {
			applyResults = append(applyResults, ApplyResult{
				Cluster: result.Cluster,
				Status:  "failed",
				Message: result.Error,
			})
		} else if ar, ok := result.Result.([]ApplyResult); ok {
			applyResults = append(applyResults, ar...)
			for _, r := range ar {
				if r.Status == "created" || r.Status == "updated" || r.Status == "would-apply" {
					successCount++
				}
			}
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        applyResults,
		"dryRun":         params.DryRun,
	}, nil
}

// kubectlToolDefs returns the tool definitions handled by this file.
func (s *Server) kubectlToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "delete_resource",
			Description: "Delete a Kubernetes resource from clusters. Supports all common resource types.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Resource kind (e.g., Deployment, Service, Pod, ConfigMap, Secret, StatefulSet, DaemonSet, Job, CronJob, Ingress, PVC, Namespace, ServiceAccount, Role, RoleBinding, ClusterRole, ClusterRoleBinding)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace (default: default, ignored for cluster-scoped resources)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"kind", "name"},
			},
			Handler: s.handleDeleteResource,
		},
		{
			Name:        "kubectl_apply",
			Description: "Apply any Kubernetes manifest to clusters. Supports all resource types using dynamic client.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"manifest": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes manifest (YAML or JSON)",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview changes without applying",
					},
					"clusters": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Target clusters (all clusters if not specified)",
					},
				},
				"required": []string{"manifest"},
			},
			Handler: s.handleKubectlApply,
		},
	}
}
