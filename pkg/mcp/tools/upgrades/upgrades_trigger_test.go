package upgrades

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// clusterVersionScheme prepares a scheme that lets the fake dynamic client
// serve get/update on config.openshift.io/v1 ClusterVersion.
func clusterVersionScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersionList",
	}, &unstructured.UnstructuredList{})
	return s
}

// --- TriggerOpenShiftUpgrade additional-branch tests (was 32.4% coverage) ---

func TestTriggerOpenShiftUpgrade_ClusterVersionGetError(t *testing.T) {
	dynClient := dynamicfake.NewSimpleDynamicClient(clusterVersionScheme())
	dynClient.PrependReactor("get", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("clusterversions.config.openshift.io \"version\" not found")
	})
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{
		"target_version": "4.14.8",
		"confirm":        "yes-upgrade-now",
	})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to get ClusterVersion")
	assert.Contains(t, result, "does not appear to be an OpenShift cluster")
}

func TestTriggerOpenShiftUpgrade_InvalidTargetVersionWithAvailableUpdates(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		nil,
		[]map[string]interface{}{
			{"version": "4.14.8", "image": "quay.io/openshift-release-dev/ocp-release@sha256:aaa"},
			{"version": "4.14.9", "image": "quay.io/openshift-release-dev/ocp-release@sha256:bbb"},
		}, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(clusterVersionScheme(), cv)
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{
		"target_version": "4.15.0",
		"confirm":        "yes-upgrade-now",
	})
	require.False(t, isErr)
	assert.Contains(t, result, "Invalid Target Version")
	assert.Contains(t, result, "Version `4.15.0` is not in the list of available updates")
	assert.Contains(t, result, "- 4.14.8")
	assert.Contains(t, result, "- 4.14.9")
	// Confirm the "none available" fallback is NOT emitted when updates exist.
	assert.NotContains(t, result, "(none available")
}

func TestTriggerOpenShiftUpgrade_InvalidTargetVersionNoAvailableUpdates(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "", nil, nil, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(clusterVersionScheme(), cv)
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{
		"target_version": "4.14.8",
		"confirm":        "yes-upgrade-now",
	})
	require.False(t, isErr)
	assert.Contains(t, result, "Invalid Target Version")
	assert.Contains(t, result, "(none available - cluster may be at latest version)")
}

func TestTriggerOpenShiftUpgrade_UpdateError(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "",
		nil,
		[]map[string]interface{}{
			{"version": "4.14.8", "image": "quay.io/openshift-release-dev/ocp-release@sha256:aaa"},
		}, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(clusterVersionScheme(), cv)
	dynClient.PrependReactor("update", "clusterversions", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("forbidden: user cannot update clusterversions")
	})
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{
		"target_version": "4.14.8",
		"confirm":        "yes-upgrade-now",
	})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to trigger upgrade")
	assert.Contains(t, result, "forbidden")
}

func TestTriggerOpenShiftUpgrade_Success(t *testing.T) {
	cv := makeClusterVersion("4.14.7", "stable-4.14", "cluster-uuid-123",
		nil,
		[]map[string]interface{}{
			{"version": "4.14.8", "image": "quay.io/openshift-release-dev/ocp-release@sha256:aaa"},
			{"version": "4.14.9", "image": "quay.io/openshift-release-dev/ocp-release@sha256:bbb"},
		}, nil)

	dynClient := dynamicfake.NewSimpleDynamicClient(clusterVersionScheme(), cv)
	ca := &mockClusterAccess{dynClient: dynClient}

	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{
		"target_version": "4.14.9",
		"confirm":        "yes-upgrade-now",
	})
	require.False(t, isErr)
	assert.Contains(t, result, "# Upgrade Initiated")
	assert.Contains(t, result, "**Target Version:** 4.14.9")
	assert.Contains(t, result, "Upgrade has been triggered")
	assert.Contains(t, result, "get_upgrade_status")

	// Verify the ClusterVersion object was updated with the desired version.
	updated, err := dynClient.Resource(clusterVersionGVR).Get(context.Background(), "version", metav1.GetOptions{})
	require.NoError(t, err)
	got, found, err := unstructured.NestedString(updated.Object, "spec", "desiredUpdate", "version")
	require.NoError(t, err)
	require.True(t, found, "spec.desiredUpdate.version was not set on ClusterVersion")
	assert.Equal(t, "4.14.9", got)
}
