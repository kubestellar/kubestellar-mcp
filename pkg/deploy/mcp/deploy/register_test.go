package deploy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

func TestToolsReturnsDeployToolDefsInOrder(t *testing.T) {
	fd := newFakeDeps()
	fd.clusterCaps = func(ctx context.Context) ([]multicluster.ClusterCapabilities, error) { return nil, nil }
	fd.findClusters = func(ctx context.Context, req multicluster.WorkloadRequirements) ([]string, error) { return nil, nil }
	fd.executeSel = func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		results := make([]multicluster.ClusterResult, 0, len(clusterNames))
		for _, name := range clusterNames {
			results = append(results, multicluster.ClusterResult{Cluster: name, Result: map[string]interface{}{"ok": true}})
		}
		return results, nil
	}
	defs := Tools(fd.deps())

	require.Len(t, defs, 5)
	assert.Equal(t, []string{
		"list_cluster_capabilities",
		"find_clusters_for_workload",
		"deploy_app",
		"scale_app",
		"patch_app",
	}, []string{defs[0].Name, defs[1].Name, defs[2].Name, defs[3].Name, defs[4].Name})

	for _, def := range defs {
		assert.NotEmpty(t, def.Description)
		assert.NotNil(t, def.InputSchema)
		assert.NotNil(t, def.Handler)
	}

	// Exercise each handler at least once through the ToolDef wiring.
	capsRes, err := defs[0].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{}))
	require.NoError(t, err)
	assert.Nil(t, capsRes)

	findRes, err := defs[1].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"gpu_type": "nvidia.com/gpu",
	}))
	require.NoError(t, err)
	assert.NotNil(t, findRes)

	deployRes, err := defs[2].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.NoError(t, err)
	assert.NotNil(t, deployRes)

	_, err = defs[3].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "replicas": 3, "clusters": []string{"missing"},
	}))
	require.NoError(t, err)

	_, err = defs[4].Handler(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app": "demo", "patch": "{}", "clusters": []string{"missing"},
	}))
	require.NoError(t, err)
}
