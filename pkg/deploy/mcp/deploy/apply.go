package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

// applyManifest applies a manifest to a cluster
func ApplyManifest(ctx context.Context, d Deps, client kubernetes.Interface, clusterName, manifest string, dryRun bool) ([]DeployResult, error) {
	_ = client

	var results []DeployResult
	if dryRun {
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
		for {
			var rawObj map[string]interface{}
			if err := decoder.Decode(&rawObj); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("failed to decode manifest: %w", err)
			}
			if rawObj == nil {
				continue
			}

			kind, _ := rawObj["kind"].(string)
			metadata, _ := rawObj["metadata"].(map[string]interface{})
			name, _ := metadata["name"].(string)
			namespace, _ := metadata["namespace"].(string)
			if namespace == "" {
				namespace = "default"
			}

			// Validate namespace from manifest to prevent access to system namespaces (#377).
			if namespace != "" {
				if err := nsval.ValidateNamespace(namespace); err != nil {
					return []DeployResult{{
						Cluster: clusterName, Status: "failed",
						Message: fmt.Sprintf("invalid namespace in manifest: %v", err),
					}}, nil
				}
			}

			resourceName := fmt.Sprintf("%s/%s", kind, name)
			results = append(results, DeployResult{
				Cluster:  clusterName,
				Resource: resourceName,
				Status:   "would-apply",
				Message:  fmt.Sprintf("Would apply %s to namespace %s", resourceName, namespace),
			})
		}
		return results, nil
	}

	reader := d.GetManifestReader()
	manifests, err := reader.ReadFromReader(strings.NewReader(manifest))
	if err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	config, err := d.GetConfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get config for cluster %s: %w", clusterName, err)
	}

	syncer, err := d.GetManifestSyncer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest syncer: %w", err)
	}

	summary, err := syncer.Sync(ctx, manifests, clusterName, gitops.SyncOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to apply manifest: %w", err)
	}

	for _, result := range summary.Results {
		results = append(results, DeployResult{
			Cluster:  clusterName,
			Resource: fmt.Sprintf("%s/%s", result.Kind, result.Name),
			Status:   string(result.Action),
			Message:  result.Message,
		})
	}

	return results, nil
}
