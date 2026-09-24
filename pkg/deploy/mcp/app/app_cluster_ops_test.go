package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
)

func TestFindAppInCluster_InvalidNamespace(t *testing.T) {
	_, err := FindAppInCluster(context.Background(), nil, "c1", "demo", "kube-system")
	require.Error(t, err)
}

func TestFindAppInCluster_MatchesAcrossKinds(t *testing.T) {
	server := startAppsServer(t, findAppFixtures{
		deployments: []appsv1.Deployment{
			mkDeployment("demo-web", "app", "demo", 3, 3),
			mkDeployment("unrelated", "other", "somethingelse", 1, 1),
		},
		statefulsets: []appsv1.StatefulSet{mkStatefulSet("demo-db", "app", "demo", 2, 1)},
		daemonsets:   []appsv1.DaemonSet{mkDaemonSet("demo-daemon", "app", "demo", 3, 0)},
	})
	defer server.Close()

	got, err := FindAppInCluster(context.Background(), clientForServer(t, server), "cA", "demo", "")
	require.NoError(t, err)
	require.Len(t, got, 3)

	byKind := map[string]AppInstance{}
	for _, instance := range got {
		byKind[instance.Kind] = instance
		assert.Equal(t, "cA", instance.Cluster)
	}
	assert.Equal(t, AppInstance{Cluster: "cA", Namespace: "app", Name: "demo-web", Kind: "Deployment", Replicas: 3, ReadyReplicas: 3, Status: "healthy"}, byKind["Deployment"])
	assert.Equal(t, AppInstance{Cluster: "cA", Namespace: "app", Name: "demo-db", Kind: "StatefulSet", Replicas: 2, ReadyReplicas: 1, Status: "degraded"}, byKind["StatefulSet"])
	assert.Equal(t, AppInstance{Cluster: "cA", Namespace: "app", Name: "demo-daemon", Kind: "DaemonSet", Replicas: 3, ReadyReplicas: 0, Status: "failed"}, byKind["DaemonSet"])
}

func TestFindAppInCluster_NoMatches(t *testing.T) {
	server := startAppsServer(t, findAppFixtures{deployments: []appsv1.Deployment{mkDeployment("other", "app", "other", 1, 1)}})
	defer server.Close()

	got, err := FindAppInCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFindAppInCluster_ListErrorReturnsJoinedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	got, err := FindAppInCluster(context.Background(), clientForServer(t, server), "c1", "demo", "")
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "list deployments")
	assert.Contains(t, err.Error(), "list statefulsets")
	assert.Contains(t, err.Error(), "list daemonsets")
}
