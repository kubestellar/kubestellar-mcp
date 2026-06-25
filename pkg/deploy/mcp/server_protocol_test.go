package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunHandlesMultipleRequestsAndEOF tests the main stdin/stdout message loop
func TestRunHandlesMultipleRequestsAndEOF(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// Create pipes for stdin/stdout
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	// Replace os.Stdin and os.Stdout
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinReader
	os.Stdout = stdoutWriter
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	// Capture stdout in a goroutine
	var outputBuf bytes.Buffer
	outputDone := make(chan struct{})
	go func() {
		io.Copy(&outputBuf, stdoutReader)
		close(outputDone)
	}()

	// Run server in a goroutine
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run()
	}()

	// Send multiple requests
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}

	for _, req := range requests {
		_, err := stdinWriter.Write([]byte(req + "\n"))
		require.NoError(t, err)
	}

	// Close stdin to trigger EOF
	stdinWriter.Close()
	stdoutWriter.Close()

	// Wait for server to finish
	err := <-serverDone
	require.NoError(t, err)

	<-outputDone

	// Verify responses
	output := outputBuf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have 2 responses (initialize and tools/list, but not for notification)
	require.Len(t, lines, 2)

	var resp1 MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp1))
	assert.Equal(t, "2.0", resp1.JSONRPC)
	assert.Equal(t, float64(1), resp1.ID)
	assert.Nil(t, resp1.Error)

	var resp2 MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &resp2))
	assert.Equal(t, "2.0", resp2.JSONRPC)
	assert.Equal(t, float64(2), resp2.ID)
	assert.Nil(t, resp2.Error)
}

// TestRunHandlesEmptyLines tests that empty lines are skipped
func TestRunHandlesEmptyLines(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinReader
	os.Stdout = stdoutWriter
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	var outputBuf bytes.Buffer
	outputDone := make(chan struct{})
	go func() {
		io.Copy(&outputBuf, stdoutReader)
		close(outputDone)
	}()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run()
	}()

	// Send request with empty lines
	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n\n"
	_, err := stdinWriter.Write([]byte(input))
	require.NoError(t, err)

	stdinWriter.Close()
	stdoutWriter.Close()

	err = <-serverDone
	require.NoError(t, err)
	<-outputDone

	output := outputBuf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have 1 response (empty lines ignored)
	require.Len(t, lines, 1)

	var resp MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp))
	assert.Equal(t, float64(1), resp.ID)
}

// TestRunHandlesMalformedJSON tests that malformed JSON returns parse error
func TestRunHandlesMalformedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinReader
	os.Stdout = stdoutWriter
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	var outputBuf bytes.Buffer
	outputDone := make(chan struct{})
	go func() {
		io.Copy(&outputBuf, stdoutReader)
		close(outputDone)
	}()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run()
	}()

	// Send malformed JSON
	_, err := stdinWriter.Write([]byte(`{invalid json}\n`))
	require.NoError(t, err)

	stdinWriter.Close()
	stdoutWriter.Close()

	err = <-serverDone
	require.NoError(t, err)
	<-outputDone

	output := outputBuf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	require.Len(t, lines, 1)

	var resp MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.ID)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32700, resp.Error.Code)
	assert.Equal(t, "Parse error", resp.Error.Message)
}

// TestSendResponseWritesNewlineDelimitedJSON tests JSON framing
func TestSendResponseWritesNewlineDelimitedJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	// Capture stdout
	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	go func() {
		io.Copy(&buf, r)
	}()

	// Send a response
	resp := &MCPResponse{
		JSONRPC: "2.0",
		ID:      123,
		Result: map[string]interface{}{
			"test": "value",
		},
	}
	server.sendResponse(resp)

	// Close pipe and wait for copy
	w.Close()

	output := buf.String()

	// Verify it's valid JSON
	var parsed MCPResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed))
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Equal(t, float64(123), parsed.ID)

	// Verify it ends with newline
	assert.True(t, strings.HasSuffix(output, "\n"), "response should end with newline")
}

// TestSendErrorWritesFormattedErrorResponse tests error JSON framing
func TestSendErrorWritesFormattedErrorResponse(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	go func() {
		io.Copy(&buf, r)
	}()

	server.sendError(456, -32601, "Method not found")

	w.Close()

	output := buf.String()

	var parsed MCPResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed))
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Equal(t, float64(456), parsed.ID)
	require.NotNil(t, parsed.Error)
	assert.Equal(t, -32601, parsed.Error.Code)
	assert.Equal(t, "Method not found", parsed.Error.Message)
}

// TestSendErrorWithNilID tests error response with nil ID
func TestSendErrorWithNilID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	go func() {
		io.Copy(&buf, r)
	}()

	server.sendError(nil, -32700, "Parse error")

	w.Close()

	output := buf.String()

	var parsed MCPResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed))
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Nil(t, parsed.ID)
	require.NotNil(t, parsed.Error)
	assert.Equal(t, -32700, parsed.Error.Code)
}

// TestHandleRequestNotificationReturnsNil tests that notifications return nil
func TestHandleRequestNotificationReturnsNil(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	tests := []struct {
		name   string
		method string
	}{
		{"initialized", "initialized"},
		{"notifications/initialized", "notifications/initialized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := server.handleRequest(&MCPRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.method,
			})
			assert.Nil(t, resp, "notification should return nil response")
		})
	}
}

// TestHandleRequestMissingJSONRPC tests that requests without jsonrpc field still work
func TestHandleRequestMissingJSONRPC(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{
		ID:     1,
		Method: "initialize",
	})

	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
}

// TestHandleRequestNullID tests handling of null ID field
func TestHandleRequestNullID(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleRequest(&MCPRequest{
		JSONRPC: "2.0",
		Method:  "unknown_method",
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.ID)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

// TestHandleToolCallSuccessPathWithKustomizeBuild tests the success path for tool calls
func TestHandleToolCallSuccessPathWithKustomizeBuild(t *testing.T) {
	setupFakeKustomize(t)
	server := newHelmTestServer(t, map[string]string{})
	dir := createTestKustomization(t, "kustomization.yaml")
	t.Setenv("FAKE_KUSTOMIZE_BUILD_STDOUT", "kind: ConfigMap\nmetadata:\n  name: demo\n")

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "kustomize_build",
			"arguments": map[string]interface{}{"path": dir},
		}),
	})

	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, float64(1), resp.ID)
	assert.Nil(t, resp.Error, "successful tool call should not have error")

	payload := resp.Result.(map[string]interface{})
	assert.False(t, payload["isError"].(bool), "successful tool call should have isError=false")

	content := payload["content"].([]map[string]interface{})
	require.Len(t, content, 1)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(content[0]["text"].(string)), &result))
	assert.Equal(t, dir, result["path"])
	assert.Equal(t, float64(1), result["resources"])
}

// TestHandleToolCallInvalidJSONParams tests handling of invalid JSON in params
func TestHandleToolCallInvalidJSONParams(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params:  []byte(`{invalid`),
	})

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
}

// TestHandleToolCallUnknownToolName tests handling of unknown tool names
func TestHandleToolCallUnknownToolName(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	resp := server.handleToolCall(context.Background(), &MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Params: mustMarshalJSON(t, map[string]interface{}{
			"name":      "nonexistent_tool",
			"arguments": map[string]interface{}{},
		}),
	})

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Unknown tool")
}

// TestRunHandlesLargeMessages tests the 1MB buffer handling
func TestRunHandlesLargeMessages(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinReader
	os.Stdout = stdoutWriter
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	var outputBuf bytes.Buffer
	outputDone := make(chan struct{})
	go func() {
		io.Copy(&outputBuf, stdoutReader)
		close(outputDone)
	}()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run()
	}()

	// Create a large but valid initialize request (well under 1MB limit)
	largeRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{
			"capabilities": strings.Repeat("x", 50000), // 50KB of data
		},
	}
	requestJSON, err := json.Marshal(largeRequest)
	require.NoError(t, err)

	_, err = stdinWriter.Write(append(requestJSON, '\n'))
	require.NoError(t, err)

	stdinWriter.Close()
	stdoutWriter.Close()

	err = <-serverDone
	require.NoError(t, err)
	<-outputDone

	output := outputBuf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	require.Len(t, lines, 1)

	var resp MCPResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp))
	assert.Equal(t, float64(1), resp.ID)
	assert.Nil(t, resp.Error)
}
