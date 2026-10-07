// Package upgrades provides MCP tool handlers for Kubernetes cluster upgrade
// operations including version detection, OLM operator checks, Helm release
// inspection, prerequisite validation, and OpenShift upgrade triggering.
package upgrades

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/openshift"
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

// GVRs for upgrade-related CRDs. The OpenShift ones are shared with
// pkg/cmd/upgrade via pkg/openshift so both consumers stay in sync.
var (
	clusterVersionGVR    = openshift.ClusterVersionGVR
	clusterOperatorGVR   = openshift.ClusterOperatorGVR
	machineConfigPoolGVR = openshift.MachineConfigPoolGVR
	subscriptionGVR      = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
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
