package labels

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddLabelsInClusterAllKinds(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()
	client := clientForServer(t, srv)

	cases := []struct {
		kind         string
		wantPathPart string
	}{
		{"deployment", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"deployments", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"service", "/api/v1/namespaces/apps/services/demo"},
		{"svc", "/api/v1/namespaces/apps/services/demo"},
		{"configmap", "/api/v1/namespaces/apps/configmaps/demo"},
		{"cm", "/api/v1/namespaces/apps/configmaps/demo"},
		{"pod", "/api/v1/namespaces/apps/pods/demo"},
		{"pods", "/api/v1/namespaces/apps/pods/demo"},
		{"statefulset", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"sts", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"daemonset", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"ds", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"namespace", "/api/v1/namespaces/demo"},
		{"ns", "/api/v1/namespaces/demo"},
		{"node", "/api/v1/nodes/demo"},
		{"nodes", "/api/v1/nodes/demo"},
		{"persistentvolume", "/api/v1/persistentvolumes/demo"},
		{"pv", "/api/v1/persistentvolumes/demo"},
		{"persistentvolumeclaim", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
		{"pvc", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			recorded = recorded[:0]
			result, err := AddLabelsInCluster(context.Background(), &fakeDeps{}, client, "cA", tc.kind, "demo", "apps", map[string]string{"env": "prod"}, false)
			require.NoError(t, err)
			assert.Equal(t, "labeled", result.Status)
			require.Len(t, recorded, 1)
			assert.Contains(t, recorded[0], tc.wantPathPart)
		})
	}
}

func TestRemoveLabelsInClusterAllKinds(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()
	client := clientForServer(t, srv)

	cases := []struct {
		kind         string
		wantPathPart string
	}{
		{"deployment", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"service", "/api/v1/namespaces/apps/services/demo"},
		{"svc", "/api/v1/namespaces/apps/services/demo"},
		{"configmap", "/api/v1/namespaces/apps/configmaps/demo"},
		{"cm", "/api/v1/namespaces/apps/configmaps/demo"},
		{"pod", "/api/v1/namespaces/apps/pods/demo"},
		{"statefulset", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"sts", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"daemonset", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"ds", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"namespace", "/api/v1/namespaces/demo"},
		{"ns", "/api/v1/namespaces/demo"},
		{"node", "/api/v1/nodes/demo"},
		{"persistentvolume", "/api/v1/persistentvolumes/demo"},
		{"pv", "/api/v1/persistentvolumes/demo"},
		{"persistentvolumeclaim", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
		{"pvc", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			recorded = recorded[:0]
			result, err := RemoveLabelsInCluster(context.Background(), &fakeDeps{}, client, "cA", tc.kind, "demo", "apps", []string{"env"}, false)
			require.NoError(t, err)
			assert.Equal(t, "unlabeled", result.Status)
			require.Len(t, recorded, 1)
			assert.Contains(t, recorded[0], tc.wantPathPart)
		})
	}
}

func TestAddLabelsInClusterDefaultNamespaceFallback(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()
	client := clientForServer(t, srv)

	result, err := AddLabelsInCluster(context.Background(), &fakeDeps{}, client, "cA", "configmap", "demo", "", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "labeled", result.Status)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], "/api/v1/namespaces/default/configmaps/demo")
}

func TestRemoveLabelsInClusterDefaultNamespaceFallback(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()
	client := clientForServer(t, srv)

	result, err := RemoveLabelsInCluster(context.Background(), &fakeDeps{}, client, "cA", "configmap", "demo", "", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "unlabeled", result.Status)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], "/api/v1/namespaces/default/configmaps/demo")
}

func TestAddLabelsInClusterUnsupportedKind(t *testing.T) {
	result, err := AddLabelsInCluster(context.Background(), &fakeDeps{}, nil, "cA", "ingress", "demo", "apps", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Message, "Unsupported resource kind: ingress")
}

func TestRemoveLabelsInClusterUnsupportedKind(t *testing.T) {
	result, err := RemoveLabelsInCluster(context.Background(), &fakeDeps{}, nil, "cA", "ingress", "demo", "apps", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Message, "Unsupported resource kind: ingress")
}
