package kubectl

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDeleteResource(t *testing.T) {
	t.Run("invalid arguments", func(t *testing.T) {
		_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), []byte(`{invalid`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid arguments")
	})

	t.Run("requires kind and name", func(t *testing.T) {
		cases := []map[string]interface{}{
			{"name": "demo"},
			{"kind": "Pod"},
			{"kind": "", "name": ""},
		}
		for _, tc := range cases {
			_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, tc))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "kind and name are required")
		}
	})

	t.Run("blocks sensitive kinds", func(t *testing.T) {
		_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":     "Secret",
			"name":     "creds",
			"clusters": []string{"alpha"},
			"dry_run":  true,
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Secret")
		assert.Contains(t, err.Error(), "blocked")
	})

	t.Run("validates namespace field", func(t *testing.T) {
		_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":      "ConfigMap",
			"name":      "demo",
			"namespace": "kube-system",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid namespace")
	})

	t.Run("validates namespace kind by name", func(t *testing.T) {
		_, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":      "Namespace",
			"name":      "kube-system",
			"namespace": "default",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid namespace")
		assert.Contains(t, err.Error(), "kube-system")
	})

	t.Run("discover clusters when unspecified", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusters = []string{"bravo", "alpha"}

		result, err := HandleDeleteResource(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":    "Pod",
			"name":    "demo",
			"dry_run": true,
		}))
		require.NoError(t, err)
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, []string{"alpha", "bravo"}, resultMap["targetClusters"])
		assert.Equal(t, 2, resultMap["successCount"])
		results, ok := resultMap["results"].([]DeleteResult)
		require.True(t, ok)
		assert.Equal(t, 2, countDeleteStatuses(results, "would-delete"))
	})

	t.Run("empty discovery yields empty result", func(t *testing.T) {
		result, err := HandleDeleteResource(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind": "Pod",
			"name": "demo",
		}))
		require.NoError(t, err)
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, 0, resultMap["successCount"])
		assert.Equal(t, 0, resultMap["totalClusters"])
	})

	t.Run("propagates discovery error", func(t *testing.T) {
		deps := newFakeDeps()
		deps.discoverErr = errors.New("discover boom")
		_, err := HandleDeleteResource(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind": "Pod",
			"name": "demo",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "discover boom")
	})

	t.Run("propagates top level executor error", func(t *testing.T) {
		deps := newFakeDeps()
		deps.executeErr = errors.New("execute boom")
		_, err := HandleDeleteResource(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":     "Pod",
			"name":     "demo",
			"clusters": []string{"alpha"},
			"dry_run":  true,
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute boom")
	})

	t.Run("aggregates failed clusters", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusterErrors["ghost"] = errors.New("ghost boom")

		result, err := HandleDeleteResource(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":     "ConfigMap",
			"name":     "demo",
			"clusters": []string{"ghost"},
			"dry_run":  true,
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		results := resultMap["results"].([]DeleteResult)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Equal(t, "ghost", results[0].Cluster)
		assert.Equal(t, 0, resultMap["successCount"])
	})

	t.Run("aggregates mixed results", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusterErrors["ghost"] = errors.New("ghost boom")

		result, err := HandleDeleteResource(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"kind":     "ConfigMap",
			"name":     "demo",
			"clusters": []string{"alpha", "ghost"},
			"dry_run":  true,
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		results := resultMap["results"].([]DeleteResult)
		assert.Equal(t, 1, countDeleteStatuses(results, "would-delete"))
		assert.Equal(t, 1, countDeleteStatuses(results, "failed"))
		assert.Equal(t, 1, resultMap["successCount"])
	})
}

func TestHandleKubectlApply(t *testing.T) {
	t.Run("invalid arguments", func(t *testing.T) {
		_, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), []byte(`{bad json`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid arguments")
	})

	t.Run("requires manifest", func(t *testing.T) {
		_, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{"manifest": ""}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "manifest is required")
	})

	t.Run("blocks sensitive kind", func(t *testing.T) {
		_, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: creds\n",
			"clusters": []string{"alpha"},
			"dry_run":  true,
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Secret")
		assert.Contains(t, err.Error(), "blocked")
	})

	t.Run("blocks sensitive kind in multi doc", func(t *testing.T) {
		manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: benign\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: creds\n"
		_, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": manifest,
			"clusters": []string{"alpha"},
			"dry_run":  true,
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Secret")
	})

	t.Run("discover clusters when unspecified", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusters = []string{"bravo", "alpha"}

		result, err := HandleKubectlApply(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
			"dry_run":  true,
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		assert.Equal(t, []string{"alpha", "bravo"}, resultMap["targetClusters"])
		assert.Equal(t, 2, resultMap["successCount"])
		results := resultMap["results"].([]ApplyResult)
		assert.Equal(t, 2, countApplyStatuses(results, "would-apply"))
	})

	t.Run("empty discovery yields empty result", func(t *testing.T) {
		result, err := HandleKubectlApply(context.Background(), newFakeDeps().deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		assert.Equal(t, 0, resultMap["successCount"])
		assert.Equal(t, 0, resultMap["totalClusters"])
	})

	t.Run("propagates discovery error", func(t *testing.T) {
		deps := newFakeDeps()
		deps.discoverErr = errors.New("discover boom")
		_, err := HandleKubectlApply(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "discover boom")
	})

	t.Run("propagates top level executor error", func(t *testing.T) {
		deps := newFakeDeps()
		deps.executeErr = errors.New("execute boom")
		_, err := HandleKubectlApply(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
			"clusters": []string{"alpha"},
			"dry_run":  true,
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute boom")
	})

	t.Run("aggregates failed clusters", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusterErrors["ghost"] = errors.New("ghost boom")

		result, err := HandleKubectlApply(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
			"clusters": []string{"ghost"},
			"dry_run":  true,
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		results := resultMap["results"].([]ApplyResult)
		require.Len(t, results, 1)
		assert.Equal(t, "failed", results[0].Status)
		assert.Equal(t, "ghost", results[0].Cluster)
		assert.Equal(t, 0, resultMap["successCount"])
	})

	t.Run("aggregates mixed results", func(t *testing.T) {
		deps := newFakeDeps()
		deps.clusterErrors["ghost"] = errors.New("ghost boom")

		result, err := HandleKubectlApply(context.Background(), deps.deps(), mustMarshalJSON(t, map[string]interface{}{
			"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
			"clusters": []string{"alpha", "ghost"},
			"dry_run":  true,
		}))
		require.NoError(t, err)
		resultMap := result.(map[string]interface{})
		results := resultMap["results"].([]ApplyResult)
		assert.Equal(t, 1, countApplyStatuses(results, "would-apply"))
		assert.Equal(t, 1, countApplyStatuses(results, "failed"))
		assert.Equal(t, 1, resultMap["successCount"])
	})
}
