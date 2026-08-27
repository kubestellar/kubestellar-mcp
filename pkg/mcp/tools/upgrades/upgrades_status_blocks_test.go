package upgrades

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// GetUpgradeStatus's ClusterOperator table, MachineConfigPool table, and
// upgrade-history rendering blocks were previously uncovered — the OpenShift
// tests only fed in a ClusterVersion, so the CO/MCP List branches and the
// history block never executed. These tests exercise each block explicitly.

func newOpenShiftScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperator",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperatorList",
	}, &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPool",
	}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPoolList",
	}, &unstructured.UnstructuredList{})
	return scheme
}

func makeMachineConfigPoolWithCounts(name string, machineCount, ready, updated int64, conditions []map[string]interface{}) *unstructured.Unstructured {
	conds := make([]interface{}, 0, len(conditions))
	for _, c := range conditions {
		conds = append(conds, c)
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "machineconfiguration.openshift.io/v1",
		"kind":       "MachineConfigPool",
		"metadata":   map[string]interface{}{"name": name},
		"status": map[string]interface{}{
			"machineCount":        machineCount,
			"readyMachineCount":   ready,
			"updatedMachineCount": updated,
			"conditions":          conds,
		},
	}}
}

// TestGetUpgradeStatus_OpenShiftWithClusterOperators exercises the
// ClusterOperator table rendering block (uncovered because prior tests
// omitted CO objects and the dynamic List returned zero items).
func TestGetUpgradeStatus_OpenShiftWithClusterOperators(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	cv := makeClusterVersion("4.14.8", "stable-4.14", "",
		[]map[string]interface{}{
			{"type": "Progressing", "status": "True", "message": "Working towards 4.14.8"},
		}, nil, nil)

	// Two operators: one fully-Available, one Degraded — verifies the
	// condition-type switch renders both columns correctly, and that
	// an unrecognized condition-type entry is silently ignored.
	coHealthy := makeClusterOperator("kube-apiserver", []map[string]interface{}{
		{"type": "Available", "status": "True"},
		{"type": "Progressing", "status": "False"},
		{"type": "Degraded", "status": "False"},
		{"type": "Upgradeable", "status": "True"}, // unrecognized — must be ignored
	})
	coDegraded := makeClusterOperator("network", []map[string]interface{}{
		{"type": "Available", "status": "False"},
		{"type": "Degraded", "status": "True"},
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(newOpenShiftScheme(), cv, coHealthy, coDegraded)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "## ClusterOperator Status")
	assert.Contains(t, result, "| Operator | Available | Progressing | Degraded |")
	assert.Contains(t, result, "| kube-apiserver | True | False | False |")
	assert.Contains(t, result, "| network | False | - | True |")
}

// TestGetUpgradeStatus_OpenShiftWithMachineConfigPools exercises the
// MachineConfigPool table rendering block (previously uncovered).
func TestGetUpgradeStatus_OpenShiftWithMachineConfigPools(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	cv := makeClusterVersion("4.14.8", "stable-4.14", "", nil, nil, nil)

	mcpMaster := makeMachineConfigPoolWithCounts("master", 3, 3, 3, []map[string]interface{}{
		{"type": "Updating", "status": "False"},
		{"type": "Degraded", "status": "False"},
	})
	mcpWorker := makeMachineConfigPoolWithCounts("worker", 5, 4, 2, []map[string]interface{}{
		{"type": "Updating", "status": "True"},
		{"type": "Degraded", "status": "True"},
		{"type": "Ignored", "status": "True"}, // unrecognized — must be ignored
	})

	dynClient := dynamicfake.NewSimpleDynamicClient(newOpenShiftScheme(), cv, mcpMaster, mcpWorker)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "## MachineConfigPool Status")
	assert.Contains(t, result, "| Pool | Ready | Updated | Updating | Degraded |")
	assert.Contains(t, result, "| master | 3/3 | 3/3 | False | False |")
	assert.Contains(t, result, "| worker | 4/5 | 2/5 | True | True |")
}

// TestGetUpgradeStatus_OpenShiftWithHistory exercises the Recent History
// rendering block, including the "In progress" placeholder for entries
// with an empty completionTime, and the top-3 limit on history entries.
func TestGetUpgradeStatus_OpenShiftWithHistory(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	// Five entries — only the first 3 should render.
	history := []map[string]interface{}{
		{"version": "4.14.8", "state": "Partial", "startedTime": "2026-01-01T00:00:00Z", "completionTime": ""},
		{"version": "4.14.7", "state": "Completed", "startedTime": "2025-12-01T00:00:00Z", "completionTime": "2025-12-01T02:00:00Z"},
		{"version": "4.14.6", "state": "Completed", "startedTime": "2025-11-01T00:00:00Z", "completionTime": "2025-11-01T02:00:00Z"},
		{"version": "4.14.5", "state": "Completed", "startedTime": "2025-10-01T00:00:00Z", "completionTime": "2025-10-01T02:00:00Z"},
		{"version": "4.14.4", "state": "Completed", "startedTime": "2025-09-01T00:00:00Z", "completionTime": "2025-09-01T02:00:00Z"},
	}

	cv := makeClusterVersion("4.14.8", "stable-4.14", "", nil, nil, history)

	dynClient := dynamicfake.NewSimpleDynamicClient(newOpenShiftScheme(), cv)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "## Recent History")
	assert.Contains(t, result, "| Version | State | Started | Completed |")
	// Top-3 rendered
	assert.Contains(t, result, "| 4.14.8 | Partial | 2026-01-01T00:00:00Z | In progress |")
	assert.Contains(t, result, "| 4.14.7 | Completed | 2025-12-01T00:00:00Z | 2025-12-01T02:00:00Z |")
	assert.Contains(t, result, "| 4.14.6 | Completed | 2025-11-01T00:00:00Z | 2025-11-01T02:00:00Z |")
	// Anything beyond the top 3 must be dropped.
	assert.NotContains(t, result, "4.14.5")
	assert.NotContains(t, result, "4.14.4")
}

// TestGetUpgradeStatus_OpenShiftHistoryFewerThanLimit exercises the branch
// where len(history) < the top-3 cap, so the limit is truncated to the
// actual entry count. Regression guard for an off-by-one that would panic
// or render fewer/more rows than requested.
func TestGetUpgradeStatus_OpenShiftHistoryFewerThanLimit(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	history := []map[string]interface{}{
		{"version": "4.14.8", "state": "Completed", "startedTime": "s1", "completionTime": "c1"},
	}
	cv := makeClusterVersion("4.14.8", "stable-4.14", "", nil, nil, history)
	dynClient := dynamicfake.NewSimpleDynamicClient(newOpenShiftScheme(), cv)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)

	assert.Contains(t, result, "| 4.14.8 | Completed | s1 | c1 |")
}

// TestGetUpgradeStatus_DynamicClientError verifies the early-return
// error branch when the dynamic client cannot be built for the target
// cluster (previously uncovered).
func TestGetUpgradeStatus_DynamicClientError(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")
	ca := &mockClusterAccess{client: cs, dynErr: assertingError("no dynamic client for cluster")}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{"cluster": "unknown"})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create dynamic client")
}

// TestGetUpgradeStatus_OpenShiftConditionsWithNonMap covers the
// `condMap, ok := cond.(map[string]interface{}); !ok { continue }` branch
// in the conditions loop by supplying a non-map entry in the conditions
// slice alongside a valid Progressing=True condition. The non-map entry
// must be silently skipped and the valid one must still be rendered.
func TestGetUpgradeStatus_OpenShiftConditionsWithNonMap(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	// Hand-build a ClusterVersion with a raw-string entry in conditions —
	// makeClusterVersion() cannot express this because it types the
	// slice as []map[string]interface{}.
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": "4.14.9"},
			"conditions": []interface{}{
				"not-a-map",
				map[string]interface{}{"type": "Progressing", "status": "True", "message": "Working towards 4.14.9"},
			},
		},
	}}

	dynClient := dynamicfake.NewSimpleDynamicClient(newOpenShiftScheme(), cv)
	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	require.False(t, isErr)
	assert.Contains(t, result, "**Status:** Upgrade in progress")
	assert.Contains(t, result, "Working towards 4.14.9")
}

// assertingError is a tiny error type used only to fabricate dynamic
// client errors in tests without pulling in errors.New everywhere.
type assertingError string

func (e assertingError) Error() string { return string(e) }
