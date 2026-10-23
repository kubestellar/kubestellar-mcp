// Package upgrades provides MCP tool handlers for Kubernetes cluster upgrade
// operations including version detection, OLM operator checks, Helm release
// inspection, prerequisite validation, and OpenShift upgrade triggering.
package upgrades

import (
	"fmt"

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

// getClusterClients creates both the typed and dynamic clients for cluster,
// deduplicating the identical client/dynClient-creation-and-error-wrapping
// block that was previously repeated across detect.go, prerequisites.go,
// status.go and version.go. On failure it returns the formatted tool-result
// error message in errMsg and ok=false; callers should return (errMsg, true)
// unchanged in that case.
func getClusterClients(ca ClusterAccess, cluster string) (client kubernetes.Interface, dynClient dynamic.Interface, errMsg string, ok bool) {
	client, err := ca.GetClientForCluster(cluster)
	if err != nil {
		return nil, nil, fmt.Sprintf("Failed to create client: %v", err), false
	}

	dynClient, err = ca.GetDynamicClientForCluster(cluster)
	if err != nil {
		return nil, nil, fmt.Sprintf("Failed to create dynamic client: %v", err), false
	}

	return client, dynClient, "", true
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
