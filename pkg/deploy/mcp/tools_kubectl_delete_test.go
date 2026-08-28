package mcp

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

// TestDeleteResourceInClusterAllKinds exercises the delete-success path for
// every supported kind + alias in deleteResourceInCluster. Uses a fake clientset
// pre-seeded with each object; a successful call must return status=deleted.
func TestDeleteResourceInClusterAllKinds(t *testing.T) {
	const ns = "test-ns"
	const name = "obj"

	objMeta := metav1.ObjectMeta{Name: name, Namespace: ns}
	clusterMeta := metav1.ObjectMeta{Name: name}

	// Distinct objects per kind so the fake tracker doesn't confuse them.
	seed := []runtime.Object{
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
	}
	client := kubernetesfake.NewSimpleClientset(seed...)
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	// One (kind-alias) form per case. Namespace-scoped kinds pass ns; cluster-scoped pass empty.
	cases := []struct {
		alias     string
		namespace string
	}{
		{"deployment", ns},
		{"Service", ns},           // case-insensitive
		{"CONFIGMAPS", ns},        // upper-case alias
		{"secret", ns},
		{"pod", ns},
		{"statefulsets", ns},
		{"ds", ns},                // daemonset alias
		{"job", ns},
		{"cronjob", ns},
		{"ing", ns},               // ingress alias
		{"pvc", ns},
		{"namespaces", ""},        // cluster-scoped
		{"sa", ns},                // serviceaccount alias
		{"role", ns},
		{"rolebinding", ns},
		{"clusterrole", ""},       // cluster-scoped
		{"clusterrolebindings", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.alias, func(t *testing.T) {
			res, err := server.deleteResourceInCluster(context.Background(), client, "c1", tc.alias, name, tc.namespace, false)
			require.NoError(t, err)
			assert.Equal(t, "deleted", res.Status, "alias=%s message=%s", tc.alias, res.Message)
			assert.Equal(t, "c1", res.Cluster)
			assert.Equal(t, name, res.Name)
			assert.Equal(t, tc.alias, res.Resource)
		})
	}
}

// TestDeleteResourceInClusterEmptyNamespaceFallsBackToDefault verifies the
// empty-namespace fallback: when namespace is "" for a namespace-scoped kind,
// the function targets "default".
func TestDeleteResourceInClusterEmptyNamespaceFallsBackToDefault(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}},
	)
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	res, err := server.deleteResourceInCluster(context.Background(), client, "c1", "pod", "p", "", false)
	require.NoError(t, err)
	assert.Equal(t, "deleted", res.Status)

	// The pod should be gone from the default namespace.
	_, getErr := client.CoreV1().Pods("default").Get(context.Background(), "p", metav1.GetOptions{})
	require.Error(t, getErr)
	assert.True(t, apierrors.IsNotFound(getErr))
}

// TestDeleteResourceInClusterNotFoundMapping verifies that an apierrors.NewNotFound
// from the k8s client is mapped to status="not-found".
func TestDeleteResourceInClusterNotFoundMapping(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset() // empty tracker → delete returns NotFound
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	res, err := server.deleteResourceInCluster(context.Background(), client, "c1", "deployment", "missing", "ns", false)
	require.NoError(t, err)
	assert.Equal(t, "not-found", res.Status)
	assert.Contains(t, res.Message, "missing")
}

// TestDeleteResourceInClusterGenericErrorMapping verifies that a non-NotFound
// error from the k8s client is mapped to status="failed" with the error message.
func TestDeleteResourceInClusterGenericErrorMapping(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	client.PrependReactor("delete", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom: server exploded")
	})
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	res, err := server.deleteResourceInCluster(context.Background(), client, "c1", "service", "svc", "ns", false)
	require.NoError(t, err)
	assert.Equal(t, "failed", res.Status)
	assert.Equal(t, "boom: server exploded", res.Message)
}

// TestDeleteResourceInClusterNotFoundReactor asserts the not-found branch is
// selected when the reactor returns apierrors.NewNotFound explicitly (belt-and-
// suspenders vs. the empty-tracker case above; documents the intended mapping).
func TestDeleteResourceInClusterNotFoundReactor(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	client.PrependReactor("delete", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "gone")
	})
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	res, err := server.deleteResourceInCluster(context.Background(), client, "c1", "cm", "gone", "ns", false)
	require.NoError(t, err)
	assert.Equal(t, "not-found", res.Status)
}

// TestDeleteResourceInClusterDryRunSkipsClient verifies the dry-run short-circuit
// runs before any client call. Passing a nil client and a nil-safe kind that would
// otherwise crash proves the client is never touched.
func TestDeleteResourceInClusterDryRunSkipsClient(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"c1": "https://c1.example.com"})

	res, err := server.deleteResourceInCluster(context.Background(), nil, "c1", "deployment", "d", "ns", true)
	require.NoError(t, err)
	assert.Equal(t, "would-delete", res.Status)
	assert.Contains(t, res.Message, "Would delete")
}
