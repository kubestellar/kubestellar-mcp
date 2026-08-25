package upgrades

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// Extra branch coverage for DetectClusterType. The pre-existing
// upgrades_test.go covers OpenShift, K3s, Kind, Unknown, and the two
// client-error paths, leaving the EKS / GKE (label + provider-id fallback) /
// AKS / minikube / kubeadm distributions and the "no nodes" / "nodes list
// error" branches untested. Those are the branches this file locks in.

// makeNode wraps a small node-object builder so each test only has to declare
// the shape it cares about (labels, annotations, providerID).
func makeNode(name string, labels, annotations map[string]string, providerID string) *corev1.Node {
	if labels == nil {
		labels = map[string]string{}
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
}

// makeNonOpenShiftDynClient builds a fake dynamic client that rejects the
// ClusterVersion lookup so DetectClusterType falls through to node-based
// distribution detection.
func makeNonOpenShiftDynClient() *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	c := dynamicfake.NewSimpleDynamicClient(scheme)
	c.PrependReactor("get", "clusterversions",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("not found")
		})
	return c
}

func TestDetectClusterType_EKS(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("i-1234",
		map[string]string{"eks.amazonaws.com/nodegroup": "ng-1"},
		nil,
		"aws:///us-east-1a/i-1234")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeEKS)
	assert.Contains(t, result, "aws:///us-east-1a/i-1234")
}

func TestDetectClusterType_GKE_WithLabel(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("gke-node",
		map[string]string{"cloud.google.com/gke-nodepool": "pool-1"},
		nil,
		"gce://my-proj/us-central1-a/gke-node")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeGKE)
	assert.Contains(t, result, "Node labels contain cloud.google.com/gke")
}

func TestDetectClusterType_GKE_ProviderIDFallback(t *testing.T) {
	// gce:// provider ID without a matching label must still be classified as
	// GKE via the provider-id fallback branch.
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("gce-node", nil, nil, "gce://my-proj/us-central1-a/gce-node")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeGKE)
	assert.Contains(t, result, "Provider ID contains gce://")
}

func TestDetectClusterType_AKS(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("aks-node",
		map[string]string{"kubernetes.azure.com/cluster": "my-aks"},
		nil,
		"azure:///subscriptions/xxx/resourceGroups/my-rg/providers/Microsoft.Compute/virtualMachines/aks-node")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeAKS)
}

func TestDetectClusterType_Minikube(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("minikube",
		map[string]string{"minikube.k8s.io/version": "v1.32.0"},
		nil,
		"")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeMinikube)
}

func TestDetectClusterType_Kubeadm(t *testing.T) {
	// Neither cloud markers nor kind/minikube labels — only a kubeadm
	// annotation. Must fall through to the kubeadm branch, not "unknown".
	cs := newFakeClientWithVersion("v1.29.0")
	node := makeNode("cp-1",
		nil,
		map[string]string{"kubeadm.alpha.kubernetes.io/cri-socket": "unix:///var/run/dockershim.sock"},
		"")
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeKubeadm)
}

func TestDetectClusterType_NoNodes(t *testing.T) {
	// Nodes.List returns an empty list — DetectClusterType must fall into the
	// "No nodes found" branch and classify as Unknown without erroring.
	cs := newFakeClientWithVersion("v1.29.0")
	// No nodes created.
	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeUnknown)
	assert.Contains(t, result, "No nodes found")
}

func TestDetectClusterType_NodesListError(t *testing.T) {
	// If Nodes.List returns an error, DetectClusterType must degrade to
	// Unknown (with a "Unable to list nodes" note) and NOT report an error
	// isErr=true — the doc contract is that detection is best-effort.
	cs := newFakeClientWithVersion("v1.29.0")
	cs.Fake.PrependReactor("list", "nodes",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("api unavailable")
		})
	ca := &mockClusterAccess{client: cs, dynClient: makeNonOpenShiftDynClient()}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeUnknown)
	assert.Contains(t, result, "Unable to list nodes")
}
