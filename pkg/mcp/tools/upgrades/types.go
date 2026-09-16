// Package upgrades provides MCP tool handlers for Kubernetes cluster upgrade
// operations including version detection, OLM operator checks, Helm release
// inspection, prerequisite validation, and OpenShift upgrade triggering.
package upgrades

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ClusterAccess abstracts the Kubernetes client factories required by upgrade
// tool handlers. The MCP Server implements this interface.
type ClusterAccess interface {
	GetClientForCluster(clusterName string) (kubernetes.Interface, error)
	GetDynamicClientForCluster(clusterName string) (dynamic.Interface, error)
}

// Cluster type constants
const (
	ClusterTypeOpenShift = "openshift"
	ClusterTypeEKS       = "eks"
	ClusterTypeGKE       = "gke"
	ClusterTypeAKS       = "aks"
	ClusterTypeKubeadm   = "kubeadm"
	ClusterTypeK3s       = "k3s"
	ClusterTypeKind      = "kind"
	ClusterTypeMinikube  = "minikube"
	ClusterTypeUnknown   = "unknown"
)

// GVRs for upgrade-related CRDs
var (
	clusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}
	clusterOperatorGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}
	subscriptionGVR = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}
	machineConfigPoolGVR = schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}
)

// HelmRelease represents a decoded Helm release
type HelmRelease struct {
	Name      string
	Namespace string
	Chart     string
	Version   string
	AppVer    string
	Status    string
	Revision  int
}
