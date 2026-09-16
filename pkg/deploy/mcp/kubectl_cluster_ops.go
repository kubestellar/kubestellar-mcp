package mcp

import (
	"context"
	"fmt"
	"strings"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// deleteResourceInCluster deletes a resource in a single cluster.
// The client parameter uses kubernetes.Interface (not *kubernetes.Clientset)
// so callers can pass a fake clientset from k8s.io/client-go/kubernetes/fake in tests.
func (s *Server) deleteResourceInCluster(ctx context.Context, client kubernetes.Interface, clusterName, kind, name, namespace string, dryRun bool) (DeleteResult, error) {
	result := DeleteResult{
		Cluster:   clusterName,
		Resource:  kind,
		Name:      name,
		Namespace: namespace,
	}

	if dryRun {
		result.Status = "would-delete"
		result.Message = fmt.Sprintf("Would delete %s/%s", kind, name)
		return result, nil
	}

	var err error
	ns := namespace
	if ns == "" {
		ns = "default"
	}

	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		err = client.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "service", "services", "svc":
		err = client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "configmap", "configmaps", "cm":
		err = client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "secret", "secrets":
		err = client.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "pod", "pods":
		err = client.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "statefulset", "statefulsets", "sts":
		err = client.AppsV1().StatefulSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "daemonset", "daemonsets", "ds":
		err = client.AppsV1().DaemonSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "job", "jobs":
		err = client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "cronjob", "cronjobs":
		err = client.BatchV1().CronJobs(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "ingress", "ingresses", "ing":
		err = client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		err = client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "namespace", "namespaces", "ns":
		err = client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	case "serviceaccount", "serviceaccounts", "sa":
		err = client.CoreV1().ServiceAccounts(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "role", "roles":
		err = client.RbacV1().Roles(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "rolebinding", "rolebindings":
		err = client.RbacV1().RoleBindings(ns).Delete(ctx, name, metav1.DeleteOptions{})
	case "clusterrole", "clusterroles":
		err = client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
	case "clusterrolebinding", "clusterrolebindings":
		err = client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
	default:
		result.Status = "failed"
		result.Message = fmt.Sprintf("Unsupported resource kind: %s", kind)
		return result, nil
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			result.Status = "not-found"
			result.Message = fmt.Sprintf("%s/%s not found", kind, name)
		} else {
			result.Status = "failed"
			result.Message = err.Error()
		}
	} else {
		result.Status = "deleted"
	}

	return result, nil
}

// applyManifestDynamic applies manifests using the dynamic client for any resource type
func (s *Server) applyManifestDynamic(ctx context.Context, clusterName, manifest string, dryRun bool) ([]ApplyResult, error) {
	var results []ApplyResult

	// Get the dynamic client for this cluster
	config, err := s.manager.GetConfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get config for cluster %s: %w", clusterName, err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Parse YAML documents
	docs := strings.Split(manifest, "---")
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Parse as unstructured
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(yamlToJSON(doc))); err != nil {
			// Try YAML parsing
			if err := unstructuredFromYAML(doc, obj); err != nil {
				results = append(results, ApplyResult{
					Cluster: clusterName,
					Status:  "failed",
					Message: fmt.Sprintf("failed to parse manifest: %v", err),
				})
				continue
			}
		}

		kind := obj.GetKind()
		name := obj.GetName()
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = "default"
		}

		// Validate namespace from manifest to prevent access to system namespaces (#377).
		// For kind Namespace the protected value is metadata.name (cluster-scoped).
		// Append failure and continue rather than returning early, so prior results are preserved (#626).
		if isNamespaceKind(kind) {
			if err := server.ValidateNamespace(name); err != nil {
				results = append(results, ApplyResult{Cluster: clusterName, Status: "failed",
					Message: fmt.Sprintf("invalid namespace in manifest: %v", err)})
				continue
			}
		} else if namespace != "" {
			if err := server.ValidateNamespace(namespace); err != nil {
				results = append(results, ApplyResult{Cluster: clusterName, Status: "failed",
					Message: fmt.Sprintf("invalid namespace in manifest: %v", err)})
				continue
			}
		}

		result := ApplyResult{
			Cluster:   clusterName,
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		}

		if dryRun {
			result.Status = "would-apply"
			result.Message = fmt.Sprintf("Would apply %s/%s to namespace %s", kind, name, namespace)
			results = append(results, result)
			continue
		}

		// Get the GVR for this resource
		gvr, namespaced := getGVR(kind)
		if gvr.Resource == "" {
			result.Status = "failed"
			result.Message = fmt.Sprintf("unknown resource kind: %s", kind)
			results = append(results, result)
			continue
		}

		// Apply the resource
		var resourceClient dynamic.ResourceInterface
		if namespaced {
			resourceClient = dynClient.Resource(gvr).Namespace(namespace)
		} else {
			resourceClient = dynClient.Resource(gvr)
		}

		// Try to get existing
		existing, err := resourceClient.Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			// Update
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, err = resourceClient.Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				result.Status = "failed"
				result.Message = err.Error()
			} else {
				result.Status = "updated"
			}
		} else {
			// Create
			_, err = resourceClient.Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				result.Status = "failed"
				result.Message = err.Error()
			} else {
				result.Status = "created"
			}
		}

		results = append(results, result)
	}

	return results, nil
}
