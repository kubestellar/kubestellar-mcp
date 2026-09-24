package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

func TestHandleListClusterCapabilities_InvalidArgs(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandleListClusterCapabilities(context.Background(), fd.deps(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid parameters")
}

func TestHandleListClusterCapabilities_AllClusters(t *testing.T) {
	fd := newFakeDeps()
	fd.clusterCaps = func(ctx context.Context) ([]multicluster.ClusterCapabilities, error) {
		return []multicluster.ClusterCapabilities{{Cluster: "alpha"}}, nil
	}
	got, err := HandleListClusterCapabilities(context.Background(), fd.deps(), nil)
	require.NoError(t, err)
	caps, ok := got.([]multicluster.ClusterCapabilities)
	require.True(t, ok)
	assert.Len(t, caps, 1)
}

func TestHandleListClusterCapabilities_SingleClusterSuccess(t *testing.T) {
	fd := newFakeDeps()
	fd.clusters = []string{"alpha"}
	fd.capsForCluster = func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (*multicluster.ClusterCapabilities, error) {
		return &multicluster.ClusterCapabilities{Cluster: clusterName, NodeCount: 1}, nil
	}
	got, err := HandleListClusterCapabilities(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{"cluster": "alpha"}))
	require.NoError(t, err)
	caps, ok := got.(*multicluster.ClusterCapabilities)
	require.True(t, ok)
	assert.Equal(t, "alpha", caps.Cluster)
	assert.Equal(t, 1, caps.NodeCount)
}

func TestHandleListClusterCapabilities_SingleClusterUnknown(t *testing.T) {
	fd := newFakeDeps()
	fd.clusters = nil
	_, err := HandleListClusterCapabilities(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{"cluster": "missing"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get capabilities for cluster missing")
}

func TestHandleFindClustersForWorkload_InvalidArgs(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandleFindClustersForWorkload(context.Background(), fd.deps(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleFindClustersForWorkload_Success(t *testing.T) {
	fd := newFakeDeps()
	fd.findClusters = func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error) {
		assert.Equal(t, "nvidia.com/gpu", req.GPUType)
		return []string{"gpu-cluster"}, nil
	}
	got, err := HandleFindClustersForWorkload(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"gpu_type": "nvidia.com/gpu",
		"min_gpu":  2,
	}))
	require.NoError(t, err)
	result := got.(map[string]interface{})
	assert.Equal(t, 1, result["count"])
	assert.Equal(t, []string{"gpu-cluster"}, result["matchingClusters"])
}

func TestHandleDeployApp_InvalidArgs(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandleDeployApp(context.Background(), fd.deps(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleDeployApp_ValidateManifestDocsError(t *testing.T) {
	fd := newFakeDeps()
	fd.validateErr = errors.New("bad manifest")
	_, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": "kind: Pod",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad manifest")
}

func TestHandleDeployApp_FindClustersForWorkloadError(t *testing.T) {
	fd := newFakeDeps()
	fd.findClusters = func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error) {
		return nil, errors.New("selector boom")
	}
	_, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": "kind: Pod",
		"gpu_type": "nvidia.com/gpu",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find matching clusters")
}

func TestHandleDeployApp_NoMatchingClusters(t *testing.T) {
	fd := newFakeDeps()
	fd.findClusters = func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error) {
		return nil, nil
	}
	_, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": "kind: Pod",
		"gpu_type": "nvidia.com/gpu",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters found matching requirements")
}

func TestHandleDeployApp_DiscoverClustersFallback(t *testing.T) {
	fd := newFakeDeps()
	fd.clusters = []string{"alpha", "beta"}
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"
	got, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"dry_run":  true,
	}))
	require.NoError(t, err)
	result := got.(map[string]interface{})
	assert.ElementsMatch(t, []string{"alpha", "beta"}, result["targetClusters"])
	assert.Equal(t, 2, result["totalClusters"])
}

func TestHandleDeployApp_DiscoverClustersError(t *testing.T) {
	fd := newFakeDeps()
	fd.discoverErr = errors.New("discover boom")
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"
	_, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discover boom")
}

func TestHandleDeployApp_DryRunAcrossClustersAndFailedSummary(t *testing.T) {
	fd := newFakeDeps()
	fd.configErrs["ghost"] = errors.New(`context "ghost" does not exist`)
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"
	got, err := HandleDeployApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha", "ghost"},
		"dry_run":  true,
	})) // fd.execute is nil -> default fake uses ExecuteOnSelected below

	require.NoError(t, err)
	result := got.(map[string]interface{})
	assert.Equal(t, []string{"alpha", "ghost"}, result["targetClusters"])
	assert.Equal(t, 2, result["totalClusters"])
	assert.Equal(t, 1, result["successCount"])

	deployResults := result["results"].([]DeployResult)
	var failed, ok bool
	for _, dr := range deployResults {
		if dr.Cluster == "ghost" {
			assert.Equal(t, "failed", dr.Status)
			failed = true
		}
		if dr.Cluster == "alpha" {
			assert.Equal(t, "would-apply", dr.Status)
			ok = true
		}
	}
	assert.True(t, failed, "expected a failed DeployResult for ghost")
	assert.True(t, ok, "expected a would-apply DeployResult for alpha")
}

func TestHandleScaleApp_InvalidArgs(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandleScaleApp(context.Background(), fd.deps(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleScaleApp_RejectsSystemNamespace(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandleScaleApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "replicas": 3, "namespace": "kube-system",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
}

func TestHandleScaleApp_NoClustersAndNoInstances(t *testing.T) {
	fd := newFakeDeps()
	fd.execute = func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		// Simulate app.GetAppInstances finding nothing anywhere.
		return []multicluster.ClusterResult{{Cluster: "only", Result: []app.AppInstance{}}}, nil
	}
	_, err := HandleScaleApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "missing-app", "replicas": 3,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-app not found in any cluster")
}

func TestHandleScaleApp_AutoDiscoversClustersFromInstances(t *testing.T) {
	fd := newFakeDeps()
	fd.execute = func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		return []multicluster.ClusterResult{{
			Cluster: "discovered-cluster",
			Result: []app.AppInstance{
				{Cluster: "discovered-cluster", Name: "demo"},
			},
		}}, nil
	}
	fd.executeSel = func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		results := make([]multicluster.ClusterResult, 0, len(clusterNames))
		for _, name := range clusterNames {
			results = append(results, multicluster.ClusterResult{Cluster: name, Result: map[string]interface{}{"scaled": true}})
		}
		return results, nil
	}
	got, err := HandleScaleApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "replicas": 4,
	}))
	require.NoError(t, err)
	result := got.(map[string]interface{})
	assert.Equal(t, "demo", result["app"])
	assert.Equal(t, int32(4), result["replicas"])
}

func TestHandleScaleApp_ExplicitMissingClusterReturnsClusterError(t *testing.T) {
	fd := newFakeDeps()
	fd.configErrs["missing"] = errors.New(`context "missing" does not exist`)
	got, err := HandleScaleApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "replicas": 3, "clusters": []string{"missing"},
	}))
	require.NoError(t, err)
	result := got.(map[string]interface{})
	clusterResults := result["results"].([]multicluster.ClusterResult)
	require.Len(t, clusterResults, 1)
	assert.Equal(t, "missing", clusterResults[0].Cluster)
	assert.Contains(t, clusterResults[0].Error, `context "missing" does not exist`)
}

func TestHandlePatchApp_InvalidArgs(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandlePatchApp(context.Background(), fd.deps(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandlePatchApp_RejectsSystemNamespace(t *testing.T) {
	fd := newFakeDeps()
	_, err := HandlePatchApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "patch": "{}", "namespace": "kube-system",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
}

func TestHandlePatchApp_DiscoverClustersFallback(t *testing.T) {
	fd := newFakeDeps()
	fd.clusters = []string{"only"}
	fd.executeSel = func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		results := make([]multicluster.ClusterResult, 0, len(clusterNames))
		for _, name := range clusterNames {
			results = append(results, multicluster.ClusterResult{Cluster: name, Result: map[string]interface{}{"patched": true}})
		}
		return results, nil
	}
	got, err := HandlePatchApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "patch": `{"spec":{"replicas":2}}`,
	}))
	require.NoError(t, err)
	result := got.(map[string]interface{})
	assert.Equal(t, "demo", result["app"])
}

func TestHandlePatchApp_DiscoverClustersError(t *testing.T) {
	fd := newFakeDeps()
	fd.discoverErr = errors.New("discover boom")
	_, err := HandlePatchApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "patch": "{}",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discover boom")
}

func TestHandlePatchApp_PatchTypes(t *testing.T) {
	for _, tt := range []struct {
		patchType string
	}{{""}, {"merge"}, {"json"}} {
		fd := newFakeDeps()
		fd.executeSel = func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
			results := make([]multicluster.ClusterResult, 0, len(clusterNames))
			for _, name := range clusterNames {
				results = append(results, multicluster.ClusterResult{Cluster: name, Result: map[string]interface{}{"patched": true}})
			}
			return results, nil
		}
		got, err := HandlePatchApp(context.Background(), fd.deps(), mustMarshalJSON(t, map[string]interface{}{
			"app": "demo", "patch": "{}", "patch_type": tt.patchType, "clusters": []string{"cA"},
		}))
		require.NoError(t, err)
		result := got.(map[string]interface{})
		assert.Equal(t, "demo", result["app"])
	}
}
