package upgrades

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// These tests target previously-uncovered error/edge branches inside
// prerequisites.go:
//
//   - checkNodeHealthPrerequisite: nodes-list API error
//   - checkPodHealthPrerequisite: pods-list API error
//   - checkPodHealthPrerequisite: >5 crashing pods "and N more" truncation
//   - checkOpenShiftClusterOperators: list API error
//   - checkOpenShiftClusterOperators: non-map condition entry (continue branch)
//   - checkOpenShiftMachineConfigPools: list API error (silent 0,0 return)
//   - checkOpenShiftMachineConfigPools: non-map condition entry (continue branch)
//   - writePrerequisitesSummary: warnings>0 && failed==0 recommendation
//
// The functions are unexported and exercised through the public
// GetUpgradePrerequisites entry point using fake clients wired to return
// errors via PrependReactor or objects with malformed condition slices.

// TestGetUpgradePrerequisites_NodesListError forces the CoreV1 Nodes().List
// call to return an error, exercising the error return in
// checkNodeHealthPrerequisite.
func TestGetUpgradePrerequisites_NodesListError(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.0")
	cs.PrependReactor("list", "nodes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("nodes list boom")
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme())
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "Unable to check nodes: nodes list boom")
	// Failure count from the nodes error should show up in the summary.
	assert.Contains(t, result, "**Failed:**")
	assert.Contains(t, result, "**Recommendation:** Fix the failed checks")
}

// TestGetUpgradePrerequisites_PodsListError forces the CoreV1 Pods("").List
// call to return an error, exercising the error return in
// checkPodHealthPrerequisite.
func TestGetUpgradePrerequisites_PodsListError(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.29.0", nodeReady)
	cs.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("pods list boom")
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme())
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "Unable to check pods: pods list boom")
}

// TestGetUpgradePrerequisites_ManyCrashingPodsTruncated puts 7 pods in
// CrashLoopBackOff so the first 5 are listed and the "... and N more"
// truncation branch fires.
func TestGetUpgradePrerequisites_ManyCrashingPodsTruncated(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	objs := []runtime.Object{nodeReady}
	for i := 0; i < 7; i++ {
		objs = append(objs, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("crash-%d", i),
				Namespace: "app",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "CrashLoopBackOff",
					}},
				}},
			},
		})
	}
	cs := newFakeClientWithVersion("v1.29.0", objs...)

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme())
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "7 pods in CrashLoopBackOff")
	assert.Contains(t, result, "... and 2 more")
	// Exactly 5 listed pod names before the truncation line.
	assert.Equal(t, 5, strings.Count(result, "app/crash-"))
}

// TestGetUpgradePrerequisites_ClusterOperatorsListError exercises the error
// return in checkOpenShiftClusterOperators.
func TestGetUpgradePrerequisites_ClusterOperatorsListError(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	// Present a ClusterVersion so the OpenShift-specific block runs, then fail
	// the ClusterOperators list call.
	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)
	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(), cv)
	dynClient.PrependReactor("list", "clusteroperators", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusteroperators list boom")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "Unable to check ClusterOperators: clusteroperators list boom")
}

// TestGetUpgradePrerequisites_ClusterOperatorMalformedCondition builds a
// ClusterOperator whose conditions slice contains a non-map entry, exercising
// the `if !ok { continue }` branch in checkOpenShiftClusterOperators.
func TestGetUpgradePrerequisites_ClusterOperatorMalformedCondition(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)

	// One valid condition (Degraded=True) plus one non-map entry that must be
	// skipped without panicking or affecting the count.
	badCO := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterOperator",
		"metadata":   map[string]interface{}{"name": "etcd"},
		"status": map[string]interface{}{
			"conditions": []interface{}{
				"not-a-map",
				map[string]interface{}{"type": "Degraded", "status": "True"},
			},
		},
	}}

	mcpHealthy := makeMachineConfigPool("worker", []map[string]interface{}{
		{"type": "Updating", "status": "False"},
		{"type": "Degraded", "status": "False"},
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(),
		cv, badCO, mcpHealthy)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	// The valid Degraded=True condition should still be recorded.
	assert.Contains(t, result, "1 degraded ClusterOperators: etcd")
}

// TestGetUpgradePrerequisites_MachineConfigPoolsListError exercises the silent
// early-return (returns 0,0) branch in checkOpenShiftMachineConfigPools when
// the list call fails.
func TestGetUpgradePrerequisites_MachineConfigPoolsListError(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)
	coHealthy := makeClusterOperator("etcd", []map[string]interface{}{
		{"type": "Available", "status": "True"},
		{"type": "Degraded", "status": "False"},
		{"type": "Progressing", "status": "False"},
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(),
		cv, coHealthy)
	dynClient.PrependReactor("list", "machineconfigpools", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("mcp list boom")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	// MCP list failure is silent — no MCP section content is written.
	assert.NotContains(t, result, "MachineConfigPools updating")
	assert.NotContains(t, result, "MachineConfigPools degraded")
	// Cluster-operators section still ran successfully.
	assert.Contains(t, result, "No degraded ClusterOperators")
}

// TestGetUpgradePrerequisites_MachineConfigPoolMalformedCondition exercises
// the `if !ok { continue }` branch in checkOpenShiftMachineConfigPools.
func TestGetUpgradePrerequisites_MachineConfigPoolMalformedCondition(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	cs := newFakeClientWithVersion("v1.28.0", nodeReady)

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{{"type": "Available", "status": "True"}}, nil, nil)
	coHealthy := makeClusterOperator("etcd", []map[string]interface{}{
		{"type": "Available", "status": "True"},
		{"type": "Degraded", "status": "False"},
		{"type": "Progressing", "status": "False"},
	})

	badMCP := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "machineconfiguration.openshift.io/v1",
		"kind":       "MachineConfigPool",
		"metadata":   map[string]interface{}{"name": "worker"},
		"status": map[string]interface{}{
			"conditions": []interface{}{
				"not-a-map", // non-map entry — should be skipped
				map[string]interface{}{"type": "Updating", "status": "True"},
			},
		},
	}}

	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme(),
		cv, coHealthy, badMCP)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	// Valid Updating=True condition should still be recorded.
	assert.Contains(t, result, "MachineConfigPools updating: worker")
}

// TestGetUpgradePrerequisites_WarningsOnlySummary drives GetUpgradePrerequisites
// to a state where failed==0 && warnings>0 && OpenShift block is skipped, so
// writePrerequisitesSummary takes the `else if warnings > 0` recommendation
// branch.
func TestGetUpgradePrerequisites_WarningsOnlySummary(t *testing.T) {
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	objs := []runtime.Object{nodeReady}
	// 6 pending pods → triggers "Many pending pods" warning without any
	// crashing or image-pull pods (which would be failures/warnings too).
	for i := 0; i < 6; i++ {
		objs = append(objs, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pending-%d", i),
				Namespace: "app",
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		})
	}
	cs := newFakeClientWithVersion("v1.29.0", objs...)

	// Non-OpenShift cluster: ClusterVersion get returns not-found so the
	// OpenShift-specific block is skipped entirely.
	dynClient := dynamicfake.NewSimpleDynamicClient(openshiftPrereqScheme())
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "Many pending pods (6)")
	assert.Contains(t, result, "**Failed:** 0")
	assert.Contains(t, result, "**Recommendation:** Review warnings before proceeding")
}

// Guard: newFakeClientWithVersion isn't used by name here, but make sure the
// package's kubefake import path is exercised so future refactors do not
// accidentally drop the helper.
var _ = kubefake.NewSimpleClientset
var _ = schema.GroupVersionKind{}
