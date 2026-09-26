package kubectl

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteResultJSON(t *testing.T) {
	result := DeleteResult{Cluster: "alpha", Resource: "Pod", Name: "test-pod", Status: "deleted"}
	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "alpha", parsed["cluster"])
	assert.Equal(t, "deleted", parsed["status"])
}

func TestApplyResultJSON(t *testing.T) {
	result := ApplyResult{Cluster: "beta", Kind: "Deployment", Name: "web", Namespace: "production", Status: "created"}
	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "beta", parsed["cluster"])
	assert.Equal(t, "created", parsed["status"])
	assert.Equal(t, "Deployment", parsed["kind"])
}
