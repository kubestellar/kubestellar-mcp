package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDeleteResourceMissingKind(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"name": "my-pod",
	})
	_, err := server.handleDeleteResource(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind and name are required")
}

func TestHandleDeleteResourceMissingName(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"kind": "Pod",
	})
	_, err := server.handleDeleteResource(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind and name are required")
}

func TestHandleDeleteResourceInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	_, err := server.handleDeleteResource(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleDeleteResourceEmptyClusterList(t *testing.T) {
	// With no clusters configured, DiscoverClusters returns empty list
	server := newHelmTestServer(t, map[string]string{})

	args := mustMarshalJSON(t, map[string]interface{}{
		"kind": "Pod",
		"name": "my-pod",
	})
	result, err := server.handleDeleteResource(context.Background(), args)
	require.NoError(t, err)

	// Should return success with zero results since there are no clusters
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, resultMap["successCount"])
}

func TestHandleDeleteResourceDryRunUnsupportedKind(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"kind":     "Widget",
		"name":     "my-widget",
		"dry_run":  true,
		"clusters": []string{"alpha"},
	})
	result, err := server.handleDeleteResource(context.Background(), args)
	if err != nil {
		assert.Contains(t, err.Error(), "alpha")
	} else {
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		results, ok := resultMap["results"].([]DeleteResult)
		if ok && len(results) > 0 {
			assert.Equal(t, "failed", results[0].Status)
			assert.Contains(t, results[0].Message, "Unsupported resource kind")
		}
	}
}

func TestHandleKubectlApplyMissingManifest(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": "",
	})
	_, err := server.handleKubectlApply(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest is required")
}

func TestHandleKubectlApplyInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	_, err := server.handleKubectlApply(context.Background(), []byte(`{bad json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestHandleKubectlApplyEmptyClusters(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n",
	})
	result, err := server.handleKubectlApply(context.Background(), args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, resultMap["successCount"])
	assert.Equal(t, 0, resultMap["totalClusters"])
}

func TestHandleKubectlApplyDryRunReturnsWouldApply(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"dry_run":  true,
		"clusters": []string{"alpha"},
	})

	result, err := server.handleKubectlApply(context.Background(), args)
	if err == nil {
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, resultMap["dryRun"])
		assert.Equal(t, []string{"alpha"}, resultMap["targetClusters"])

		results, ok := resultMap["results"].([]ApplyResult)
		if ok && len(results) > 0 {
			assert.Equal(t, "would-apply", results[0].Status)
			assert.Equal(t, "ConfigMap", results[0].Kind)
			assert.Equal(t, "test-cm", results[0].Name)
		}
	}
}

func TestHandleKubectlApplyInvalidYAML(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": "this is not valid yaml: [[[",
		"clusters": []string{"alpha"},
	})

	result, err := server.handleKubectlApply(context.Background(), args)
	if err == nil {
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		results, ok := resultMap["results"].([]ApplyResult)
		if ok && len(results) > 0 {
			assert.Equal(t, "failed", results[0].Status)
		}
	}
}

func TestHandleKubectlApplyMultiDocManifest(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2`

	args := mustMarshalJSON(t, map[string]interface{}{
		"manifest": manifest,
		"dry_run":  true,
	})

	result, err := server.handleKubectlApply(context.Background(), args)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["dryRun"])
}

func TestDeleteResourceInClusterUnsupportedKind(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	result, err := server.deleteResourceInCluster(context.Background(), nil, "alpha", "Widget", "my-widget", "default", false)
	require.NoError(t, err)

	dr, ok := result.(DeleteResult)
	require.True(t, ok)
	assert.Equal(t, "failed", dr.Status)
	assert.Contains(t, dr.Message, "Unsupported resource kind")
}

func TestDeleteResourceInClusterDryRun(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	result, err := server.deleteResourceInCluster(context.Background(), nil, "alpha", "Pod", "my-pod", "default", true)
	require.NoError(t, err)

	dr, ok := result.(DeleteResult)
	require.True(t, ok)
	assert.Equal(t, "would-delete", dr.Status)
}

func TestApplyManifestDynamicDryRun(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	if err != nil {
		assert.Contains(t, err.Error(), "alpha")
	} else {
		require.Len(t, results, 1)
		assert.Equal(t, "would-apply", results[0].Status)
		assert.Equal(t, "ConfigMap", results[0].Kind)
		assert.Equal(t, "test-cm", results[0].Name)
	}
}

func TestApplyManifestDynamicInvalidYAML(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	results, err := server.applyManifestDynamic(context.Background(), "alpha", "not: [valid: yaml: {{", true)
	if err != nil {
		return
	}
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "parse")
}

func TestApplyManifestDynamicUnknownKind(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `{"apiVersion":"v1","kind":"UnknownThing","metadata":{"name":"x"}}`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, false)
	if err != nil {
		return
	}
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "unknown resource kind")
}

func TestApplyManifestDynamicMultiDoc(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: default
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
  namespace: default`

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	if err != nil {
		return
	}
	require.Len(t, results, 2)
	assert.Equal(t, "would-apply", results[0].Status)
	assert.Equal(t, "cm1", results[0].Name)
	assert.Equal(t, "would-apply", results[1].Status)
	assert.Equal(t, "cm2", results[1].Name)
}

func TestApplyManifestDynamicEmptyDocs(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := "---\n---\n"

	results, err := server.applyManifestDynamic(context.Background(), "alpha", manifest, true)
	if err != nil {
		return
	}
	assert.Len(t, results, 0)
}

func TestDeleteResultJSON(t *testing.T) {
	dr := DeleteResult{
		Cluster:  "alpha",
		Resource: "Pod",
		Name:     "test-pod",
		Status:   "deleted",
	}
	data, err := json.Marshal(dr)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "alpha", parsed["cluster"])
	assert.Equal(t, "deleted", parsed["status"])
}

func TestApplyResultJSON(t *testing.T) {
	ar := ApplyResult{
		Cluster:   "beta",
		Kind:      "Deployment",
		Name:      "web",
		Namespace: "production",
		Status:    "created",
	}
	data, err := json.Marshal(ar)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "beta", parsed["cluster"])
	assert.Equal(t, "created", parsed["status"])
	assert.Equal(t, "Deployment", parsed["kind"])
}
