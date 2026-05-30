package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRequestLifecycleAndUnknownMethod(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	initResp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	require.NotNil(t, initResp)
	assert.Nil(t, initResp.Error)
	result := initResp.Result.(map[string]interface{})
	serverInfo := result["serverInfo"].(map[string]string)
	assert.Equal(t, ServerName, serverInfo["name"])
	assert.Equal(t, ServerVersion, serverInfo["version"])

	assert.Nil(t, server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 2, Method: "notifications/initialized"}))

	unknownResp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 3, Method: "unknown"})
	require.NotNil(t, unknownResp)
	require.NotNil(t, unknownResp.Error)
	assert.Equal(t, -32601, unknownResp.Error.Code)
}

func TestHandleListToolsIncludesDeploymentGitOpsAndKustomizeTools(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	resp := server.handleListTools(&MCPRequest{JSONRPC: "2.0", ID: 1})
	require.NotNil(t, resp)

	payload := resp.Result.(map[string]interface{})
	tools := payload["tools"].([]map[string]interface{})
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool["name"].(string)] = true
	}

	for _, name := range []string{"deploy_app", "sync_from_git", "kustomize_apply"} {
		assert.Truef(t, names[name], "expected tool %q to be registered", name)
	}
}

func TestHandleToolCallReturnsErrorResponsesForInvalidParamsAndUnknownTool(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	invalid := server.handleToolCall(context.Background(), &MCPRequest{JSONRPC: "2.0", ID: 1, Params: []byte(`{invalid`)})
	require.NotNil(t, invalid.Error)
	assert.Equal(t, -32602, invalid.Error.Code)

	unknown := server.handleToolCall(context.Background(), &MCPRequest{JSONRPC: "2.0", ID: 2, Params: mustMarshalJSON(t, map[string]interface{}{"name": "missing_tool", "arguments": map[string]interface{}{}})})
	require.NotNil(t, unknown.Error)
	assert.Equal(t, -32601, unknown.Error.Code)
	assert.Contains(t, unknown.Error.Message, "Unknown tool")
}

func TestHandleToolCallFormatsHandlerErrorsAsContent(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{JSONRPC: "2.0", ID: 1, Params: mustMarshalJSON(t, map[string]interface{}{
		"name":      "kustomize_build",
		"arguments": map[string]interface{}{},
	})})
	require.Nil(t, resp.Error)

	payload := resp.Result.(map[string]interface{})
	assert.Equal(t, true, payload["isError"])
	content := payload["content"].([]map[string]interface{})
	require.Len(t, content, 1)
	assert.Contains(t, content[0]["text"].(string), "path is required")
}

func TestHandleToolCallDispatchesKustomizeBuild(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")

	resp := server.handleToolCall(context.Background(), &MCPRequest{JSONRPC: "2.0", ID: 1, Params: mustMarshalJSON(t, map[string]interface{}{
		"name":      "kustomize_build",
		"arguments": map[string]interface{}{"path": dir},
	})})
	require.Nil(t, resp.Error)

	payload := resp.Result.(map[string]interface{})
	content := payload["content"].([]map[string]interface{})
	require.Len(t, content, 1)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(content[0]["text"].(string)), &decoded))
	assert.Equal(t, dir, decoded["path"])
	assert.Equal(t, float64(1), decoded["resources"])
}
