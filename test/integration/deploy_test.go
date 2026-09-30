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
	"k8s.io/client-go/rest"

	"github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp/deploy"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// newDeployDeps wires deploy.Deps directly to the shared envtest environment
// with a single hard-coded cluster, mirroring newKubectlDeps in
// kubectl_test.go and newLabelsDeps in labels_test.go (all three packages sit
// under pkg/deploy/mcp and share the same multicluster.Deps-style wiring
// rather than the pkg/mcp/server/handlers.Registry pattern used by
// workloads_test.go/rbac_test.go). Only the fields the scale_app/patch_app/
// list_cluster_capabilities handlers actually touch are wired; the manifest
// reader/syncer factories used by deploy_app are left nil (its git-syncer
// path is exercised by the package's own unit tests) so this suite stays a
// focused, real-apiserver round-trip for the direct-client tools.
func newDeployDeps(t *testing.T) deploy.Deps {
	t.Helper()

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	execute := func(ctx context.Context, clusterName string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		name := clusterName
		if name == "" {
			name = integrationClusterName
		}
		result, err := fn(ctx, clientset, name)
		if err != nil {
			return []multicluster.ClusterResult{{Cluster: name, Error: err.Error()}}, nil
		}
		return []multicluster.ClusterResult{{Cluster: name, Result: result}}, nil
	}

	executeOnSelected := func(ctx context.Context, clusterNames []string, fn multicluster.ExecuteFunc) ([]multicluster.ClusterResult, error) {
		results := make([]multicluster.ClusterResult, 0, len(clusterNames))
		for _, name := range clusterNames {
			result, err := fn(ctx, clientset, name)
			if err != nil {
				results = append(results, multicluster.ClusterResult{Cluster: name, Error: err.Error()})
				continue
			}
			results = append(results, multicluster.ClusterResult{Cluster: name, Result: result})
		}
		return results, nil
	}

	// The real *Server wires GetCapabilitiesForCluster to a
	// *multicluster.Selector; that method only reads Nodes().List and never
	// touches the selector's executor, so a nil-executor Selector is a
	// behavior-identical stand-in here.
	selector := multicluster.NewSelector(nil)

	return deploy.Deps{
		Execute:                   execute,
		ExecuteOnSelected:         executeOnSelected,
		DiscoverClusters:          func() ([]multicluster.ClusterInfo, error) { return []multicluster.ClusterInfo{{Name: integrationClusterName}}, nil },
		GetConfig:                 func(string) (*rest.Config, error) { return testCfg, nil },
		GetCapabilitiesForCluster: selector.GetCapabilitiesForCluster,
		ValidateManifestDocs:      func(string) error { return nil },
	}
}

// seedDeployDeployment creates a Deployment the deploy-domain tools can find
// via app.MatchesApp (its "app" label matches the app name), returning the
// namespace/name it used.
func seedDeployDeployment(t *testing.T, clientset *kubernetes.Clientset, namespace, name string, replicas int32) {
	t.Helper()
	ctx := context.Background()

	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoErrorf(t, err, "create namespace %s", namespace)

	labels := map[string]string{"app": name}
	r := replicas
	_, err = clientset.AppsV1().Deployments(namespace).Create(ctx, &appsv1.Deployment{
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
}

// TestDeployScalePatchCapabilities exercises pkg/deploy/mcp/deploy's
// scale_app, patch_app, and list_cluster_capabilities MCP tools end-to-end
// against a real kube-apiserver (envtest): a Deployment is seeded through the
// typed clientset, scale_app changes its replica count (verified on the live
// object), patch_app applies a strategic-merge patch (verified on the live
// object), and list_cluster_capabilities runs the real Nodes().List path
// (envtest has no kubelet, so it reports zero nodes). This is the real HTTP
// round-trip that the package's own unit tests (deploy_test.go,
// cluster_ops_test.go) fake with httptest servers and fake clientsets.
func TestDeployScalePatchCapabilities(t *testing.T) {
	ctx := context.Background()
	d := newDeployDeps(t)

	clientset, err := kubernetes.NewForConfig(testCfg)
	require.NoError(t, err, "kubernetes.NewForConfig")

	const namespace = "mcp-integration-deploy"
	const appName = "checkout"
	const initialReplicas int32 = 2
	seedDeployDeployment(t, clientset, namespace, appName, initialReplicas)

	t.Run("scale_app changes the live deployment replica count", func(t *testing.T) {
		const targetReplicas int32 = 5
		args, err := json.Marshal(map[string]interface{}{
			"app":       appName,
			"namespace": namespace,
			"replicas":  targetReplicas,
			"clusters":  []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(scale_app args)")

		res, err := deploy.HandleScaleApp(ctx, d, args)
		require.NoError(t, err, "HandleScaleApp")

		m, ok := res.(map[string]interface{})
		require.Truef(t, ok, "expected map[string]interface{} result, got %T", res)
		results, ok := m["results"].([]multicluster.ClusterResult)
		require.Truef(t, ok, "expected []multicluster.ClusterResult in results, got %#v", m["results"])
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.Empty(t, results[0].Error, "scale_app per-cluster error")

		dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, appName, metav1.GetOptions{})
		require.NoError(t, err, "Get deployment after scale_app")
		require.NotNil(t, dep.Spec.Replicas, "deployment spec.replicas after scale_app")
		require.Equal(t, targetReplicas, *dep.Spec.Replicas, "live deployment replica count after scale_app")
	})

	t.Run("patch_app applies a strategic-merge patch to the live deployment", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"app":        appName,
			"namespace":  namespace,
			"patch":      `{"metadata":{"labels":{"tier":"frontend"}}}`,
			"patch_type": "strategic",
			"clusters":   []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(patch_app args)")

		res, err := deploy.HandlePatchApp(ctx, d, args)
		require.NoError(t, err, "HandlePatchApp")

		m, ok := res.(map[string]interface{})
		require.Truef(t, ok, "expected map[string]interface{} result, got %T", res)
		results, ok := m["results"].([]multicluster.ClusterResult)
		require.Truef(t, ok, "expected []multicluster.ClusterResult in results, got %#v", m["results"])
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.Empty(t, results[0].Error, "patch_app per-cluster error")

		dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, appName, metav1.GetOptions{})
		require.NoError(t, err, "Get deployment after patch_app")
		require.Equal(t, "frontend", dep.Labels["tier"], "live deployment label after patch_app")
	})

	t.Run("list_cluster_capabilities reports node capacity from the live apiserver", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"cluster": integrationClusterName,
		})
		require.NoError(t, err, "json.Marshal(list_cluster_capabilities args)")

		res, err := deploy.HandleListClusterCapabilities(ctx, d, args)
		require.NoError(t, err, "HandleListClusterCapabilities")

		cap, ok := res.(*multicluster.ClusterCapabilities)
		require.Truef(t, ok, "expected *multicluster.ClusterCapabilities result, got %T", res)
		require.Equal(t, integrationClusterName, cap.Cluster, "capabilities cluster name")
		// envtest runs no kubelet, so there are no nodes to report; the real
		// Nodes().List round-trip still succeeds and returns an empty set.
		require.Equal(t, 0, cap.NodeCount, "envtest reports zero nodes")
		require.Equal(t, 0, cap.ReadyNodes, "envtest reports zero ready nodes")
	})

	t.Run("scale_app reports app-not-found for a missing deployment", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{
			"app":       "does-not-exist",
			"namespace": namespace,
			"replicas":  int32(1),
			"clusters":  []string{integrationClusterName},
		})
		require.NoError(t, err, "json.Marshal(scale_app args, missing app)")

		res, err := deploy.HandleScaleApp(ctx, d, args)
		require.NoError(t, err, "HandleScaleApp (missing app)")

		m, ok := res.(map[string]interface{})
		require.Truef(t, ok, "expected map[string]interface{} result, got %T", res)
		results, ok := m["results"].([]multicluster.ClusterResult)
		require.Truef(t, ok, "expected []multicluster.ClusterResult in results, got %#v", m["results"])
		require.Len(t, results, 1, "expected exactly one per-cluster result")
		require.NotEmpty(t, results[0].Error, "scale_app on a missing deployment should surface a per-cluster error")
	})
}
