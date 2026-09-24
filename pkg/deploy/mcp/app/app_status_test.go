package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
)

func TestGetAppStatus_MalformedJSON(t *testing.T) {
	_, err := GetAppStatus(context.Background(), &fakeExecutor{}, json.RawMessage(`{`))
	require.Error(t, err)
}

func TestGetAppStatus_ExecutorErrorPropagates(t *testing.T) {
	executor := &fakeExecutor{execErr: errors.New("executor unavailable")}

	_, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executor unavailable")
}

func TestGetAppStatus_HealthyAcrossClusters(t *testing.T) {
	srvA := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}})
	defer srvA.Close()
	srvB := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-api", "app", "demo", 2, 2)}})
	defer srvB.Close()

	executor := &fakeExecutor{order: []string{"cA", "cB"}, clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srvA), "cB": clientForServer(t, srvB)}}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "healthy", status.OverallStatus)
	assert.Equal(t, 2, status.TotalClusters)
	assert.Equal(t, 2, status.HealthyClusters)
	assert.Equal(t, int32(5), status.TotalReplicas)
	assert.Equal(t, int32(5), status.ReadyReplicas)
	assert.Empty(t, status.Issues)
}

func TestGetAppStatus_DegradedMixed(t *testing.T) {
	srvA := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}})
	defer srvA.Close()
	srvB := startAppsServer(t, findAppFixtures{statefulsets: []appsv1.StatefulSet{mkStatefulSet("demo-db", "app", "demo", 3, 1)}})
	defer srvB.Close()

	executor := &fakeExecutor{order: []string{"cA", "cB"}, clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srvA), "cB": clientForServer(t, srvB)}}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "degraded", status.OverallStatus)
	assert.Equal(t, 1, status.HealthyClusters)
	assert.Equal(t, 2, status.TotalClusters)
	assert.Condition(t, func() bool {
		for _, issue := range status.Issues {
			if strings.Contains(issue, "cB/demo-db") && strings.Contains(issue, "degraded") {
				return true
			}
		}
		return false
	})
}

func TestGetAppStatus_AllFailedYieldsFailedOverall(t *testing.T) {
	srv := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 0)}})
	defer srv.Close()

	executor := &fakeExecutor{order: []string{"cA"}, clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srv)}}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "failed", status.OverallStatus)
	assert.Equal(t, 0, status.HealthyClusters)
	assert.Equal(t, 1, status.TotalClusters)
}

func TestGetAppStatus_NotFoundWhenNoInstances(t *testing.T) {
	srv := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("other", "app", "other", 1, 1)}})
	defer srv.Close()

	executor := &fakeExecutor{order: []string{"cA"}, clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srv)}}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "not found", status.OverallStatus)
	assert.Zero(t, status.TotalClusters)
}

func TestGetAppStatus_BrokenClusterYieldsUnknown(t *testing.T) {
	executor := &fakeExecutor{order: []string{"brokenCluster"}, errs: map[string]error{"brokenCluster": errors.New("boom")}}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "unknown", status.OverallStatus)
	assert.Zero(t, status.TotalClusters)
	assert.Equal(t, 1, status.UncheckedClusters)
	require.Len(t, status.Issues, 1)
}

func TestGetAppStatus_HealthyPlusUnreachableYieldsDegraded(t *testing.T) {
	srv := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}})
	defer srv.Close()

	executor := &fakeExecutor{
		order:   []string{"cA", "cB"},
		clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srv)},
		errs:    map[string]error{"cB": errors.New("boom")},
	}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	assert.Equal(t, "degraded", status.OverallStatus)
	assert.Equal(t, 1, status.TotalClusters)
	assert.Equal(t, 1, status.HealthyClusters)
	assert.Equal(t, 1, status.UncheckedClusters)
}

func TestGetAppStatus_InstancesFromEveryCluster(t *testing.T) {
	clients := map[string]*kubernetes.Clientset{}
	for _, cluster := range []string{"c1", "c2", "c3"} {
		srv := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo", "app", "demo", 1, 1)}})
		defer srv.Close()
		clients[cluster] = clientForServer(t, srv)
	}

	executor := &fakeExecutor{order: []string{"c1", "c2", "c3"}, clients: clients}
	res, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	status := decodeAppStatusResult(t, res)
	require.Len(t, status.Instances, 3)
	got := []string{status.Instances[0].Cluster, status.Instances[1].Cluster, status.Instances[2].Cluster}
	sort.Strings(got)
	assert.Equal(t, []string{"c1", "c2", "c3"}, got)
}
