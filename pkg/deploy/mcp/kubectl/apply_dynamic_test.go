package kubectl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestApplyManifestDynamicTopLevelErrors(t *testing.T) {
	t.Run("get config", func(t *testing.T) {
		deps := newFakeDeps()
		deps.configErrs["ghost"] = errors.New("no kubeconfig entry")

		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "ghost", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", false)
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "failed to get config for cluster ghost")
	})

	t.Run("dynamic client creation", func(t *testing.T) {
		deps := newFakeDeps()
		deps.configs["alpha"] = &rest.Config{Host: "://bad-host"}

		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", false)
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "failed to create dynamic client")
	})
}

func TestApplyManifestDynamicDryRunAndParsingBranches(t *testing.T) {
	deps := newFakeDeps()

	results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n", true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Equal(t, "ConfigMap", results[0].Kind)
	assert.Equal(t, "demo", results[0].Name)

	results, err = ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "not: [valid: yaml: {{", true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "failed to parse manifest")

	results, err = ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", `{"apiVersion":"v1","kind":"UnknownThing","metadata":{"name":"x"}}`, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "unknown resource kind")
}

func TestApplyManifestDynamicNamespaceValidationAndMultidoc(t *testing.T) {
	deps := newFakeDeps()

	t.Run("namespace kind validates metadata name", func(t *testing.T) {
		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: kube-system\n", true)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Contains(t, results[0].Message, "invalid namespace in manifest")
		assert.Contains(t, results[0].Message, "kube-system")
	})

	t.Run("invalid label namespace", func(t *testing.T) {
		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n  namespace: BAD_NS\n", true)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Contains(t, results[0].Message, "invalid namespace in manifest")
	})

	t.Run("empty namespace defaults to default", func(t *testing.T) {
		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-no-ns\n", true)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "would-apply", results[0].Status)
		assert.Equal(t, "default", results[0].Namespace)
	})

	t.Run("mid stream failure preserves surrounding docs", func(t *testing.T) {
		manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n  namespace: default\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm2\n  namespace: kube-system\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm3\n  namespace: default\n"
		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", manifest, true)
		require.NoError(t, err)
		require.Len(t, results, 3)
		assert.Equal(t, "would-apply", results[0].Status)
		assert.Equal(t, "failed", results[1].Status)
		assert.Contains(t, results[1].Message, "kube-system")
		assert.Equal(t, "would-apply", results[2].Status)
	})

	t.Run("namespace document in middle preserves surrounding docs", func(t *testing.T) {
		manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-first\n---\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: openshift-monitoring\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-last\n"
		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", manifest, true)
		require.NoError(t, err)
		require.Len(t, results, 3)
		assert.Equal(t, "would-apply", results[0].Status)
		assert.Equal(t, "failed", results[1].Status)
		assert.Contains(t, strings.ToLower(results[1].Message), "openshift-monitoring")
		assert.Equal(t, "would-apply", results[2].Status)
	})
}

func TestApplyManifestDynamicNonDryRunCreateUpdateAndFailures(t *testing.T) {
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\ndata:\n  key: v1\n"

	t.Run("create then update", func(t *testing.T) {
		state := &fakeK8sState{}
		server := startFakeConfigMapAPI(t, state, fakeAPIMode{})
		defer server.Close()

		deps := newFakeDeps()
		deps.configs["alpha"] = configForServer(server)

		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", manifest, false)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "created", results[0].Status)

		results, err = ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", strings.Replace(manifest, "v1", "v2", 1), false)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "updated", results[0].Status)

		state.mu.Lock()
		stored := cloneConfigMap(state.stored["demo"])
		state.mu.Unlock()
		meta, _ := stored["metadata"].(map[string]interface{})
		assert.Equal(t, "2", meta["resourceVersion"])
	})

	t.Run("create failure maps to failed status", func(t *testing.T) {
		server := startFakeConfigMapAPI(t, &fakeK8sState{}, fakeAPIMode{failCreate: true})
		defer server.Close()

		deps := newFakeDeps()
		deps.configs["alpha"] = configForServer(server)

		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", manifest, false)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Contains(t, results[0].Message, "create boom")
	})

	t.Run("update failure maps to failed status", func(t *testing.T) {
		state := &fakeK8sState{}
		server := startFakeConfigMapAPI(t, state, fakeAPIMode{})
		defer server.Close()

		deps := newFakeDeps()
		deps.configs["alpha"] = configForServer(server)

		_, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", manifest, false)
		require.NoError(t, err)

		server.Close()
		server = startFakeConfigMapAPI(t, state, fakeAPIMode{failUpdate: true})
		defer server.Close()
		deps.configs["alpha"] = configForServer(server)

		results, err := ApplyManifestDynamic(context.Background(), deps.deps(), "alpha", strings.Replace(manifest, "v1", "v2", 1), false)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Contains(t, results[0].Message, "update boom")
	})
}
