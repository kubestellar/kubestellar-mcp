package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunDispatchesAllMethodCases covers every branch of handleRequest by
// feeding one line per case through the Run loop and inspecting the emitted
// responses (or lack thereof, for notifications).
func TestRunDispatchesAllMethodCases(t *testing.T) {
	// tools/call uses "ping"-style handling only if we pick a tool whose
	// handler works with an empty Server; simplest is to hit the "Unknown
	// tool" branch inside handleToolsCall — that still exercises the
	// tools/call case of handleRequest.
	callParams, err := json.Marshal(CallToolParams{Name: "definitely_not_a_tool"})
	require.NoError(t, err)

	callReq := Request{JSONRPC: "2.0", ID: "call-1", Method: "tools/call", Params: callParams}
	callLine, err := json.Marshal(callReq)
	require.NoError(t, err)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init-1","method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"list-1","method":"tools/list"}`,
		string(callLine),
	}, "\n") + "\n"

	var output bytes.Buffer
	s := &Server{
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &output,
	}
	require.NoError(t, s.Run(context.Background()))

	responses := decodeResponses(t, output.String())
	// initialize + tools/list + tools/call → 3 responses. Both notification
	// forms ("initialized" and "notifications/initialized") return nothing.
	require.Len(t, responses, 3)

	// initialize response
	assert.Nil(t, responses[0].Error)
	assert.Equal(t, "init-1", responses[0].ID)
	var initResult InitializeResult
	require.NoError(t, json.Unmarshal(responses[0].Result, &initResult))
	assert.Equal(t, ServerName, initResult.ServerInfo.Name)

	// tools/list response
	assert.Nil(t, responses[1].Error)
	assert.Equal(t, "list-1", responses[1].ID)
	var listResult ToolsListResult
	require.NoError(t, json.Unmarshal(responses[1].Result, &listResult))
	assert.NotEmpty(t, listResult.Tools)

	// tools/call → "Unknown tool" error (still routed through the tools/call
	// case in handleRequest, so the branch is covered).
	require.NotNil(t, responses[2].Error)
	assert.Equal(t, -32602, responses[2].Error.Code)
	assert.Contains(t, responses[2].Error.Message, "Unknown tool")
	assert.Equal(t, "call-1", responses[2].ID)
}

// TestHandleToolsCallInvalidParams covers the JSON unmarshal-failure branch
// of handleToolsCall (params that don't decode into CallToolParams).
func TestHandleToolsCallInvalidParams(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	// Params is a JSON string, not an object → Unmarshal into CallToolParams
	// fails with a type-mismatch error.
	s.handleToolsCall(context.Background(), &Request{
		ID:     "bad-1",
		Params: json.RawMessage(`"not-an-object"`),
	})

	responses := decodeResponses(t, buf.String())
	require.Len(t, responses, 1)
	require.NotNil(t, responses[0].Error)
	assert.Equal(t, -32602, responses[0].Error.Code)
	assert.Equal(t, "Invalid params", responses[0].Error.Message)
	assert.Equal(t, "bad-1", responses[0].ID)
}

// TestRunReturnsCtxErrWhenContextCancelled covers the ctx.Done() branch at
// the top of Run's select.
func TestRunReturnsCtxErrWhenContextCancelled(t *testing.T) {
	// Reader has no data; if Run doesn't honor ctx.Done, it would block on
	// ReadBytes forever. Cancelling before Run enters the select makes the
	// ctx.Done case fire immediately.
	var buf bytes.Buffer
	s := &Server{
		reader: bufio.NewReader(strings.NewReader("")),
		writer: &buf,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	// Nothing should have been written.
	assert.Empty(t, buf.String())
}

// TestRunReturnsErrorOnNonEOFReadFailure covers the "return fmt.Errorf" path
// in Run when the reader returns a non-EOF error.
func TestRunReturnsErrorOnNonEOFReadFailure(t *testing.T) {
	sentinel := errors.New("boom-from-reader")
	// iotest.ErrReader returns (0, sentinel) on every Read → ReadBytes wraps
	// it as a non-EOF error.
	var buf bytes.Buffer
	s := &Server{
		reader: bufio.NewReader(iotest.ErrReader(sentinel)),
		writer: &buf,
	}

	err := s.Run(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, io.EOF)
	assert.Contains(t, err.Error(), "failed to read request")
	assert.ErrorIs(t, err, sentinel)
	// No responses written (error surfaces before dispatch).
	assert.Empty(t, buf.String())
}

// TestRunHandlesEOFCleanly covers the io.EOF return branch (already tested
// implicitly by TestRunHandlesParseErrorsAndRequests, but exercise it in
// isolation to prevent regression).
func TestRunHandlesEOFCleanly(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{
		reader: bufio.NewReader(strings.NewReader("")), // empty → EOF
		writer: &buf,
	}
	assert.NoError(t, s.Run(context.Background()))
	assert.Empty(t, buf.String())
}
