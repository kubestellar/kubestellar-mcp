package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestApplyManifest_DryRunDefaultsNamespace(t *testing.T) {
	fd := newFakeDeps()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Contains(t, results[0].Message, "namespace default")
}

func TestApplyManifest_DryRunSkipsNilDocuments(t *testing.T) {
	fd := newFakeDeps()
	manifest := "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n---\n---\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Resource, "ConfigMap/demo")
}

func TestApplyManifest_DryRunReportsInvalidNamespace(t *testing.T) {
	fd := newFakeDeps()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: kube-system\n"

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", manifest, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "invalid namespace in manifest")
}

func TestApplyManifest_DryRunDecodeError(t *testing.T) {
	fd := newFakeDeps()
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "[", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestApplyManifest_NonDryRunDecodeError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{err: errors.New("decode boom")}
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode manifest")
}

func TestApplyManifest_NonDryRunGetConfigError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	fd.configErrs["does-not-exist"] = errors.New("no such cluster")
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "does-not-exist", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get config for cluster does-not-exist")
}

func TestApplyManifest_NonDryRunSyncerFactoryError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	factoryErr := errors.New("factory boom")
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) { return nil, factoryErr }
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create manifest syncer")
	assert.ErrorIs(t, err, factoryErr)
}

type erroringManifestSyncer struct{ err error }

func (e *erroringManifestSyncer) Sync(context.Context, []gitops.Manifest, string, gitops.SyncOptions) (*gitops.SyncSummary, error) {
	return nil, e.err
}

func TestApplyManifest_NonDryRunSyncError(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{}
	syncErr := errors.New("sync boom")
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) {
		return &erroringManifestSyncer{err: syncErr}, nil
	}
	_, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply manifest")
	assert.ErrorIs(t, err, syncErr)
}

type capturingManifestSyncer struct{ kinds []string }

func (s *capturingManifestSyncer) Sync(_ context.Context, manifests []gitops.Manifest, clusterName string, _ gitops.SyncOptions) (*gitops.SyncSummary, error) {
	results := make([]gitops.SyncResult, 0, len(manifests))
	for _, manifest := range manifests {
		s.kinds = append(s.kinds, manifest.Kind)
		results = append(results, gitops.SyncResult{
			Cluster: clusterName, Kind: manifest.Kind, Name: manifest.Metadata.Name,
			Namespace: manifest.Metadata.Namespace, Action: gitops.SyncActionCreated,
		})
	}
	return &gitops.SyncSummary{Cluster: clusterName, Created: len(results), Results: results}, nil
}

func TestApplyManifest_NonDryRunSuccess(t *testing.T) {
	fd := newFakeDeps()
	fd.manifestReader = &fakeManifestReader{manifests: []gitops.Manifest{
		{Kind: "ConfigMap", Metadata: gitops.ManifestMetadata{Name: "demo"}},
	}}
	syncer := &capturingManifestSyncer{}
	fd.manifestSyncer = func(*rest.Config) (ManifestSyncer, error) { return syncer, nil }

	results, err := ApplyManifest(context.Background(), fd.deps(), nil, "alpha", "irrelevant", false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "created", results[0].Status)
	assert.Equal(t, []string{"ConfigMap"}, syncer.kinds)
}

// applyBranchCase represents a scenario for exercising the created /
// unchanged / error branches of the Apply{Deployment,Service,ConfigMap,Secret}
// helpers.
type applyBranchCase struct {
	name      string
	resource  string
	raw       map[string]interface{}
	applyFunc func(context.Context, kubernetes.Interface, map[string]interface{}, string) (string, error)
	makeObj   func(rv string) runtime.Object
}

func applyBranchCases() []applyBranchCase {
	rawDeployment := map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "demo"}}
	rawService := map[string]interface{}{"apiVersion": "v1", "kind": "Service", "metadata": map[string]interface{}{"name": "demo"}}
	rawConfigMap := map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "demo"}}
	rawSecret := map[string]interface{}{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]interface{}{"name": "demo"}}

	return []applyBranchCase{
		{
			name: "deployment", resource: "deployments", raw: rawDeployment,
			applyFunc: ApplyDeployment,
			makeObj: func(rv string) runtime.Object {
				return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name: "service", resource: "services", raw: rawService,
			applyFunc: ApplyService,
			makeObj: func(rv string) runtime.Object {
				return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name: "configmap", resource: "configmaps", raw: rawConfigMap,
			applyFunc: ApplyConfigMap,
			makeObj: func(rv string) runtime.Object {
				return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
		{
			name: "secret", resource: "secrets", raw: rawSecret,
			applyFunc: ApplySecret,
			makeObj: func(rv string) runtime.Object {
				return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: rv}}
			},
		},
	}
}

func TestApplyResourceFunctions_NewResourceReturnsUpdatedNotCreated(t *testing.T) {
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			client.PrependReactor("get", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: tc.resource}, "demo")
			})
			client.PrependReactor("patch", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, tc.makeObj("1"), nil
			})

			status, err := tc.applyFunc(context.Background(), client, tc.raw, "default")
			require.NoError(t, err)
			assert.Equal(t, "updated", status)
		})
	}
}

func TestApplyResourceFunctions_UnchangedBranch(t *testing.T) {
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			existing := tc.makeObj("42")
			client := kubernetesfake.NewSimpleClientset(existing)
			client.PrependReactor("patch", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, tc.makeObj("42"), nil
			})

			status, err := tc.applyFunc(context.Background(), client, tc.raw, "default")
			require.NoError(t, err)
			assert.Equal(t, "unchanged", status)
		})
	}
}

func TestApplyResourceFunctions_GetErrorPropagates(t *testing.T) {
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			boom := errors.New("transient api server failure")
			client.PrependReactor("get", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, boom
			})

			_, err := tc.applyFunc(context.Background(), client, tc.raw, "default")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "transient api server failure")
		})
	}
}

func TestApplyResourceFunctions_PatchErrorPropagates(t *testing.T) {
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			client.PrependReactor("get", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: tc.resource}, "demo")
			})
			client.PrependReactor("patch", tc.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("patch rejected by admission webhook")
			})

			_, err := tc.applyFunc(context.Background(), client, tc.raw, "default")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "patch rejected by admission webhook")
		})
	}
}

func TestApplyResourceFunctions_UnmarshalErrorPropagates(t *testing.T) {
	badRaw := map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment", "metadata": "not-an-object",
	}
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			_, err := tc.applyFunc(context.Background(), client, badRaw, "default")
			require.Error(t, err)
		})
	}
}

func TestApplyResourceFunctions_NamespaceDefaulting(t *testing.T) {
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			var patchedNS string
			client.PrependReactor("patch", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchedNS = action.GetNamespace()
				return true, tc.makeObj("1"), nil
			})

			_, err := tc.applyFunc(context.Background(), client, tc.raw, "kube-system")
			require.NoError(t, err)
			assert.Equal(t, "kube-system", patchedNS)
		})
	}
}

func TestApplyResourceFunctions_MarshalErrorPropagates(t *testing.T) {
	badRaw := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "demo"},
		"__bad":      make(chan int),
	}
	for _, tc := range applyBranchCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			_, err := tc.applyFunc(context.Background(), client, badRaw, "default")
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "unsupported type") || strings.Contains(err.Error(), "chan"))
		})
	}
}

func TestApplyResourceFunctionsUseServerSideApplyPatch(t *testing.T) {
	tests := []struct {
		name      string
		resource  string
		existing  runtime.Object
		raw       map[string]interface{}
		applyFunc func(context.Context, kubernetes.Interface, map[string]interface{}, string) (string, error)
	}{
		{
			name: "deployment", resource: "deployments",
			existing:  &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw:       map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "demo"}},
			applyFunc: ApplyDeployment,
		},
		{
			name: "service", resource: "services",
			existing:  &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw:       map[string]interface{}{"apiVersion": "v1", "kind": "Service", "metadata": map[string]interface{}{"name": "demo"}},
			applyFunc: ApplyService,
		},
		{
			name: "configmap", resource: "configmaps",
			existing:  &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw:       map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "demo"}},
			applyFunc: ApplyConfigMap,
		},
		{
			name: "secret", resource: "secrets",
			existing:  &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", ResourceVersion: "1"}},
			raw:       map[string]interface{}{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]interface{}{"name": "demo"}},
			applyFunc: ApplySecret,
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
