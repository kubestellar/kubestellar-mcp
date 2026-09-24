package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func TestGetLogsFromCluster_InvalidNamespace(t *testing.T) {
	_, err := GetLogsFromCluster(context.Background(), nil, "c1", "demo", "kube-system", 10, "")
	require.Error(t, err)
}

func TestGetLogsFromCluster_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := GetLogsFromCluster(context.Background(), clientForServer(t, server), "c1", "demo", "", 10, "")
	require.Error(t, err)
}

func TestGetLogsFromCluster_AggregatesLinesAcrossPodsAndContainers(t *testing.T) {
	pods := []corev1.Pod{
		mkPod("demo-web-1", "app", "demo", "web", "sidecar"),
		mkPod("demo-web-2", "app", "demo", "web"),
		mkPod("other-1", "app", "other", "web"),
	}
	server := startPodsAndLogsServer(t, pods, map[string]map[string][]string{
		"demo-web-1": {"web": {"L1", "L2"}, "sidecar": {"S1"}},
		"demo-web-2": {"web": {"W1", "", "W2"}},
	})
	defer server.Close()

	got, err := GetLogsFromCluster(context.Background(), clientForServer(t, server), "cA", "demo", "app", 100, "")
	require.NoError(t, err)
	require.Len(t, got, 5)
	for _, entry := range got {
		assert.Equal(t, "cA", entry.Cluster)
	}
	messages := map[string]bool{}
	containers := map[string]bool{}
	for _, entry := range got {
		messages[entry.Message] = true
		if entry.Pod == "demo-web-1" {
			containers[entry.Container] = true
		}
	}
	for _, want := range []string{"L1", "L2", "S1", "W1", "W2"} {
		assert.True(t, messages[want], want)
	}
	assert.True(t, containers["web"])
	assert.True(t, containers["sidecar"])
}

func TestGetLogsFromCluster_SinceDurationAccepted(t *testing.T) {
	pods := []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}
	server := startPodsAndLogsServer(t, pods, map[string]map[string][]string{"demo-1": {"web": {"hi"}}})
	defer server.Close()

	got, err := GetLogsFromCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app", 50, "1h")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "hi", got[0].Message)

	got, err = GetLogsFromCluster(context.Background(), clientForServer(t, server), "c1", "demo", "app", 50, "not-a-duration")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestGetAppLogs_HappyPathAcrossClusters(t *testing.T) {
	srvA := startPodsAndLogsServer(t, []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}, map[string]map[string][]string{"demo-1": {"web": {"A1", "A2"}}})
	defer srvA.Close()
	srvB := startPodsAndLogsServer(t, []corev1.Pod{mkPod("demo-2", "app", "demo", "web")}, map[string]map[string][]string{"demo-2": {"web": {"B1"}}})
	defer srvB.Close()

	executor := &fakeExecutor{
		order: []string{"cA", "cB"},
		clients: map[string]*kubernetes.Clientset{
			"cA": clientForServer(t, srvA),
			"cB": clientForServer(t, srvB),
		},
	}
	res, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "app", "tail": 50}))
	require.NoError(t, err)

	decoded, ok := res.(map[string]interface{})
	require.True(t, ok)
	logs, ok := decoded["logs"].([]LogEntry)
	require.True(t, ok)
	assert.Equal(t, 3, decoded["logCount"])
	require.Len(t, logs, 3)
	clusters := []string{logs[0].Cluster, logs[1].Cluster, logs[2].Cluster}
	sort.Strings(clusters)
	assert.Equal(t, []string{"cA", "cA", "cB"}, clusters)
}

func TestGetAppLogs_ExecutorErrorPropagates(t *testing.T) {
	executor := &fakeExecutor{execErr: errors.New("executor unavailable")}

	_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executor unavailable")
}

func TestGetAppLogs_TailDefaultsTo100(t *testing.T) {
	srv := startPodsAndLogsServer(t, []corev1.Pod{mkPod("demo-1", "app", "demo", "web")}, map[string]map[string][]string{"demo-1": {"web": {"only"}}})
	defer srv.Close()

	executor := &fakeExecutor{order: []string{"c1"}, clients: map[string]*kubernetes.Clientset{"c1": clientForServer(t, srv)}}
	res, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "app"}))
	require.NoError(t, err)

	decoded, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, decoded["logCount"])
}
