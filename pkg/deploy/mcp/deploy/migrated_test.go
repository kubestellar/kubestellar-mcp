package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func executeResultsCount(t *testing.T, res interface{}) int {
	t.Helper()
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("res = %#v, want map", res)
	}
	items, ok := m["results"].([]multicluster.ClusterResult)
	if !ok {
		t.Fatalf("results field = %#v (%T), want []multicluster.ClusterResult", m["results"], m["results"])
	}
	return len(items)
}

func nodesHandlerServer(t *testing.T) *httptest.Server {
	t.Helper()
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "n1",
			Labels: map[string]string{"topology.kubernetes.io/region": "us-east-1"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes" {
			http.NotFound(w, r)
			return
		}
		list := corev1.NodeList{
			TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"},
			Items:    []corev1.Node{node},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&list)
	}))
}

func TestHandleListClusterCapabilities_RealSingleClusterSuccess(t *testing.T) {
	srv := nodesHandlerServer(t)
	defer srv.Close()

	got, err := HandleListClusterCapabilities(
		context.Background(),
		newRealDeps(t, map[string]string{"alpha": srv.URL}),
		mustMarshalJSON(t, map[string]interface{}{"cluster": "alpha"}),
	)
	require.NoError(t, err)

	caps, ok := got.(*multicluster.ClusterCapabilities)
	require.True(t, ok, "expected *multicluster.ClusterCapabilities, got %T", got)
	assert.Equal(t, "alpha", caps.Cluster)
	assert.Equal(t, 1, caps.NodeCount)
	assert.Equal(t, 1, caps.ReadyNodes)
}

func TestHandleListClusterCapabilities_SingleClusterListNodesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := HandleListClusterCapabilities(
		context.Background(),
		newRealDeps(t, map[string]string{"alpha": srv.URL}),
		mustMarshalJSON(t, map[string]interface{}{"cluster": "alpha"}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get capabilities for cluster alpha")
}

func TestHandleFindClustersForWorkload_ReturnsFullRequirements(t *testing.T) {
	got, err := HandleFindClustersForWorkload(context.Background(), newRealDeps(t, map[string]string{}), mustMarshalJSON(t, map[string]interface{}{
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

func TestHandleDeployApp_DryRunAcrossClustersMultipleDocs(t *testing.T) {
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

	got, err := HandleDeployApp(context.Background(), newRealDeps(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	}), mustMarshalJSON(t, map[string]interface{}{
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

func TestHandleDeployAppRejectsSensitiveKinds(t *testing.T) {
	manifest := `apiVersion: v1
kind: Secret
metadata:
  name: demo-secret
`

	_, err := HandleDeployApp(context.Background(), newRealDeps(t, map[string]string{"alpha": "https://alpha.example.com"}), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked via MCP kubectl tools")
}

func TestHandleDeployAppRejectsSystemNamespaces(t *testing.T) {
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: kube-system
`

	_, err := HandleDeployApp(context.Background(), newRealDeps(t, map[string]string{"alpha": "https://alpha.example.com"}), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"clusters": []string{"alpha"},
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace in manifest")
}

func TestApplyManifest_UsesSyncerForAdditionalKinds(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = realManifestReaderAdapter{reader: gitops.NewManifestReaderWithSchemes(map[string]bool{
		"https": true,
		"http":  true,
		"file":  true,
	})}
	fakeSyncer := &capturingManifestSyncer{}
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) {
		return fakeSyncer, nil
	}

	manifest := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: demo-statefulset
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: demo-daemonset
---
apiVersion: batch/v1
kind: Job
metadata:
  name: demo-job
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: demo-cronjob
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: demo-ingress
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: demo-network-policy
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: demo-pvc
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: demo-service-account
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: demo-role
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: demo-rolebinding
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: demo-clusterrole
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: demo-clusterrolebinding
`

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 12)
	assert.Equal(t, []string{
		"StatefulSet",
		"DaemonSet",
		"Job",
		"CronJob",
		"Ingress",
		"NetworkPolicy",
		"PersistentVolumeClaim",
		"ServiceAccount",
		"Role",
		"RoleBinding",
		"ClusterRole",
		"ClusterRoleBinding",
	}, fakeSyncer.kinds)
	for _, result := range results {
		assert.Equal(t, "created", result.Status)
	}
}

func TestHandlePatchApp_RealTwoClustersStrategic(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 3, 3)}},
		"cB": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 2, 2)}},
	})
	defer cleanup()

	res, err := HandlePatchApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":9}}","clusters":["cA","cB"]}`))
	require.NoError(t, err)
	assert.Equal(t, 2, executeResultsCount(t, res))
	m := res.(map[string]interface{})
	assert.Equal(t, "demo", m["app"].(string))
}

func TestHandlePatchApp_RealMergePatchType(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	res, err := HandlePatchApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":5}}","patch_type":"merge","clusters":["cA"]}`))
	require.NoError(t, err)
	assert.Equal(t, 1, executeResultsCount(t, res))
}

func TestHandlePatchApp_RealJSONPatchType(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	res, err := HandlePatchApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"demo","namespace":"apps","patch":"[{\"op\":\"replace\",\"path\":\"/spec/replicas\",\"value\":4}]","patch_type":"json","clusters":["cA"]}`))
	require.NoError(t, err)
	assert.Equal(t, 1, executeResultsCount(t, res))
}

func TestHandlePatchApp_RealDiscoverClustersFallback(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"only": {deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)}},
	})
	defer cleanup()

	res, err := HandlePatchApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"demo","namespace":"apps","patch":"{\"spec\":{\"replicas\":2}}"}`))
	require.NoError(t, err)
	assert.Equal(t, 1, executeResultsCount(t, res))
}

func TestHandleScaleApp_RealNoClustersAndNoInstancesReturnsNotFoundError(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"only": {deployments: []appsv1.Deployment{mkDeployment("other", "apps", "other", 1, 1)}},
	})
	defer cleanup()

	_, err := HandleScaleApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"missing-app","namespace":"apps","replicas":3}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-app")
	assert.Contains(t, err.Error(), "not found in any cluster")
}

func TestHandleScaleApp_RealAutoDiscoversClustersFromInstances(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"discovered-cluster": {
			deployments: []appsv1.Deployment{mkDeployment("demo", "apps", "demo", 1, 1)},
		},
	})
	defer cleanup()

	res, err := HandleScaleApp(context.Background(), realDepsFromManager(mgr), json.RawMessage(`{"app":"demo","namespace":"apps","replicas":4}`))
	require.NoError(t, err)
	m, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "demo", m["app"].(string))
	assert.Equal(t, int32(4), m["replicas"].(int32))
	assert.Equal(t, 1, executeResultsCount(t, res))
}

func TestHandleScaleAppRejectsMalformedNamespace(t *testing.T) {
	_, err := HandleScaleApp(context.Background(), newRealDeps(t, map[string]string{}), mustMarshalJSON(t, map[string]interface{}{
		"app":       "demo",
		"replicas":  3,
		"namespace": "BadNamespace",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
}
