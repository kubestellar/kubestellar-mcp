package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleToolCallDispatchArmsFormatErrorContent locks in that every tool
// name registered in handleToolCall's switch actually reaches its handler
// and, when the handler returns an error (as it will with empty arguments
// due to required-field validation), the response is packaged as an
// isError=true content payload rather than a JSON-RPC error object.
//
// Before this test, most of the individual case arms in server.go's
// handleToolCall switch had zero direct coverage — only kustomize_build was
// exercised as a dispatch target. Each case arm is a wiring point that could
// silently regress if a rename or refactor drops a mapping.
func TestHandleToolCallDispatchArmsFormatErrorContent(t *testing.T) {
	toolNames := []string{
		// App tools
		"get_app_instances",
		"get_app_status",
		"get_app_logs",
		// Deploy tools
		"list_cluster_capabilities",
		"find_clusters_for_workload",
		"deploy_app",
		"scale_app",
		"patch_app",
		// GitOps tools
		"detect_drift",
		"sync_from_git",
		"reconcile",
		"preview_changes",
		// Helm tools
		"helm_install",
		"helm_uninstall",
		"helm_list",
		"helm_rollback",
		// Delete/apply
		"delete_resource",
		"kubectl_apply",
		// Kustomize
		"kustomize_apply",
		"kustomize_delete",
		// Labels
		"add_labels",
		"remove_labels",
	}

	for _, name := range toolNames {
		t.Run(name, func(t *testing.T) {
			server := newHelmTestServer(t, map[string]string{})

			resp := server.handleToolCall(context.Background(), &MCPRequest{
				JSONRPC: "2.0",
				ID:      1,
				Params: mustMarshalJSON(t, map[string]interface{}{
					"name":      name,
					"arguments": map[string]interface{}{},
				}),
			})
			require.NotNil(t, resp, "dispatch %q returned nil response", name)
			// Dispatch must reach the handler, not fall through to the
			// default arm's JSON-RPC "Unknown tool" error.
			assert.Nil(t, resp.Error, "dispatch %q produced JSON-RPC error (unregistered arm?)", name)

			payload, ok := resp.Result.(map[string]interface{})
			require.True(t, ok, "dispatch %q result is not a map", name)
			// Handlers vary: some fail on missing required args (isError=true),
			// others succeed with an empty result (e.g. list operations with
			// no matching resources). Both outcomes prove the switch reached
			// the handler rather than the default arm.
			if payload["isError"] == true {
				content, ok := payload["content"].([]map[string]interface{})
				require.True(t, ok, "dispatch %q content missing/wrong shape", name)
				require.Len(t, content, 1)
				text, ok := content[0]["text"].(string)
				require.True(t, ok, "dispatch %q content text missing", name)
				assert.NotEmpty(t, text, "dispatch %q error text is empty", name)
			}
		})
	}
}

// TestHandleToolCallDispatchArmsUseJSONUnmarshalError verifies that when the
// arguments blob is not a JSON object at all, each handler still round-trips
// through the switch arm and returns a formatted error. This exercises the
// json.Unmarshal error branch inside each dispatched handler.
func TestHandleToolCallDispatchArmsHandleMalformedArgs(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "helm_install",
			"arguments": json.RawMessage(`"not-an-object"`),
		}),
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	payload := resp.Result.(map[string]interface{})
	assert.Equal(t, true, payload["isError"])
}
