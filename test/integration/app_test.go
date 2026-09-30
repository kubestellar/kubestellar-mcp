//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/app"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// integrationAppExecutor satisfies app.Executor by running the callback once
// against the shared envtest clientset under a single synthetic cluster name,
// mirroring how the real *multicluster.Executor fans a callback out across
// clusters (here there is exactly one). app.GetAppStatus/GetAppInstances call
// Execute with an empty cluster name to mean "all clusters".
type integrationAppExecutor struct {
	clientset *kubernetes.Clientset
}

func (e integrationAppExecutor) Execute(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
	name := clusterName
	if name == "" {
		name = integrationClusterName
	}
	result, err := fn(ctx, e.clientset, name)
	if err != nil {
		return []multicluster.ClusterResult{{Cluster: name, Error: err.Error()}}, nil
	}
	return []multicluster.ClusterResult{{Cluster: name, Result: result}}, nil
}

// seedAppDeployment creates a Deployment whose /status subresource reports it
// fully available, so app.GetDeploymentStatus classifies it "healthy".
// envtest runs no controller-manager, so the status must be written directly.
func seedAppDeployment(t *testing.T, clientset *kubernetes.Clientset, namespace, name string, replicas int32) {
	t.Helper()
	ctx := context.Background()

	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoErrorf(t, err, "create namespace %s", namespace)

	labels := map[string]string{"app": name}
	r := replicas
	created, err := clientset.AppsV1().Deployments(namespace).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &r,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "registry.k8s.io/pause:3.9"},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	require.NoErrorf(t, err, "create deployment %s/%s", namespace, name)

	created.Status = appsv1.DeploymentStatus{
		Replicas:          replicas,
		ReadyReplicas:     replicas,
		AvailableReplicas: replicas,
	}
	_, err = clientset.AppsV1().Deployments(namespace).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoErrorf(t, err, "update deployment status %s/%s", namespace, name)
}

// TestAppInstancesAndStatus exercises pkg/deploy/mcp/app's get_app_instances
// and get_app_status MCP tools end-to-end against a real kube-apiserver
// (envtest): a Deployment is seeded through the typed clientset with a
// hand-written /status reporting full availability, then each tool is driven
// through its exported handler (the same functions the app_adapter.go wires
// into the deploy server) and asserted on. get_app_logs is deliberately left
// out — pod-log streaming needs a real kubelet, which envtest does not run.
// This is the real HTTP round-trip that the package's own unit tests
// (app_test.go, app_status_test.go) fake with fake clientsets.
func TestAppInstancesAndStatus(t *testing.T) {
	ctx := context.Background()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-app"
	const appName = "billing"
	const replicas int32 = 3
	seedAppDeployment(t, clientset, namespace, appName, replicas)

	executor := integrationAppExecutor{clientset: clientset}

	t.Run("get_app_instances finds the seeded deployment", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"app":       appName,
			"namespace": namespace,
		})
		require.NoError(t, err, "json.Marshal(get_app_instances args)")

		res, err := app.GetAppInstances(ctx, executor, args)
		require.NoError(t, err, "GetAppInstances")

		m, ok := res.(map[string]interface{})
		require.Truef(t, ok, "expected map[string]interface{} result, got %T", res)
		require.Equal(t, 1, m["count"], "get_app_instances count")

		instances, ok := m["instances"].([]app.AppInstance)
		require.Truef(t, ok, "expected []app.AppInstance in instances, got %#v", m["instances"])
		require.Len(t, instances, 1, "expected exactly one instance")
		require.Equal(t, appName, instances[0].Name, "instance name")
		require.Equal(t, "Deployment", instances[0].Kind, "instance kind")
		require.Equal(t, replicas, instances[0].Replicas, "instance replicas")
	})

	t.Run("get_app_status aggregates the seeded deployment as healthy", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"app":       appName,
			"namespace": namespace,
		})
		require.NoError(t, err, "json.Marshal(get_app_status args)")

		res, err := app.GetAppStatus(ctx, executor, args)
		require.NoError(t, err, "GetAppStatus")

		status, ok := res.(app.AppStatus)
		require.Truef(t, ok, "expected app.AppStatus result, got %T", res)
		require.Equal(t, "healthy", status.OverallStatus, "overall status")
		require.Equal(t, 1, status.TotalClusters, "total clusters")
		require.Equal(t, 1, status.HealthyClusters, "healthy clusters")
		require.Equal(t, replicas, status.ReadyReplicas, "ready replicas")
	})

	t.Run("get_app_status reports not found for a missing app", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"app":       "nonexistent",
			"namespace": namespace,
		})
		require.NoError(t, err, "json.Marshal(get_app_status args, missing app)")

		res, err := app.GetAppStatus(ctx, executor, args)
		require.NoError(t, err, "GetAppStatus (missing app)")

		status, ok := res.(app.AppStatus)
		require.Truef(t, ok, "expected app.AppStatus result, got %T", res)
		require.Equal(t, "not found", status.OverallStatus, "overall status for a missing app")
	})
}
