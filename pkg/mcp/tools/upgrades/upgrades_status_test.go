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
	k8stesting "k8s.io/client-go/testing"
)

// makeClusterVersion builds an *unstructured.Unstructured representing a
// config.openshift.io/v1 ClusterVersion with the supplied nested fields.
// Field lookups tolerate missing keys, so callers can override only the
// fields they care about per-test.
func makeClusterVersion(desiredVersion, channel, clusterID string,
	conditions []map[string]interface{},
	availableUpdates []map[string]interface{},
	history []map[string]interface{}) *unstructured.Unstructured {

	obj := map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"spec": map[string]interface{}{
			"channel":   channel,
			"clusterID": clusterID,
		},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": desiredVersion},
		},
	}

	status := obj["status"].(map[string]interface{})
	if len(conditions) > 0 {
		conds := make([]interface{}, 0, len(conditions))
		for _, c := range conditions {
			conds = append(conds, c)
		}
		status["conditions"] = conds
	}
	if len(availableUpdates) > 0 {
		ups := make([]interface{}, 0, len(availableUpdates))
		for _, u := range availableUpdates {
			ups = append(ups, u)
		}
		status["availableUpdates"] = ups
	}
	if len(history) > 0 {
		h := make([]interface{}, 0, len(history))
		for _, e := range history {
			h = append(h, e)
		}
		status["history"] = h
	}

	return &unstructured.Unstructured{Object: obj}
}

// --- getOpenShiftVersionInfo — pure formatting helper, 0% prior coverage. ---

func TestGetOpenShiftVersionInfo_MinimalCluster(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "", nil, nil, nil)
	var sb strings.Builder
	out, isErr := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.False(t, isErr)
	assert.Contains(t, out, "**Cluster Type:** OpenShift")
	assert.Contains(t, out, "**Current Version:** 4.14.7")
	assert.Contains(t, out, "**Update Channel:** stable-4.14")
	// clusterID is empty, so that line must be omitted.
	assert.NotContains(t, out, "**Cluster ID:**")
	// No availableUpdates → the "None" sentinel must appear.
	assert.Contains(t, out, "**Available Updates:** None")
	// No history → the "## Upgrade History" section must be omitted.
	assert.NotContains(t, out, "## Upgrade History")
}

func TestGetOpenShiftVersionInfo_WithClusterID(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "abc-123", nil, nil, nil)
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.Contains(t, out, "**Cluster ID:** abc-123")
}

func TestGetOpenShiftVersionInfo_ProgressingCondition(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{
			{"type": "Available", "status": "True", "message": "ignored"},
			{"type": "Progressing", "status": "True", "message": "Working towards 4.14.7"},
		}, nil, nil)
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.Contains(t, out, "**Upgrade Status:** In Progress")
	assert.Contains(t, out, "**Progress:** Working towards 4.14.7")
}

func TestGetOpenShiftVersionInfo_ProgressingFalseIsIgnored(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{
			{"type": "Progressing", "status": "False", "message": "should-not-appear"},
		}, nil, nil)
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.NotContains(t, out, "**Upgrade Status:** In Progress")
	assert.NotContains(t, out, "should-not-appear")
}

func TestGetOpenShiftVersionInfo_AvailableUpdates(t *testing.T) {
	longImage := "quay.io/openshift-release-dev/ocp-release@sha256:" + strings.Repeat("a", 70)
	cv := makeClusterVersion("4.14.7", "stable-4.14", "", nil,
		[]map[string]interface{}{
			{"version": "4.14.8", "image": "quay.io/short"},
			{"version": "4.14.9", "image": longImage},
		}, nil)
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.Contains(t, out, "## Available Updates")
	assert.Contains(t, out, "| 4.14.8 | quay.io/short |")
	// Long image truncated to 57 chars + "..." (60 chars total).
	assert.Contains(t, out, "...")
	assert.NotContains(t, out, longImage)
	assert.NotContains(t, out, "**Available Updates:** None")
}

func TestGetOpenShiftVersionInfo_HistoryWithLimit(t *testing.T) {
	// Build 7 history entries — the helper should render only the first 5.
	history := make([]map[string]interface{}, 0, 7)
	for i := 1; i <= 7; i++ {
		history = append(history, map[string]interface{}{
			"version":        fmt.Sprintf("4.14.%d", i),
			"state":          "Completed",
			"completionTime": fmt.Sprintf("2024-01-0%dT00:00:00Z", i),
		})
	}
	cv := makeClusterVersion("4.14.7", "stable-4.14", "", nil, nil, history)
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	assert.Contains(t, out, "## Upgrade History")
	assert.Contains(t, out, "| 4.14.1 | Completed |")
	assert.Contains(t, out, "| 4.14.5 | Completed |")
	// Entries 6 and 7 are past the 5-item limit.
	assert.NotContains(t, out, "| 4.14.6 | Completed |")
	assert.NotContains(t, out, "| 4.14.7 | Completed |")
}

func TestGetOpenShiftVersionInfo_HistoryInProgressPlaceholder(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "", nil, nil,
		[]map[string]interface{}{
			{"version": "4.14.7", "state": "Partial", "completionTime": ""},
		})
	var sb strings.Builder
	out, _ := getOpenShiftVersionInfo(context.Background(), cv, &sb)

	// Empty completionTime becomes "In progress" placeholder.
	assert.Contains(t, out, "| 4.14.7 | Partial | In progress |")
}

// --- GetUpgradeStatus — vanilla-Kubernetes fallback path (5.3% prior). ---

func TestGetUpgradeStatus_VanillaKubernetes(t *testing.T) {
	// Two nodes with mixed Ready status.
	nodeReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.30.1"},
		},
	}
	nodeNotReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.30.0"},
		},
	}
	cs := newFakeClientWithVersion("v1.30.1", nodeReady, nodeNotReady)

	// Dynamic client that returns "not found" for clusterversions so the
	// handler falls through to the vanilla-Kubernetes branch.
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "**Cluster Type:** Kubernetes")
	assert.Contains(t, result, "## Node Versions")
	assert.Contains(t, result, "| node-a | v1.30.1 | Ready |")
	assert.Contains(t, result, "| node-b | v1.30.0 | NotReady |")
	// The vanilla branch always emits a note about non-OpenShift installers.
	assert.Contains(t, result, "installation method")
}

// --- GetUpgradeStatus — OpenShift progressing path (extends 5.3% coverage). ---

func TestGetUpgradeStatus_OpenShiftProgressing(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperatorList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPoolList",
	}, &unstructured.UnstructuredList{})

	cv := makeClusterVersion("4.14.8", "stable-4.14", "",
		[]map[string]interface{}{
			{"type": "Progressing", "status": "True", "message": "Working towards 4.14.8: 42% complete"},
		}, nil, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(scheme, cv)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "**Cluster Type:** OpenShift")
	assert.Contains(t, result, "**Target Version:** 4.14.8")
	assert.Contains(t, result, "**Status:** Upgrade in progress")
	assert.Contains(t, result, "42% complete")
}

func TestGetUpgradeStatus_OpenShiftNotProgressing(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperatorList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPoolList",
	}, &unstructured.UnstructuredList{})

	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		[]map[string]interface{}{
			{"type": "Available", "status": "True"},
			{"type": "Progressing", "status": "False"},
		}, nil, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(scheme, cv)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "**Status:** Not currently upgrading")
	assert.NotContains(t, result, "Upgrade in progress")
}

func TestGetUpgradeStatus_KubernetesClientError(t *testing.T) {
	ca := &mockClusterAccess{clientErr: fmt.Errorf("no kubeconfig")}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}
