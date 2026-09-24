package kubectl

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GetGVR returns the GroupVersionResource for common Kubernetes kinds.
// The second return value indicates whether the resource is namespaced.
func GetGVR(kind string) (schema.GroupVersionResource, bool) {
	kindLower := strings.ToLower(kind)
	switch kindLower {
	case "pod", "pods":
		return schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true
	case "service", "services":
		return schema.GroupVersionResource{Version: "v1", Resource: "services"}, true
	case "configmap", "configmaps":
		return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true
	case "secret", "secrets":
		return schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true
	case "namespace", "namespaces":
		return schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, false
	case "serviceaccount", "serviceaccounts":
		return schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, true
	case "persistentvolumeclaim", "persistentvolumeclaims":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, true
	case "persistentvolume", "persistentvolumes":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, false

	case "deployment", "deployments":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true
	case "statefulset", "statefulsets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true
	case "daemonset", "daemonsets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true
	case "replicaset", "replicasets":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, true

	case "job", "jobs":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true
	case "cronjob", "cronjobs":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true

	case "ingress", "ingresses":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true
	case "networkpolicy", "networkpolicies":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, true

	case "role", "roles":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true
	case "rolebinding", "rolebindings":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true
	case "clusterrole", "clusterroles":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false
	case "clusterrolebinding", "clusterrolebindings":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false

	case "horizontalpodautoscaler", "horizontalpodautoscalers", "hpa":
		return schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, true

	default:
		return schema.GroupVersionResource{}, false
	}
}
