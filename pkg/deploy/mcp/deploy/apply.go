package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

// boolPtr returns a pointer to a bool value
func BoolPtr(b bool) *bool {
	return &b
}

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

// applyDeployment creates or updates a deployment
func ApplyDeployment(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var deployment appsv1.Deployment
	if err := json.Unmarshal(data, &deployment); err != nil {
		return "", err
	}
	if deployment.Namespace == "" {
		deployment.Namespace = namespace
	}

	existing, err := client.AppsV1().Deployments(namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}

	data, err = json.Marshal(deployment)
	if err != nil {
		return "", err
	}

	updated, err := client.AppsV1().Deployments(namespace).Patch(ctx, deployment.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubestellar-deploy",
		Force:        BoolPtr(true),
	})
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "created", nil
	}
	if existing.ResourceVersion == updated.ResourceVersion {
		return "unchanged", nil
	}
	return "updated", nil
}

// applyService creates or updates a service
func ApplyService(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var service corev1.Service
	if err := json.Unmarshal(data, &service); err != nil {
		return "", err
	}
	if service.Namespace == "" {
		service.Namespace = namespace
	}

	existing, err := client.CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}

	data, err = json.Marshal(service)
	if err != nil {
		return "", err
	}

	updated, err := client.CoreV1().Services(namespace).Patch(ctx, service.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubestellar-deploy",
		Force:        BoolPtr(true),
	})
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "created", nil
	}
	if existing.ResourceVersion == updated.ResourceVersion {
		return "unchanged", nil
	}
	return "updated", nil
}

// applyConfigMap creates or updates a configmap
func ApplyConfigMap(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var cm corev1.ConfigMap
	if err := json.Unmarshal(data, &cm); err != nil {
		return "", err
	}
	if cm.Namespace == "" {
		cm.Namespace = namespace
	}

	existing, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cm.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}

	data, err = json.Marshal(cm)
	if err != nil {
		return "", err
	}

	updated, err := client.CoreV1().ConfigMaps(namespace).Patch(ctx, cm.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubestellar-deploy",
		Force:        BoolPtr(true),
	})
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "created", nil
	}
	if existing.ResourceVersion == updated.ResourceVersion {
		return "unchanged", nil
	}
	return "updated", nil
}

// applySecret creates or updates a secret
func ApplySecret(ctx context.Context, client kubernetes.Interface, rawObj map[string]interface{}, namespace string) (string, error) {
	data, err := json.Marshal(rawObj)
	if err != nil {
		return "", err
	}

	var secret corev1.Secret
	if err := json.Unmarshal(data, &secret); err != nil {
		return "", err
	}
	if secret.Namespace == "" {
		secret.Namespace = namespace
	}

	existing, err := client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}

	data, err = json.Marshal(secret)
	if err != nil {
		return "", err
	}

	updated, err := client.CoreV1().Secrets(namespace).Patch(ctx, secret.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubestellar-deploy",
		Force:        BoolPtr(true),
	})
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "created", nil
	}
	if existing.ResourceVersion == updated.ResourceVersion {
		return "unchanged", nil
	}
	return "updated", nil
}
