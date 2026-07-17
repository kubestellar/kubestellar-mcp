package upgrades

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// makeSubscription builds an operators.coreos.com/v1alpha1 Subscription
// unstructured object with the supplied nested spec / status fields.
func makeSubscription(name, namespace, channel, approval, currentCSV, state string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"channel":             channel,
				"installPlanApproval": approval,
			},
			"status": map[string]interface{}{
				"currentCSV": currentCSV,
				"state":      state,
			},
		},
	}
}

// subscriptionScheme returns a runtime.Scheme prepped with the SubscriptionList
// kind so the fake dynamic client can serve List() calls without panicking.
func subscriptionScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v1alpha1", Kind: "SubscriptionList",
	}, &unstructured.UnstructuredList{})
	return s
}

// --- CheckOLMOperatorUpgrades tests (was 8.9% coverage) ---

func TestCheckOLMOperatorUpgrades_DynamicClientError(t *testing.T) {
	ca := &mockClusterAccess{dynErr: fmt.Errorf("boom")}
	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

func TestCheckOLMOperatorUpgrades_OLMNotInstalled(t *testing.T) {
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme())
	dynClient.PrependReactor("list", "subscriptions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("no matches for kind \"Subscription\" in version \"operators.coreos.com/v1alpha1\"")
	})
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "**OLM Status:** Not installed")
	assert.Contains(t, result, "operatorframework.io")
}

func TestCheckOLMOperatorUpgrades_OLMNotInstalled_ResourceMissing(t *testing.T) {
	// Alternate not-installed signal: "could not find the requested resource".
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme())
	dynClient.PrependReactor("list", "subscriptions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("the server could not find the requested resource")
	})
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "**OLM Status:** Not installed")
}

func TestCheckOLMOperatorUpgrades_ListErrorReturnsError(t *testing.T) {
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme())
	dynClient.PrependReactor("list", "subscriptions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("connection refused")
	})
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to list subscriptions")
}

func TestCheckOLMOperatorUpgrades_NoSubscriptions(t *testing.T) {
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme())
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "**OLM Status:** Installed")
	assert.Contains(t, result, "**Subscriptions Found:** 0")
}

func TestCheckOLMOperatorUpgrades_MixedStates_AllNamespaces(t *testing.T) {
	subs := []runtime.Object{
		makeSubscription("op-a", "operators", "stable", "Automatic", "op-a.v1.0.0", "AtLatestKnown"),
		makeSubscription("op-b", "operators", "alpha", "Manual", "op-b.v0.5.0", "UpgradePending"),
		makeSubscription("op-c", "kube-system", "stable", "Manual", "op-c.v2.0.0", "UpgradeAvailable"),
		makeSubscription("op-d", "operators", "stable", "Automatic", "op-d.v0.1.0", "SomethingElse"),
	}
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme(), subs...)
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "**Subscriptions Found:** 4")
	// Table rows.
	assert.Contains(t, result, "| op-a | operators | op-a.v1.0.0 | stable | Auto | Up to date |")
	assert.Contains(t, result, "| op-b | operators | op-b.v0.5.0 | alpha | Manual | Upgrade pending |")
	assert.Contains(t, result, "| op-c | kube-system | op-c.v2.0.0 | stable | Manual | Upgrade available |")
	// Unknown state passes through verbatim.
	assert.Contains(t, result, "| op-d | operators | op-d.v0.1.0 | stable | Auto | SomethingElse |")
	// 2 subs report pending/available upgrades.
	assert.Contains(t, result, "**Upgrades Available:** 2 operator(s) have pending upgrades")
}

func TestCheckOLMOperatorUpgrades_NamespaceScoped_AllUpToDate(t *testing.T) {
	subs := []runtime.Object{
		makeSubscription("op-a", "operators", "stable", "Automatic", "op-a.v1", "AtLatestKnown"),
		makeSubscription("op-b", "operators", "stable", "Automatic", "op-b.v2", "AtLatestKnown"),
		// Different namespace — must be excluded when filter is applied.
		makeSubscription("op-c", "other", "stable", "Automatic", "op-c.v3", "UpgradePending"),
	}
	dynClient := dynamicfake.NewSimpleDynamicClient(subscriptionScheme(), subs...)
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{
		"namespace": "operators",
	})
	require.False(t, isErr)

	assert.Contains(t, result, "**Subscriptions Found:** 2")
	assert.Contains(t, result, "op-a")
	assert.Contains(t, result, "op-b")
	assert.NotContains(t, result, "op-c")
	assert.Contains(t, result, "**Upgrades Available:** All operators are at their latest known version")
}

// --- GetUpgradePrerequisites additional-branch tests (was 35.1% coverage) ---

// makeClusterOperator builds a config.openshift.io/v1 ClusterOperator with the
// supplied status conditions.
func makeClusterOperator(name string, conds []map[string]interface{}) *unstructured.Unstructured {
	condsIface := make([]interface{}, 0, len(conds))
	for _, c := range conds {
		condsIface = append(condsIface, c)
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterOperator",
			"metadata":   map[string]interface{}{"name": name},
			"status":     map[string]interface{}{"conditions": condsIface},
		},
	}
}

func makeMachineConfigPool(name string, conds []map[string]interface{}) *unstructured.Unstructured {
	condsIface := make([]interface{}, 0, len(conds))
	for _, c := range conds {
		condsIface = append(condsIface, c)
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "machineconfiguration.openshift.io/v1",
			"kind":       "MachineConfigPool",
			"metadata":   map[string]interface{}{"name": name},
			"status":     map[string]interface{}{"conditions": condsIface},
		},
	}
}

func openshiftPrereqScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperatorList",
	}, &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPoolList",
	}, &unstructured.UnstructuredList{})
	return s
}

func TestGetUpgradePrerequisites_DynamicClientError(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	ca := &mockClusterAccess{
		client:       cs,
		dynErr: fmt.Errorf("no dynamic client"),
	}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create dynamic client")
}

func TestGetUpgradePrerequisites_UnhealthyKubernetesCluster(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	nodeNotReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
		}},
	}

	// A pod in CrashLoopBackOff.
	crashPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crash", Namespace: "app"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff",
				}},
			}},
		},
	}
	// A pod in ImagePullBackOff.
	imgPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "img", Namespace: "app"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: "ImagePullBackOff",
				}},
			}},
		},
	}
	// A pod that finished — should be ignored.
	donePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: "app"},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	// Six pending pods → triggers the "many pending pods" warning branch.
	pendingObjs := []runtime.Object{nodeReady, nodeNotReady, crashPod, imgPod, donePod}
	for i := 0; i < 6; i++ {
		pendingObjs = append(pendingObjs, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pending-%d", i),
				Namespace: "app",
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		})
	}
	cs := newFakeClientWithVersion("v1.29.0", pendingObjs...)

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "Some nodes not ready (1/2)")
	assert.Contains(t, result, "Not ready: node-b")
	assert.Contains(t, result, "1 pods in CrashLoopBackOff")
	assert.Contains(t, result, "app/crash")
	assert.Contains(t, result, "1 pods with image pull errors")
	assert.Contains(t, result, "app/img")
	assert.Contains(t, result, "Many pending pods (6)")
	// No OpenShift section because ClusterVersion get returns not-found.
	assert.NotContains(t, result, "OpenShift-Specific Checks")
	assert.Contains(t, result, "**Failed:**")
	assert.Contains(t, result, "**Recommendation:** Fix the failed checks")
}

func TestGetUpgradePrerequisites_OpenShiftDegradedOperators(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)

	// One degraded, one unavailable, one progressing operator.
	coDegraded := makeClusterOperator("etcd", []map[string]interface{}{
		{"type": "Degraded", "status": "True"},
	})
	coUnavail := makeClusterOperator("network", []map[string]interface{}{
		{"type": "Available", "status": "False"},
	})
	coProgressing := makeClusterOperator("kube-apiserver", []map[string]interface{}{
		{"type": "Progressing", "status": "True"},
	})

	mcpUpdating := makeMachineConfigPool("worker", []map[string]interface{}{
		{"type": "Updating", "status": "True"},
	})
	mcpDegraded := makeMachineConfigPool("master", []map[string]interface{}{
		{"type": "Degraded", "status": "True"},
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(),
		cv, coDegraded, coUnavail, coProgressing, mcpUpdating, mcpDegraded)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "OpenShift-Specific Checks")
	assert.Contains(t, result, "1 degraded ClusterOperators: etcd")
	assert.Contains(t, result, "1 unavailable ClusterOperators: network")
	assert.Contains(t, result, "1 ClusterOperators progressing: kube-apiserver")
	assert.Contains(t, result, "1 MachineConfigPools updating: worker")
	assert.Contains(t, result, "1 MachineConfigPools degraded: master")
	assert.Contains(t, result, "**Recommendation:** Fix the failed checks")
}

func TestGetUpgradePrerequisites_OpenShiftAllHealthy(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)
	// Include a healthy CO and MCP so both the "all clean" success paths run.
	coHealthy := makeClusterOperator("etcd", []map[string]interface{}{
		{"type": "Available", "status": "True"},
		{"type": "Degraded", "status": "False"},
		{"type": "Progressing", "status": "False"},
	})
	mcpHealthy := makeMachineConfigPool("worker", []map[string]interface{}{
		{"type": "Updating", "status": "False"},
		{"type": "Degraded", "status": "False"},
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(),
		cv, coHealthy, mcpHealthy)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "No degraded ClusterOperators")
	assert.Contains(t, result, "All ClusterOperators available")
	assert.Contains(t, result, "No ClusterOperators progressing")
	assert.Contains(t, result, "No MachineConfigPools updating")
	assert.Contains(t, result, "No MachineConfigPools degraded")
	assert.Contains(t, result, "**Recommendation:** All prerequisites passed")
}
