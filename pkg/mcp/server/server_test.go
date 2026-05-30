package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func TestNewServerInitializesDefaults(t *testing.T) {
	s := NewServer("kubeconfig-for-tests")

	require.Equal(t, "kubeconfig-for-tests", s.kubeconfig)
	require.NotNil(t, s.discoverer)
	require.NotNil(t, s.reader)
	require.NotNil(t, s.writer)
	assert.Nil(t, s.clientFactory)
}

func TestHandleInitializeReturnsServerMetadata(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	s.handleInitialize(&Request{ID: "init-1"})

	responses := decodeResponses(t, buf.String())
	require.Len(t, responses, 1)
	assert.Nil(t, responses[0].Error)
	assert.Equal(t, "2.0", responses[0].JSONRPC)
	assert.Equal(t, "init-1", responses[0].ID)

	var result InitializeResult
	require.NoError(t, json.Unmarshal(responses[0].Result, &result))
	assert.Equal(t, MCPVersion, result.ProtocolVersion)
	require.NotNil(t, result.Capabilities.Tools)
	assert.Equal(t, ServerName, result.ServerInfo.Name)
	assert.Equal(t, ServerVersion, result.ServerInfo.Version)
}

func TestHandleToolsListIncludesDiagnosticsAndUpgradeTools(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	s.handleToolsList(&Request{ID: "tools-1"})

	responses := decodeResponses(t, buf.String())
	require.Len(t, responses, 1)
	require.Nil(t, responses[0].Error)

	var result ToolsListResult
	require.NoError(t, json.Unmarshal(responses[0].Result, &result))
	require.NotEmpty(t, result.Tools)

	toolNames := make(map[string]Tool, len(result.Tools))
	for _, tool := range result.Tools {
		toolNames[tool.Name] = tool
	}

	for _, name := range []string{
		"list_clusters",
		"find_pod_issues",
		"analyze_namespace",
		"detect_cluster_type",
		"get_cluster_version_info",
		"get_upgrade_prerequisites",
		"trigger_openshift_upgrade",
		"get_upgrade_status",
	} {
		assert.Contains(t, toolNames, name)
	}
	assert.Equal(t, []string{"namespace"}, toolNames["analyze_namespace"].InputSchema.Required)
}

func TestRunHandlesParseErrorsAndRequests(t *testing.T) {
	input := strings.Join([]string{
		`{not-json}`,
		`{"jsonrpc":"2.0","id":"ping-1","method":"ping"}`,
		`{"jsonrpc":"2.0","id":"missing-1","method":"missing"}`,
	}, "\n") + "\n"

	var output bytes.Buffer
	s := &Server{
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &output,
	}

	require.NoError(t, s.Run(context.Background()))

	responses := decodeResponses(t, output.String())
	require.Len(t, responses, 3)

	require.NotNil(t, responses[0].Error)
	assert.Equal(t, -32700, responses[0].Error.Code)
	assert.Equal(t, "Parse error", responses[0].Error.Message)

	assert.Nil(t, responses[1].Error)
	assert.Equal(t, "ping-1", responses[1].ID)
	assert.JSONEq(t, `{}`, string(responses[1].Result))

	require.NotNil(t, responses[2].Error)
	assert.Equal(t, -32601, responses[2].Error.Code)
	assert.Contains(t, responses[2].Error.Message, "Method not found")
	assert.Equal(t, "missing-1", responses[2].ID)
}

func decodeResponses(t *testing.T, output string) []rpcEnvelope {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]rpcEnvelope, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var resp rpcEnvelope
		require.NoError(t, json.Unmarshal([]byte(line), &resp))
		responses = append(responses, resp)
	}
	return responses
}
