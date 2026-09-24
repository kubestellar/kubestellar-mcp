package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
)

func TestGetAppInstances_MalformedJSON(t *testing.T) {
	_, err := GetAppInstances(context.Background(), &fakeExecutor{}, json.RawMessage(`{`))
	require.Error(t, err)
}

func TestGetAppInstances_ExecutorErrorPropagates(t *testing.T) {
	executor := &fakeExecutor{execErr: errors.New("executor unavailable")}

	_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executor unavailable")
}

func TestGetAppInstances_ExecutorSuccess(t *testing.T) {
	srvA := startAppsServer(t, findAppFixtures{
		deployments:  []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3), mkDeployment("other", "app", "somethingelse", 1, 1)},
		statefulsets: []appsv1.StatefulSet{mkStatefulSet("demo-db", "app", "demo", 2, 2)},
	})
	defer srvA.Close()
	srvB := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-api", "app", "demo", 2, 2)}})
	defer srvB.Close()

	executor := &fakeExecutor{order: []string{"cA", "cB"}, clients: map[string]*kubernetes.Clientset{"cA": clientForServer(t, srvA), "cB": clientForServer(t, srvB)}}
	res, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	decoded := decodeAppInstancesResult(t, res)
	assert.Equal(t, "demo", decoded.App)
	assert.Equal(t, 3, decoded.Count)
	require.Len(t, decoded.Instances, 3)
	for _, instance := range decoded.Instances {
		assert.Contains(t, []string{"cA", "cB"}, instance.Cluster)
		assert.Contains(t, instance.Name, "demo")
	}
}

func TestGetAppInstances_BrokenClusterYieldsUnchecked(t *testing.T) {
	executor := &fakeExecutor{order: []string{"broken"}, errs: map[string]error{"broken": errors.New("boom")}}
	res, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	decoded := decodeAppInstancesResult(t, res)
	assert.Zero(t, decoded.Count)
	assert.Empty(t, decoded.Instances)
	assert.Equal(t, []string{"broken"}, decoded.UncheckedClusters)
}

func TestGetAppInstances_MixedClustersReportsHealthyPlusUnchecked(t *testing.T) {
	srv := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("demo-web", "app", "demo", 3, 3)}})
	defer srv.Close()

	executor := &fakeExecutor{
		order:   []string{"cGood", "cBroken"},
		clients: map[string]*kubernetes.Clientset{"cGood": clientForServer(t, srv)},
		errs:    map[string]error{"cBroken": errors.New("boom")},
	}
	res, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.NoError(t, err)
	decoded := decodeAppInstancesResult(t, res)
	assert.Equal(t, 1, decoded.Count)
	require.Len(t, decoded.Instances, 1)
	assert.Equal(t, []string{"cBroken"}, decoded.UncheckedClusters)
}

func TestGetAppLogs_MalformedJSON(t *testing.T) {
	_, err := GetAppLogs(context.Background(), &fakeExecutor{}, json.RawMessage(`{`))
	require.Error(t, err)
}
