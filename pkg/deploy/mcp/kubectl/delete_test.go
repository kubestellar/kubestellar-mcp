package kubectl

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDeleteResourceInClusterAllKinds(t *testing.T) {
	const namespace = "test-ns"
	const name = "obj"

	objMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	clusterMeta := metav1.ObjectMeta{Name: name}
	client := kubernetesfake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ConfigMap{ObjectMeta: objMeta},
		&corev1.Secret{ObjectMeta: objMeta},
		&corev1.Pod{ObjectMeta: objMeta},
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&appsv1.DaemonSet{ObjectMeta: objMeta},
		&batchv1.Job{ObjectMeta: objMeta},
		&batchv1.CronJob{ObjectMeta: objMeta},
		&networkingv1.Ingress{ObjectMeta: objMeta},
		&corev1.PersistentVolumeClaim{ObjectMeta: objMeta},
		&corev1.Namespace{ObjectMeta: clusterMeta},
		&corev1.ServiceAccount{ObjectMeta: objMeta},
		&rbacv1.Role{ObjectMeta: objMeta},
		&rbacv1.RoleBinding{ObjectMeta: objMeta},
		&rbacv1.ClusterRole{ObjectMeta: clusterMeta},
		&rbacv1.ClusterRoleBinding{ObjectMeta: clusterMeta},
	)

	cases := []struct {
		kind      string
		namespace string
	}{
		{kind: "deployment", namespace: namespace},
		{kind: "Service", namespace: namespace},
		{kind: "CONFIGMAPS", namespace: namespace},
		{kind: "secret", namespace: namespace},
		{kind: "pod", namespace: namespace},
		{kind: "statefulsets", namespace: namespace},
		{kind: "ds", namespace: namespace},
		{kind: "job", namespace: namespace},
		{kind: "cronjob", namespace: namespace},
		{kind: "ing", namespace: namespace},
		{kind: "pvc", namespace: namespace},
		{kind: "namespaces", namespace: ""},
		{kind: "sa", namespace: namespace},
		{kind: "role", namespace: namespace},
		{kind: "rolebinding", namespace: namespace},
		{kind: "clusterrole", namespace: ""},
		{kind: "clusterrolebindings", namespace: ""},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			result, err := DeleteResourceInCluster(context.Background(), client, "c1", tc.kind, name, tc.namespace, false)
			require.NoError(t, err)
			assert.Equal(t, "deleted", result.Status)
			assert.Equal(t, tc.kind, result.Resource)
			assert.Equal(t, "c1", result.Cluster)
		})
	}
}

func TestDeleteResourceInClusterEmptyNamespaceDefaultsToDefault(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}})

	result, err := DeleteResourceInCluster(context.Background(), client, "c1", "pod", "p", "", false)
	require.NoError(t, err)
	assert.Equal(t, "deleted", result.Status)

	_, getErr := client.CoreV1().Pods("default").Get(context.Background(), "p", metav1.GetOptions{})
	require.Error(t, getErr)
	assert.True(t, apierrors.IsNotFound(getErr))
}

func TestDeleteResourceInClusterErrorMappings(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		result, err := DeleteResourceInCluster(context.Background(), kubernetesfake.NewSimpleClientset(), "c1", "deployment", "missing", "ns", false)
		require.NoError(t, err)
		assert.Equal(t, "not-found", result.Status)
		assert.Contains(t, result.Message, "missing")
	})

	t.Run("explicit not found reactor", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependReactor("delete", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "gone")
		})

		result, err := DeleteResourceInCluster(context.Background(), client, "c1", "cm", "gone", "ns", false)
		require.NoError(t, err)
		assert.Equal(t, "not-found", result.Status)
	})

	t.Run("generic error", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependReactor("delete", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("boom: server exploded")
		})

		result, err := DeleteResourceInCluster(context.Background(), client, "c1", "service", "svc", "ns", false)
		require.NoError(t, err)
		assert.Equal(t, "failed", result.Status)
		assert.Equal(t, "boom: server exploded", result.Message)
	})
}

func TestDeleteResourceInClusterDryRunAndUnsupportedKind(t *testing.T) {
	dryRunResult, err := DeleteResourceInCluster(context.Background(), nil, "c1", "deployment", "demo", "ns", true)
	require.NoError(t, err)
	assert.Equal(t, "would-delete", dryRunResult.Status)
	assert.Contains(t, dryRunResult.Message, "Would delete")

	unsupportedResult, err := DeleteResourceInCluster(context.Background(), nil, "c1", "Widget", "demo", "default", false)
	require.NoError(t, err)
	assert.Equal(t, "failed", unsupportedResult.Status)
	assert.Contains(t, unsupportedResult.Message, "Unsupported resource kind")
}
