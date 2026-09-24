package labels

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLabelPatch(t *testing.T) {
	addPatch := decodeLabelPatch(t, BuildLabelPatch(map[string]string{"env": "prod"}, false))
	labels := addPatch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	assert.Equal(t, "prod", labels["env"])

	removePatch := decodeLabelPatch(t, BuildLabelPatch(map[string]string{"env": "ignored"}, true))
	removeLabels := removePatch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	value, exists := removeLabels["env"]
	require.True(t, exists)
	assert.Nil(t, value)
}

func TestBuildLabelPatchRemoveEncodesNullValues(t *testing.T) {
	patch := BuildLabelPatch(map[string]string{"env": "", "tier": ""}, true)
	decoded := decodeLabelPatch(t, patch)
	labels := decoded["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	require.Len(t, labels, 2)
	for key, value := range labels {
		assert.Nil(t, value, key)
	}
}

func TestLabelOperationsDryRunAndUnsupportedKinds(t *testing.T) {
	deps := &fakeDeps{}

	addResult, err := AddLabelsInCluster(context.Background(), deps, nil, "cluster-a", "deployment", "demo", "apps", map[string]string{"env": "prod"}, true)
	require.NoError(t, err)
	assert.Equal(t, "would-label", addResult.Status)
	assert.Contains(t, addResult.Message, "Would add labels")

	removeResult, err := RemoveLabelsInCluster(context.Background(), deps, nil, "cluster-a", "deployment", "demo", "apps", []string{"env"}, true)
	require.NoError(t, err)
	assert.Equal(t, "would-unlabel", removeResult.Status)
	assert.Contains(t, removeResult.Message, "Would remove labels")

	unsupportedAdd, err := AddLabelsInCluster(context.Background(), deps, nil, "cluster-a", "widget", "demo", "apps", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", unsupportedAdd.Status)
	assert.Contains(t, unsupportedAdd.Message, "Unsupported resource kind")

	unsupportedRemove, err := RemoveLabelsInCluster(context.Background(), deps, nil, "cluster-a", "widget", "demo", "apps", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", unsupportedRemove.Status)
	assert.Contains(t, unsupportedRemove.Message, "Unsupported resource kind")
}

func TestHandleLabelValidation(t *testing.T) {
	deps := &fakeDeps{clusterNames: []string{"alpha"}, sensitive: map[string]bool{"secret": true, "secrets": true, "serviceaccount": true}}
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{name: "add invalid json", call: func() error { _, err := HandleAddLabels(context.Background(), deps, json.RawMessage("{")); return err }, want: "invalid arguments"},
		{name: "add missing kind", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"name": "demo", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "kind and name are required"},
		{name: "add missing labels", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo"}))
			return err
		}, want: "labels are required"},
		{name: "remove invalid json", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, json.RawMessage("{"))
			return err
		}, want: "invalid arguments"},
		{name: "remove missing kind", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"name": "demo", "labels": []string{"env"}}))
			return err
		}, want: "kind and name are required"},
		{name: "remove missing labels", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo"}))
			return err
		}, want: "labels are required"},
		{name: "add blocked system namespace", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo", "namespace": "kube-system", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "invalid namespace"},
		{name: "add invalid namespace format", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo", "namespace": "Invalid_NS", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "invalid namespace"},
		{name: "add openshift-prefixed namespace", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo", "namespace": "openshift-monitoring", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "invalid namespace"},
		{name: "remove blocked system namespace", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo", "namespace": "kube-public", "labels": []string{"env"}}))
			return err
		}, want: "invalid namespace"},
		{name: "remove invalid namespace format", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "pod", "name": "demo", "namespace": "Invalid_NS", "labels": []string{"env"}}))
			return err
		}, want: "invalid namespace"},
		{name: "add blocks Secret", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "Secret", "name": "creds", "namespace": "default", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "blocked"},
		{name: "add blocks secrets plural", call: func() error {
			_, err := HandleAddLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "secrets", "name": "creds", "labels": map[string]string{"env": "prod"}}))
			return err
		}, want: "blocked"},
		{name: "remove blocks Secret", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "Secret", "name": "creds", "namespace": "default", "labels": []string{"env"}}))
			return err
		}, want: "blocked"},
		{name: "remove blocks ServiceAccount", call: func() error {
			_, err := HandleRemoveLabels(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "ServiceAccount", "name": "default", "namespace": "default", "labels": []string{"env"}}))
			return err
		}, want: "blocked"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLabelOperationsBlockSensitiveKinds(t *testing.T) {
	deps := &fakeDeps{sensitive: map[string]bool{"secret": true, "secrets": true}}

	addResult, err := AddLabelsInCluster(context.Background(), deps, nil, "cluster-a", "Secret", "creds", "default", map[string]string{"env": "prod"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", addResult.Status)
	assert.Contains(t, addResult.Message, "blocked")

	dryAdd, err := AddLabelsInCluster(context.Background(), deps, nil, "cluster-a", "secrets", "creds", "default", map[string]string{"env": "prod"}, true)
	require.NoError(t, err)
	assert.Equal(t, "failed", dryAdd.Status)
	assert.Contains(t, dryAdd.Message, "blocked")

	removeResult, err := RemoveLabelsInCluster(context.Background(), deps, nil, "cluster-a", "Secret", "creds", "default", []string{"env"}, false)
	require.NoError(t, err)
	assert.Equal(t, "failed", removeResult.Status)
	assert.Contains(t, removeResult.Message, "blocked")
}

func TestTools(t *testing.T) {
	defs := Tools()
	require.Len(t, defs, 2)
	assert.Equal(t, []string{"add_labels", "remove_labels"}, []string{defs[0].Name, defs[1].Name})

	deps := &fakeDeps{clusterNames: []string{"alpha"}}
	addRes, err := defs[0].Handler(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": map[string]string{"env": "prod"}, "dry_run": true}))
	require.NoError(t, err)
	assert.Equal(t, 1, int(decodeLabelsResp(t, addRes)["successCount"].(float64)))

	removeRes, err := defs[1].Handler(context.Background(), deps, mustMarshalJSON(t, map[string]interface{}{"kind": "deployment", "name": "demo", "labels": []string{"env"}, "dry_run": true}))
	require.NoError(t, err)
	assert.Equal(t, 1, int(decodeLabelsResp(t, removeRes)["successCount"].(float64)))
}
