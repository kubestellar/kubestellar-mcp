package mcp

import (
	"context"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListClusterCapabilitiesReturnsEmptySliceWithoutClusters(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleListClusterCapabilities(context.Background(), nil)
	require.NoError(t, err)

	capabilities, ok := got.([]multicluster.ClusterCapabilities)
	require.True(t, ok)
	assert.Len(t, capabilities, 0)
}

func TestHandleListClusterCapabilitiesReturnsErrorForUnknownCluster(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleListClusterCapabilities(context.Background(), mustMarshalJSON(t, map[string]interface{}{"cluster": "missing"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get capabilities for cluster missing")
}

func TestHandleFindClustersForWorkloadValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleFindClustersForWorkload(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleFindClustersForWorkloadReturnsRequirements(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleFindClustersForWorkload(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"gpu_type":   "nvidia.com/gpu",
		"min_gpu":    2,
		"min_memory": "16Gi",
		"min_cpu":    "4",
		"labels":     map[string]string{"topology.kubernetes.io/region": "us-west-1"},
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, 0, result["count"])
	assert.Empty(t, result["matchingClusters"])
	assert.Equal(t, multicluster.WorkloadRequirements{
		GPUType:    "nvidia.com/gpu",
		MinGPU:     2,
		MinMemory:  "16Gi",
		MinCPU:     "4",
		NodeLabels: map[string]string{"topology.kubernetes.io/region": "us-west-1"},
	}, result["requirements"])
}

func TestHandleDeployAppValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleDeployApp(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleDeployAppDryRunAcrossClusters(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com", "beta": "https://beta.example.com"})
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
---
apiVersion: v1
kind: Service
metadata:
  name: demo-service
`

	got, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha", "beta"},
		"dry_run":  true,
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	assert.Equal(t, []string{"alpha", "beta"}, result["targetClusters"])
	assert.Equal(t, 2, result["successCount"])
	assert.Equal(t, 2, result["totalClusters"])
	assert.True(t, result["dryRun"].(bool))

	deployResults := result["results"].([]DeployResult)
	require.Len(t, deployResults, 4)
	for _, item := range deployResults {
		assert.Equal(t, "would-apply", item.Status)
	}
}

func TestHandleDeployAppReturnsNoMatchingClustersForGPURequirements(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	manifest := `apiVersion: v1
kind: Pod
metadata:
  name: demo
`

	_, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"gpu_type": "nvidia.com/gpu",
		"min_gpu":  1,
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters found matching requirements")
}

func TestApplyManifestDryRunDefaultsNamespace(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
`

	results, err := server.applyManifest(context.Background(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Contains(t, results[0].Message, "namespace default")
}

func TestApplyManifestReturnsDecodeError(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.applyManifest(context.Background(), nil, "alpha", "[", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestHandleScaleAppRequiresExistingAppWhenNoClustersSpecified(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{"app": "demo", "replicas": 3}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app demo not found in any cluster")
}

func TestHandleScaleAppExplicitMissingClusterReturnsClusterError(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app":      "demo",
		"replicas": 3,
		"clusters": []string{"missing"},
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	clusterResults := result["results"].([]multicluster.ClusterResult)
	require.Len(t, clusterResults, 1)
	assert.Equal(t, "missing", clusterResults[0].Cluster)
	assert.Contains(t, clusterResults[0].Error, "context \"missing\" does not exist")
}
