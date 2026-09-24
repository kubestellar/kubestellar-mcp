package labels

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAddLabelsDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"alpha", "beta"}}
	got, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": map[string]string{"env": "prod"}, "dry_run": true}))
	require.NoError(t, err)

	result := decodeLabelsResp(t, got)
	targets := result["targetClusters"].([]interface{})
	require.Len(t, targets, 2)
	assert.ElementsMatch(t, []string{"alpha", "beta"}, []string{targets[0].(string), targets[1].(string)})
	assert.Equal(t, 2, int(result["totalClusters"].(float64)))
	assert.Equal(t, 2, int(result["successCount"].(float64)))
	assert.True(t, result["dryRun"].(bool))
}

func TestHandleRemoveLabelsDiscoversAllClustersWhenNoTargetSpecified(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"alpha", "beta"}}
	got, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": []string{"env"}, "dry_run": true}))
	require.NoError(t, err)

	result := decodeLabelsResp(t, got)
	targets := result["targetClusters"].([]interface{})
	require.Len(t, targets, 2)
	assert.ElementsMatch(t, []string{"alpha", "beta"}, []string{targets[0].(string), targets[1].(string)})
	assert.Equal(t, 2, int(result["totalClusters"].(float64)))
	assert.Equal(t, 2, int(result["successCount"].(float64)))
	assert.True(t, result["dryRun"].(bool))
}

func TestHandleAddLabels_DiscoverClusterNamesErrorPropagates(t *testing.T) {
	deps := &fakeDeps{discoverErr: errors.New("discovery unavailable")}
	_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": map[string]string{"env": "prod"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery unavailable")
}

func TestHandleRemoveLabels_DiscoverClusterNamesErrorPropagates(t *testing.T) {
	deps := &fakeDeps{discoverErr: errors.New("discovery unavailable")}
	_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": []string{"env"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery unavailable")
}

func TestHandleAddLabels_ExecuteOnSelectedErrorPropagates(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"alpha"}, execAllErr: errors.New("execution unavailable")}
	_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": map[string]string{"env": "prod"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution unavailable")
}

func TestHandleRemoveLabels_ExecuteOnSelectedErrorPropagates(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"alpha"}, execAllErr: errors.New("execution unavailable")}
	_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": []string{"env"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution unavailable")
}
