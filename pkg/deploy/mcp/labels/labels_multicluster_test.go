package labels

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
)

func TestHandleAddLabelsHappyPathTwoClusters(t *testing.T) {
	srvA := patchAcceptingServer(t, nil)
	defer srvA.Close()
	srvB := patchAcceptingServer(t, nil)
	defer srvB.Close()

	deps := &fakeDeps{clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srvA), "cB": clientForServer(t, srvB)}}
	res, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "namespace": "apps", "labels": map[string]string{"env": "prod"}, "clusters": []string{"cA", "cB"}}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.Equal(t, 2, int(decoded["successCount"].(float64)))
	assert.Equal(t, 2, int(decoded["totalClusters"].(float64)))
	assert.False(t, decoded["dryRun"].(bool))
	assert.Equal(t, []string{"labeled", "labeled"}, labelResultsStatuses(decoded))
}

func TestHandleAddLabelsDryRunDiscoverClusters(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"cA"}}
	res, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "namespace": "apps", "labels": map[string]string{"env": "prod"}, "dry_run": true}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.True(t, decoded["dryRun"].(bool))
	assert.Equal(t, 1, int(decoded["successCount"].(float64)))
	targets := decoded["targetClusters"].([]interface{})
	require.Len(t, targets, 1)
	assert.Equal(t, "cA", targets[0].(string))
	assert.Equal(t, []string{"would-label"}, labelResultsStatuses(decoded))
}

func TestHandleRemoveLabelsHappyPathTwoClusters(t *testing.T) {
	srvA := patchAcceptingServer(t, nil)
	defer srvA.Close()
	srvB := patchAcceptingServer(t, nil)
	defer srvB.Close()

	deps := &fakeDeps{clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srvA), "cB": clientForServer(t, srvB)}}
	res, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "namespace": "apps", "labels": []string{"env", "team"}, "clusters": []string{"cA", "cB"}}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.Equal(t, 2, int(decoded["successCount"].(float64)))
	assert.Equal(t, []string{"unlabeled", "unlabeled"}, labelResultsStatuses(decoded))
	keys := decoded["labelKeys"].([]interface{})
	assert.Len(t, keys, 2)
}

func TestHandleRemoveLabelsDryRunDiscoverClusters(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"only"}}
	res, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": []string{"env"}, "dry_run": true}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.Equal(t, 1, int(decoded["successCount"].(float64)))
	assert.Equal(t, []string{"would-unlabel"}, labelResultsStatuses(decoded))
}

func TestAddLabelsInClusterNotFoundMapsToNotFoundStatus(t *testing.T) {
	srv := startNotFoundServer(t)
	defer srv.Close()
	res, err := AddLabelsInCluster(context.Background(), &fakeDeps{}, clientForServer(t, srv), "cA", "deployment", "demo", "apps", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "not-found", res.Status)
	assert.Contains(t, res.Message, "not found")
}

func TestRemoveLabelsInClusterNotFoundMapsToNotFoundStatus(t *testing.T) {
	srv := startNotFoundServer(t)
	defer srv.Close()
	res, err := RemoveLabelsInCluster(context.Background(), &fakeDeps{}, clientForServer(t, srv), "cA", "deployment", "demo", "", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "not-found", res.Status)
	assert.Contains(t, res.Message, "not found")
}

func TestAddLabelsInClusterServerErrorMapsToFailed(t *testing.T) {
	srv := startServerErrServer(t)
	defer srv.Close()
	res, err := AddLabelsInCluster(context.Background(), &fakeDeps{}, clientForServer(t, srv), "cA", "deployment", "demo", "", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", res.Status)
	assert.NotEmpty(t, res.Message)
}

func TestRemoveLabelsInClusterServerErrorMapsToFailed(t *testing.T) {
	srv := startServerErrServer(t)
	defer srv.Close()
	res, err := RemoveLabelsInCluster(context.Background(), &fakeDeps{}, clientForServer(t, srv), "cA", "deployment", "demo", "", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", res.Status)
	assert.NotEmpty(t, res.Message)
}

func TestHandleAddLabelsFailedSummaryArm(t *testing.T) {
	srv := patchAcceptingServer(t, nil)
	defer srv.Close()
	deps := &fakeDeps{clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srv)}, execErrs: map[string]error{"ghost": errors.New("cluster not found")}}
	res, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "namespace": "apps", "labels": map[string]string{"env": "prod"}, "clusters": []string{"cA", "ghost"}}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.Equal(t, 1, int(decoded["successCount"].(float64)))
	assert.Equal(t, 2, int(decoded["totalClusters"].(float64)))

	items := decoded["results"].([]interface{})
	var failed, succeeded int
	for _, item := range items {
		result := item.(map[string]interface{})
		switch result["status"] {
		case "failed":
			failed++
			assert.Equal(t, "ghost", result["cluster"])
			assert.NotEmpty(t, result["message"])
		case "labeled":
			succeeded++
			assert.Equal(t, "cA", result["cluster"])
		}
	}
	assert.Equal(t, 1, failed)
	assert.Equal(t, 1, succeeded)
}

func TestHandleRemoveLabelsFailedSummaryArm(t *testing.T) {
	srv := patchAcceptingServer(t, nil)
	defer srv.Close()
	deps := &fakeDeps{clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srv)}, execErrs: map[string]error{"ghost": errors.New("cluster not found")}}
	res, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "namespace": "apps", "labels": []string{"env"}, "clusters": []string{"cA", "ghost"}}))
	require.NoError(t, err)
	decoded := decodeLabelsResp(t, res)
	assert.Equal(t, 1, int(decoded["successCount"].(float64)))
	assert.Equal(t, 2, int(decoded["totalClusters"].(float64)))

	items := decoded["results"].([]interface{})
	var failed int
	for _, item := range items {
		result := item.(map[string]interface{})
		if result["status"] == "failed" {
			failed++
			assert.Equal(t, "ghost", result["cluster"])
			assert.NotEmpty(t, result["message"])
		}
	}
	assert.Equal(t, 1, failed)
}
