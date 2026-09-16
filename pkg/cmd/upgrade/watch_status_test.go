package upgrade

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestParseProgressMessage(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantPct    int
		wantDone   int
		wantTotal  int
		wantWaitOn string
	}{
		{
			name:       "extracts all progress fields",
			message:    "Working towards 4.18.30: 168 of 906 done (18% complete), waiting on cloud-controller-manager",
			wantPct:    18,
			wantDone:   168,
			wantTotal:  906,
			wantWaitOn: "cloud-controller-manager",
		},
		{
			name:       "extracts counts without waiting target",
			message:    "Working towards 4.18.30: 5 of 10 done (50% complete)",
			wantPct:    50,
			wantDone:   5,
			wantTotal:  10,
			wantWaitOn: "",
		},
		{
			name:       "trims waiting target before trailing details",
			message:    "Working towards 4.18.30: 168 of 906 done (18% complete), waiting on cloud-controller-manager, node drains pending",
			wantPct:    18,
			wantDone:   168,
			wantTotal:  906,
			wantWaitOn: "cloud-controller-manager",
		},
		{
			name:       "returns zero values when format does not match",
			message:    "upgrade has not started",
			wantPct:    0,
			wantDone:   0,
			wantTotal:  0,
			wantWaitOn: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, done, total, waiting := parseProgressMessage(tt.message)
			assert.Equal(t, tt.wantPct, pct)
			assert.Equal(t, tt.wantDone, done)
			assert.Equal(t, tt.wantTotal, total)
			assert.Equal(t, tt.wantWaitOn, waiting)
		})
	}
}

func TestGetUpgradeStatusInProgress(t *testing.T) {
	dynClient := newFakeDynamicClient(
		newClusterVersion(
			"4.18.30",
			"Working towards 4.18.30: 168 of 906 done (18% complete), waiting on cloud-controller-manager",
		),
	)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "4.18.30", status.Label)
	assert.Equal(t, 18, status.Percent)
	assert.Equal(t, 168, status.Done)
	assert.Equal(t, 906, status.Total)
	assert.Equal(t, "cloud-controller-manager", status.Current)
	assert.False(t, status.Complete)
}

func TestGetUpgradeStatusComplete(t *testing.T) {
	dynClient := newFakeDynamicClient(
		newClusterVersion("4.18.30", "Cluster version is 4.18.30"),
	)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "4.18.30", status.Label)
	assert.Equal(t, 100, status.Percent)
	assert.True(t, status.Complete)
}

func newFakeDynamicClient(objects ...runtime.Object) dynamic.Interface {
	scheme := runtime.NewScheme()
	clusterVersionGV := schema.GroupVersion{Group: clusterVersionGVR.Group, Version: clusterVersionGVR.Version}
	scheme.AddKnownTypeWithName(clusterVersionGV.WithKind("ClusterVersion"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(clusterVersionGV.WithKind("ClusterVersionList"), &unstructured.UnstructuredList{})

	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

func newClusterVersion(desiredVersion, progressMessage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata": map[string]interface{}{
			"name": "version",
		},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{
				"version": desiredVersion,
			},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"message": progressMessage,
				},
			},
		},
	}}
}

func TestGetNestedString(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"desired": map[string]interface{}{
				"version": "4.14.0",
			},
		},
	}}

	assert.Equal(t, "4.14.0", getNestedString(obj, "status", "desired", "version"))
	assert.Equal(t, "", getNestedString(obj, "status", "nonexistent"))
	assert.Equal(t, "", getNestedString(obj, "missing", "path"))
}

func TestGetUpgradeStatus_NotProgressing(t *testing.T) {
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": "4.14.0"},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "False",
					"message": "Cluster version is 4.14.0",
				},
			},
		},
	}}
	dynClient := newFakeDynamicClient(cv)
	status, err := getUpgradeStatus(context.Background(), dynClient)
	require.NoError(t, err)
	assert.Equal(t, "4.14.0", status.Label)
	assert.True(t, status.Complete)
	assert.Equal(t, 100, status.Percent)
}

func TestGetUpgradeStatus_ErrorFetchingClusterVersion(t *testing.T) {
	// Empty client with no ClusterVersion resource
	dynClient := newFakeDynamicClient()

	_, err := getUpgradeStatus(context.Background(), dynClient)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get ClusterVersion")
}

func TestGetUpgradeStatus_EmptyConditions(t *testing.T) {
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired":    map[string]interface{}{"version": "4.18.30"},
			"conditions": []interface{}{},
		},
	}}
	dynClient := newFakeDynamicClient(cv)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "4.18.30", status.Label)
	assert.Equal(t, 0, status.Percent)
	assert.False(t, status.Complete)
}

func TestGetUpgradeStatus_NoDesiredVersion(t *testing.T) {
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"message": "Working towards 4.18.30: 50 of 100 done (50% complete)",
				},
			},
		},
	}}
	dynClient := newFakeDynamicClient(cv)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "", status.Label)
	assert.Equal(t, 50, status.Percent)
	assert.False(t, status.Complete)
}

func TestParseProgressMessage_PercentOnly(t *testing.T) {
	pct, done, total, waiting := parseProgressMessage("something 75% complete")
	assert.Equal(t, 75, pct)
	assert.Equal(t, 0, done)
	assert.Equal(t, 0, total)
	assert.Equal(t, "", waiting)
}

func TestParseProgressMessage_WaitingOnly(t *testing.T) {
	pct, done, total, waiting := parseProgressMessage("something, waiting on kube-apiserver")
	assert.Equal(t, 0, pct)
	assert.Equal(t, 0, done)
	assert.Equal(t, 0, total)
	assert.Equal(t, "kube-apiserver", waiting)
}

func TestParseProgressMessage_100Percent(t *testing.T) {
	pct, done, total, waiting := parseProgressMessage("900 of 906 done (100% complete)")
	assert.Equal(t, 100, pct)
	assert.Equal(t, 900, done)
	assert.Equal(t, 906, total)
	assert.Equal(t, "", waiting)
}

func TestGetUpgradeStatus_NonProgressingCondition(t *testing.T) {
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": "4.17.0"},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Degraded",
					"message": "etcd is unhealthy",
				},
				map[string]interface{}{
					"type":    "Available",
					"status":  "True",
					"message": "Cluster version is 4.17.0",
				},
			},
		},
	}}
	dynClient := newFakeDynamicClient(cv)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "4.17.0", status.Label)
	assert.Equal(t, 0, status.Percent)
}

func TestGetUpgradeStatus_MultipleConditionsWithProgressing(t *testing.T) {
	cv := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": "4.18.30"},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Available",
					"status":  "True",
					"message": "Cluster is available",
				},
				map[string]interface{}{
					"type":    "Degraded",
					"status":  "False",
					"message": "",
				},
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "True",
					"message": "Working towards 4.18.30: 300 of 906 done (33% complete), waiting on etcd",
				},
			},
		},
	}}
	dynClient := newFakeDynamicClient(cv)

	status, err := getUpgradeStatus(context.Background(), dynClient)

	require.NoError(t, err)
	assert.Equal(t, "4.18.30", status.Label)
	assert.Equal(t, 33, status.Percent)
	assert.Equal(t, 300, status.Done)
	assert.Equal(t, 906, status.Total)
	assert.Equal(t, "etcd", status.Current)
	assert.False(t, status.Complete)
}
