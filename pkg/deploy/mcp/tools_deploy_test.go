package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestHandleListClusterCapabilitiesReturnsEmptySliceWithoutClusters(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleListClusterCapabilities(context.Background(), nil)
	require.NoError(t, err)

	capabilities, ok := got.([]multicluster.ClusterCapabilities)
	require.True(t, ok)
	assert.Len(t, capabilities, 0)
}

func TestHandleListClusterCapabilitiesReturnsErrorForUnknownCluster(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleListClusterCapabilities(context.Background(), mustMarshalJSON(t, map[string]interface{}{"cluster": "missing"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get capabilities for cluster missing")
}

func TestHandleListClusterCapabilitiesValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleListClusterCapabilities(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid parameters")
}

func TestHandleFindClustersForWorkloadValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleFindClustersForWorkload(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleFindClustersForWorkloadReturnsRequirements(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleFindClustersForWorkload(context.Background(), mustMarshalJSON(t, map[string]interface{}{
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

func TestHandleDeployAppValidatesArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleDeployApp(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleDeployAppDryRunAcrossClusters(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com", "beta": "https://beta.example.com"})
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

	got, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
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

func TestHandleDeployAppReturnsNoMatchingClustersForGPURequirements(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	manifest := `apiVersion: v1
kind: Pod
metadata:
  name: demo
`

	_, err := server.handleDeployApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"gpu_type": "nvidia.com/gpu",
		"min_gpu":  1,
		"dry_run":  true,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters found matching requirements")
}

func TestApplyManifestDryRunDefaultsNamespace(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
`

	results, err := server.applyManifest(context.Background(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Contains(t, results[0].Message, "namespace default")
}

func TestApplyManifestReturnsDecodeError(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.applyManifest(context.Background(), nil, "alpha", "[", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestHandleScaleAppRequiresExistingAppWhenNoClustersSpecified(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{"app": "demo", "replicas": 3}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app demo not found in any cluster")
}

func TestHandleScaleAppExplicitMissingClusterReturnsClusterError(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	got, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app":      "demo",
		"replicas": 3,
		"clusters": []string{"missing"},
	}))
	require.NoError(t, err)

	result := got.(map[string]interface{})
	clusterResults := result["results"].([]multicluster.ClusterResult)
	require.Len(t, clusterResults, 1)
	assert.Equal(t, "missing", clusterResults[0].Cluster)
	assert.Contains(t, clusterResults[0].Error, "context \"missing\" does not exist")
}

func TestApplyManifestUsesSyncerForAdditionalKinds(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})
	fakeSyncer := &capturingManifestSyncer{}
	server.newManifestSyncer = func(*rest.Config) (manifestSyncer, error) {
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

	results, err := server.applyManifest(context.Background(), nil, "alpha", manifest, false)
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

func TestApplyResourceFunctionsUseServerSideApplyPatch(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	tests := []struct {
		name      string
		resource  string
		existing  runtime.Object
		raw       map[string]interface{}
		applyFunc func(context.Context, kubernetes.Interface, map[string]interface{}, string) (string, error)
	}{
		{
			name:     "deployment",
			resource: "deployments",
			existing: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]interface{}{"name": "demo"},
			},
			applyFunc: func(ctx context.Context, client kubernetes.Interface, raw map[string]interface{}, namespace string) (string, error) {
				return server.applyDeployment(ctx, client, raw, namespace)
			},
		},
		{
			name:     "service",
			resource: "services",
			existing: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata":   map[string]interface{}{"name": "demo"},
			},
			applyFunc: func(ctx context.Context, client kubernetes.Interface, raw map[string]interface{}, namespace string) (string, error) {
				return server.applyService(ctx, client, raw, namespace)
			},
		},
		{
			name:     "configmap",
			resource: "configmaps",
			existing: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]interface{}{"name": "demo"},
			},
			applyFunc: func(ctx context.Context, client kubernetes.Interface, raw map[string]interface{}, namespace string) (string, error) {
				return server.applyConfigMap(ctx, client, raw, namespace)
			},
		},
		{
			name:     "secret",
			resource: "secrets",
			existing: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]interface{}{"name": "demo"},
			},
			applyFunc: func(ctx context.Context, client kubernetes.Interface, raw map[string]interface{}, namespace string) (string, error) {
				return server.applySecret(ctx, client, raw, namespace)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset(tt.existing)
			client.PrependReactor("patch", tt.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, ok := action.(k8stesting.PatchAction)
				require.True(t, ok)
				assert.Equal(t, types.ApplyPatchType, patchAction.GetPatchType())

				var raw map[string]interface{}
				require.NoError(t, json.Unmarshal(patchAction.GetPatch(), &raw))
				metadata, ok := raw["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "demo", metadata["name"])
				assert.Equal(t, "default", metadata["namespace"])

				updated := tt.existing.DeepCopyObject()
				switch obj := updated.(type) {
				case *appsv1.Deployment:
					obj.ResourceVersion = "2"
				case *corev1.Service:
					obj.ResourceVersion = "2"
				case *corev1.ConfigMap:
					obj.ResourceVersion = "2"
				case *corev1.Secret:
					obj.ResourceVersion = "2"
				}
				return true, updated, nil
			})

			status, err := tt.applyFunc(context.Background(), client, tt.raw, "default")
			require.NoError(t, err)
			assert.Equal(t, "updated", status)
			for _, action := range client.Actions() {
				assert.NotEqual(t, "update", action.GetVerb())
			}
		})
	}
}

type capturingManifestSyncer struct {
	kinds []string
}

func (s *capturingManifestSyncer) Sync(_ context.Context, manifests []gitops.Manifest, clusterName string, _ gitops.SyncOptions) (*gitops.SyncSummary, error) {
	results := make([]gitops.SyncResult, 0, len(manifests))
	for _, manifest := range manifests {
		s.kinds = append(s.kinds, manifest.Kind)
		results = append(results, gitops.SyncResult{
			Cluster:   clusterName,
			Kind:      manifest.Kind,
			Name:      manifest.Metadata.Name,
			Namespace: manifest.Metadata.Namespace,
			Action:    gitops.SyncActionCreated,
		})
	}
	return &gitops.SyncSummary{Cluster: clusterName, Created: len(results), Results: results}, nil
}
