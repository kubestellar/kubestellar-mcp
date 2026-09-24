package kubectl

import (
	"testing"
)

func TestGetGVR(t *testing.T) {
	tests := []struct {
		kind       string
		group      string
		version    string
		resource   string
		namespaced bool
	}{
		// Core v1 (namespaced)
		{kind: "pod", version: "v1", resource: "pods", namespaced: true},
		{kind: "Pods", version: "v1", resource: "pods", namespaced: true},
		{kind: "service", version: "v1", resource: "services", namespaced: true},
		{kind: "Services", version: "v1", resource: "services", namespaced: true},
		{kind: "configmap", version: "v1", resource: "configmaps", namespaced: true},
		{kind: "ConfigMaps", version: "v1", resource: "configmaps", namespaced: true},
		{kind: "secret", version: "v1", resource: "secrets", namespaced: true},
		{kind: "Secrets", version: "v1", resource: "secrets", namespaced: true},
		{kind: "serviceaccount", version: "v1", resource: "serviceaccounts", namespaced: true},
		{kind: "ServiceAccounts", version: "v1", resource: "serviceaccounts", namespaced: true},
		{kind: "persistentvolumeclaim", version: "v1", resource: "persistentvolumeclaims", namespaced: true},
		{kind: "PersistentVolumeClaims", version: "v1", resource: "persistentvolumeclaims", namespaced: true},
		// Core v1 (cluster-scoped)
		{kind: "namespace", version: "v1", resource: "namespaces", namespaced: false},
		{kind: "Namespaces", version: "v1", resource: "namespaces", namespaced: false},
		{kind: "persistentvolume", version: "v1", resource: "persistentvolumes", namespaced: false},
		{kind: "PersistentVolumes", version: "v1", resource: "persistentvolumes", namespaced: false},
		// Apps v1
		{kind: "Deployment", group: "apps", version: "v1", resource: "deployments", namespaced: true},
		{kind: "deployments", group: "apps", version: "v1", resource: "deployments", namespaced: true},
		{kind: "statefulset", group: "apps", version: "v1", resource: "statefulsets", namespaced: true},
		{kind: "StatefulSets", group: "apps", version: "v1", resource: "statefulsets", namespaced: true},
		{kind: "daemonset", group: "apps", version: "v1", resource: "daemonsets", namespaced: true},
		{kind: "DaemonSets", group: "apps", version: "v1", resource: "daemonsets", namespaced: true},
		{kind: "replicaset", group: "apps", version: "v1", resource: "replicasets", namespaced: true},
		{kind: "ReplicaSets", group: "apps", version: "v1", resource: "replicasets", namespaced: true},
		// Batch v1
		{kind: "job", group: "batch", version: "v1", resource: "jobs", namespaced: true},
		{kind: "Jobs", group: "batch", version: "v1", resource: "jobs", namespaced: true},
		{kind: "cronjob", group: "batch", version: "v1", resource: "cronjobs", namespaced: true},
		{kind: "CronJobs", group: "batch", version: "v1", resource: "cronjobs", namespaced: true},
		// Networking v1
		{kind: "ingress", group: "networking.k8s.io", version: "v1", resource: "ingresses", namespaced: true},
		{kind: "Ingresses", group: "networking.k8s.io", version: "v1", resource: "ingresses", namespaced: true},
		{kind: "networkpolicy", group: "networking.k8s.io", version: "v1", resource: "networkpolicies", namespaced: true},
		{kind: "NetworkPolicies", group: "networking.k8s.io", version: "v1", resource: "networkpolicies", namespaced: true},
		// RBAC v1
		{kind: "role", group: "rbac.authorization.k8s.io", version: "v1", resource: "roles", namespaced: true},
		{kind: "Roles", group: "rbac.authorization.k8s.io", version: "v1", resource: "roles", namespaced: true},
		{kind: "rolebinding", group: "rbac.authorization.k8s.io", version: "v1", resource: "rolebindings", namespaced: true},
		{kind: "RoleBindings", group: "rbac.authorization.k8s.io", version: "v1", resource: "rolebindings", namespaced: true},
		{kind: "clusterrole", group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterroles", namespaced: false},
		{kind: "ClusterRoles", group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterroles", namespaced: false},
		{kind: "clusterrolebinding", group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterrolebindings", namespaced: false},
		{kind: "ClusterRoleBindings", group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterrolebindings", namespaced: false},
		// HPA (all three aliases)
		{kind: "horizontalpodautoscaler", group: "autoscaling", version: "v2", resource: "horizontalpodautoscalers", namespaced: true},
		{kind: "HorizontalPodAutoscalers", group: "autoscaling", version: "v2", resource: "horizontalpodautoscalers", namespaced: true},
		{kind: "hpa", group: "autoscaling", version: "v2", resource: "horizontalpodautoscalers", namespaced: true},
		{kind: "HPA", group: "autoscaling", version: "v2", resource: "horizontalpodautoscalers", namespaced: true},
		// Default
		{kind: "Widget", resource: "", namespaced: false},
		{kind: "", resource: "", namespaced: false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			gvr, namespaced := GetGVR(tt.kind)
			if namespaced != tt.namespaced {
				t.Fatalf("GetGVR(%q) namespaced = %v, want %v", tt.kind, namespaced, tt.namespaced)
			}
			if gvr.Resource != tt.resource {
				t.Fatalf("GetGVR(%q) resource = %q, want %q", tt.kind, gvr.Resource, tt.resource)
			}
			if gvr.Group != tt.group {
				t.Fatalf("GetGVR(%q) group = %q, want %q", tt.kind, gvr.Group, tt.group)
			}
			if gvr.Version != tt.version {
				t.Fatalf("GetGVR(%q) version = %q, want %q", tt.kind, gvr.Version, tt.version)
			}
		})
	}
}
