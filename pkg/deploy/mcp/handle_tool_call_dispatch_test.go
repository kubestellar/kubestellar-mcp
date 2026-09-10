package mcp

import (
	"context"
	"encoding/json"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

// dispatchMetricFamily gathers a single named metric family from the shared
// metrics registry, useful for asserting label combinations recorded by
// handleToolCall without needing a live /metrics endpoint.
func dispatchMetricFamily(t *testing.T, name string) *dto.MetricFamily {
	t.Helper()
	families, err := metrics.Registry.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func dispatchLabelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

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

// TestHandleToolCallRecordsMetricsForKnownTool verifies that a recognized
// tool name produces both a mcpserver_tool_calls_total series (bounded
// "none" cluster label, since this dispatch point has no per-request
// cluster scoping) and a mcpserver_tool_errors_total series when the
// handler errors, closing the gap where this dispatch path previously
// recorded nothing at all.
func TestHandleToolCallRecordsMetricsForKnownTool(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "helm_install",
			"arguments": map[string]interface{}{},
		}),
	})
	require.NotNil(t, resp)
	payload := resp.Result.(map[string]interface{})
	require.Equal(t, true, payload["isError"], "expected helm_install with no args to fail validation")

	found := false
	for _, m := range dispatchMetricFamily(t, "mcpserver_tool_calls_total").GetMetric() {
		if dispatchLabelValue(m, "tool") == "helm_install" &&
			dispatchLabelValue(m, "cluster") == "none" &&
			dispatchLabelValue(m, "status") == "error" {
			found = true
			assert.GreaterOrEqual(t, m.GetCounter().GetValue(), float64(1))
		}
	}
	assert.True(t, found, "expected mcpserver_tool_calls_total series for helm_install/none/error")
}

// TestHandleToolCallSkipsMetricsForUnknownTool verifies the default arm
// (unrecognized tool name) returns before any metrics are recorded, so the
// "tool" label can never take on an unbounded, client-controlled value.
func TestHandleToolCallSkipsMetricsForUnknownTool(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	unknownTool := "definitely_not_a_registered_tool"
	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      unknownTool,
			"arguments": map[string]interface{}{},
		}),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)

	for _, m := range dispatchMetricFamily(t, "mcpserver_tool_calls_total").GetMetric() {
		assert.NotEqual(t, unknownTool, dispatchLabelValue(m, "tool"),
			"unknown tool name must never be recorded as a metric label value")
	}
}
