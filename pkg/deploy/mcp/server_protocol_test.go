package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunProcessesMultipleRequestsAndHandlesEOF verifies the stdin/stdout
// message loop in Run(). It injects multiple newline-delimited JSON-RPC
// requests via stdin and confirms corresponding responses on stdout, then
// verifies graceful termination on EOF.
func TestRunProcessesMultipleRequestsAndHandlesEOF(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	requests := []MCPRequest{
		{JSONRPC: "2.0", ID: 1, Method: "initialize"},
		{JSONRPC: "2.0", ID: 2, Method: "tools/list"},
		{JSONRPC: "2.0", ID: 3, Method: "unknown_method"},
	}

	var input bytes.Buffer
	for _, req := range requests {
		data, err := json.Marshal(req)
		require.NoError(t, err)
		input.Write(data)
		input.WriteByte('\n')
	}

	// Redirect stdin/stdout for the Run() call
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdin = stdinR
	os.Stdout = stdoutW

	// Write input and close to signal EOF
	_, err = stdinW.Write(input.Bytes())
	require.NoError(t, err)
	_ = stdinW.Close()

	// Run the server (blocks until EOF)
	runErr := server.Run()
	_ = stdoutW.Close()

	assert.NoError(t, runErr)

	// Read all output
	output, err := io.ReadAll(stdoutR)
	require.NoError(t, err)

	// Parse responses - one per line
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.Len(t, lines, 3, "expected 3 responses for 3 requests")

	// Verify initialize response
	var resp1 MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp1))
	assert.Equal(t, "2.0", resp1.JSONRPC)
	assert.Nil(t, resp1.Error)

	// Verify tools/list response
	var resp2 MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &resp2))
	assert.Nil(t, resp2.Error)

	// Verify unknown method returns error
	var resp3 MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &resp3))
	require.NotNil(t, resp3.Error)
	assert.Equal(t, -32601, resp3.Error.Code)
}

// TestRunSkipsEmptyLines verifies that blank lines in the input stream
// are silently ignored and don't produce responses.
func TestRunSkipsEmptyLines(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// Input with empty lines interspersed
	input := "\n\n" + marshalRequest(t, MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"}) + "\n\n\n"

	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdin = stdinR
	os.Stdout = stdoutW

	_, err = stdinW.WriteString(input)
	require.NoError(t, err)
	_ = stdinW.Close()

	runErr := server.Run()
	_ = stdoutW.Close()

	assert.NoError(t, runErr)

	output, err := io.ReadAll(stdoutR)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	assert.Len(t, lines, 1, "only one valid request should produce one response")
}

// TestRunHandlesMalformedJSON verifies that invalid JSON on stdin produces
// a parse error response (-32700) and continues processing subsequent messages.
func TestRunHandlesMalformedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	input := "{not valid json}\n" + marshalRequest(t, MCPRequest{JSONRPC: "2.0", ID: 99, Method: "initialize"}) + "\n"

	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdin = stdinR
	os.Stdout = stdoutW

	_, err = stdinW.WriteString(input)
	require.NoError(t, err)
	_ = stdinW.Close()

	runErr := server.Run()
	_ = stdoutW.Close()

	assert.NoError(t, runErr)

	output, err := io.ReadAll(stdoutR)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.Len(t, lines, 2, "malformed JSON + valid request = 2 responses")

	// First response should be a parse error
	var parseErr MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &parseErr))
	require.NotNil(t, parseErr.Error)
	assert.Equal(t, -32700, parseErr.Error.Code)
	assert.Contains(t, parseErr.Error.Message, "Parse error")

	// Second response should be a valid initialize response
	var initResp MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &initResp))
	assert.Nil(t, initResp.Error)
}

// TestSendResponseWritesNewlineDelimitedJSON verifies that sendResponse
// outputs valid JSON followed by a newline character.
func TestSendResponseWritesNewlineDelimitedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	resp := &MCPResponse{
		JSONRPC: "2.0",
		ID:      42,
		Result:  map[string]interface{}{"hello": "world"},
	}
	server.sendResponse(resp)
	_ = w.Close()

	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Must end with exactly one newline
	assert.True(t, strings.HasSuffix(string(output), "\n"))
	trimmed := strings.TrimSuffix(string(output), "\n")
	assert.False(t, strings.Contains(trimmed, "\n"), "should be a single line")

	// Must be valid JSON
	var decoded MCPResponse
	require.NoError(t, json.Unmarshal(output, &decoded))
	assert.Equal(t, "2.0", decoded.JSONRPC)
	assert.Equal(t, float64(42), decoded.ID)
}

// TestSendErrorWritesErrorResponse verifies that sendError produces
// a properly structured JSON-RPC error response.
func TestSendErrorWritesErrorResponse(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	server.sendError(7, -32700, "Parse error")
	_ = w.Close()

	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var decoded MCPResponse
	require.NoError(t, json.Unmarshal(output, &decoded))
	assert.Equal(t, "2.0", decoded.JSONRPC)
	assert.Equal(t, float64(7), decoded.ID)
	require.NotNil(t, decoded.Error)
	assert.Equal(t, -32700, decoded.Error.Code)
	assert.Equal(t, "Parse error", decoded.Error.Message)
}

// TestSendErrorWithNilID verifies sendError works when id is nil
// (as happens for parse errors before the id is known).
func TestSendErrorWithNilID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	server.sendError(nil, -32700, "Parse error")
	_ = w.Close()

	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var decoded MCPResponse
	require.NoError(t, json.Unmarshal(output, &decoded))
	assert.Nil(t, decoded.ID)
	require.NotNil(t, decoded.Error)
	assert.Equal(t, -32700, decoded.Error.Code)
}

// TestHandleRequestWithNullID verifies that a request with null id
// still produces a valid response with null id.
func TestHandleRequestWithNullID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: nil, Method: "initialize"})
	require.NotNil(t, resp)
	assert.Nil(t, resp.ID)
	assert.Nil(t, resp.Error)
}

// TestHandleRequestWithStringID verifies that string-typed IDs are preserved
// in the response (JSON-RPC allows strings or numbers as id).
func TestHandleRequestWithStringID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: "abc-123", Method: "initialize"})
	require.NotNil(t, resp)
	assert.Equal(t, "abc-123", resp.ID)
	assert.Nil(t, resp.Error)
}

// TestHandleRequestNotificationsInitializedReturnsNil verifies that the
// "notifications/initialized" lifecycle notification returns nil (no response).
func TestHandleRequestNotificationsInitializedReturnsNil(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 1, Method: "notifications/initialized"})
	assert.Nil(t, resp, "notifications/initialized should return nil (no response)")
}

// TestHandleRequestInitializedReturnsNil verifies that the bare "initialized"
// method also returns nil.
func TestHandleRequestInitializedReturnsNil(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialized"})
	assert.Nil(t, resp, "initialized should return nil (no response)")
}

// TestHandleToolCallSuccessPathFormatsResultAsContent verifies that a
// successful tool call returns properly structured MCP content with JSON.
func TestHandleToolCallSuccessPathFormatsResultAsContent(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: test\n")

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      10,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "kustomize_build",
			"arguments": map[string]interface{}{"path": dir},
		}),
	})

	require.Nil(t, resp.Error, "successful tool call should not have an error")
	assert.EqualValues(t, 10, resp.ID)

	payload := resp.Result.(map[string]interface{})
	// Success responses should NOT have isError set
	_, hasIsError := payload["isError"]
	assert.False(t, hasIsError, "successful responses should not include isError")

	content := payload["content"].([]map[string]interface{})
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])

	// The text field should be valid JSON
	var parsed interface{}
	require.NoError(t, json.Unmarshal([]byte(content[0]["text"].(string)), &parsed))
}

// TestHandleToolCallWithNullParams verifies that null params returns an
// invalid params error.
func TestHandleToolCallWithNullParams(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      5,
		Params:  nil,
	})

	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
}

// TestHandleToolCallWithEmptyParams verifies that empty JSON object params
// (missing "name") returns an unknown tool error.
func TestHandleToolCallWithEmptyParams(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      6,
		Params:  mustMarshalJSON(t, map[string]interface{}{}),
	})

	// Empty name dispatches to default case → unknown tool
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Unknown tool")
}

// TestRunLargeMessageWithinBufferLimit verifies that messages up to 1MB
// are handled correctly by the scanner buffer.
func TestRunLargeMessageWithinBufferLimit(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// Create a request with a large params field (under 1MB)
	largeValue := strings.Repeat("x", 500000)
	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "missing_tool",
			"arguments": map[string]interface{}{"data": largeValue},
		}),
	}

	input := marshalRequest(t, req) + "\n"

	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdin = stdinR
	os.Stdout = stdoutW

	// Write in a goroutine to avoid pipe buffer deadlock — the message
	// exceeds the OS pipe buffer (~64KB on Linux), so writing must happen
	// concurrently with server.Run() reading from the other end.
	go func() {
		_, _ = stdinW.WriteString(input)
		_ = stdinW.Close()
	}()

	runErr := server.Run()
	_ = stdoutW.Close()

	assert.NoError(t, runErr)

	output, err := io.ReadAll(stdoutR)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.Len(t, lines, 1)

	var resp MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp))
	// Should get an "Unknown tool" error for "missing_tool"
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

// TestHandleInitializeResponseStructure verifies the full structure of the
// initialize response matches MCP protocol requirements.
func TestHandleInitializeResponseStructure(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.Error)

	result := resp.Result.(map[string]interface{})
	assert.Equal(t, "2024-11-05", result["protocolVersion"])

	serverInfo := result["serverInfo"].(map[string]string)
	assert.Equal(t, ServerName, serverInfo["name"])
	assert.Equal(t, ServerVersion, serverInfo["version"])

	caps := result["capabilities"].(map[string]interface{})
	_, hasTools := caps["tools"]
	assert.True(t, hasTools, "capabilities must include tools")
}

// marshalRequest is a test helper that marshals an MCPRequest to a JSON string.
func marshalRequest(t *testing.T, req MCPRequest) string {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	return string(data)
}

// TestHandleToolCallErrorPathFormatsAsContent verifies that handler errors
// are returned as isError:true content blocks (not JSON-RPC errors).
func TestHandleToolCallErrorPathFormatsAsContent(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// Call deploy_app with invalid arguments to trigger a handler error
	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      20,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "deploy_app",
			"arguments": map[string]interface{}{},
		}),
	})

	// Handler errors should NOT be JSON-RPC errors
	assert.Nil(t, resp.Error, "handler errors should be content, not protocol errors")

	payload := resp.Result.(map[string]interface{})
	assert.Equal(t, true, payload["isError"])

	content := payload["content"].([]map[string]interface{})
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	text := content[0]["text"].(string)
	assert.True(t, strings.HasPrefix(text, "Error:"), "error content should start with 'Error:'")
}

// TestHandleRequestPreservesNumericID verifies various numeric ID types.
func TestHandleRequestPreservesNumericID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	tests := []struct {
		name string
		id   interface{}
	}{
		{"integer", 42},
		{"float", 3.14},
		{"zero", 0},
		{"negative", -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := server.handleRequest(&MCPRequest{JSONRPC: "2.0", ID: tc.id, Method: "initialize"})
			require.NotNil(t, resp)
			assert.Equal(t, tc.id, resp.ID)
		})
	}
}

// TestSendResponseMultipleCallsProduceSeparateLines verifies that multiple
// sendResponse calls produce separate newline-delimited JSON lines.
func TestSendResponseMultipleCallsProduceSeparateLines(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	for i := 1; i <= 3; i++ {
		server.sendResponse(&MCPResponse{
			JSONRPC: "2.0",
			ID:      i,
			Result:  fmt.Sprintf("result-%d", i),
		})
	}
	_ = w.Close()

	output, err := io.ReadAll(r)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	assert.Len(t, lines, 3, "3 sendResponse calls should produce 3 lines")

	for i, line := range lines {
		var resp MCPResponse
		require.NoError(t, json.Unmarshal([]byte(line), &resp), "line %d should be valid JSON", i)
	}
}
