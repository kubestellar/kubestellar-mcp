package rpcloop

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// echoHandler is a Handler that replies to every request with its ID and
// method name, so tests can assert on dispatch without needing a real MCP
// server wired up.
func echoHandler(t *testing.T) Handler {
	t.Helper()
	return func(_ context.Context, req *protocol.Request) *protocol.Response {
		if req.Method == "initialized" || req.Method == "notifications/initialized" {
			return nil
		}
		return protocol.NewResult(req.ID, map[string]interface{}{"method": req.Method})
	}
}

func marshalLine(t *testing.T, req protocol.Request) string {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	return string(data) + "\n"
}

func TestLoopRunProcessesMultipleRequestsAndHandlesEOF(t *testing.T) {
	var input bytes.Buffer
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}))
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 2, Method: "tools/list"}))
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 3, Method: "unknown_method"}))

	var output bytes.Buffer
	loop := NewLoop(&input, &output)

	err := loop.Run(context.Background(), echoHandler(t))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 3)

	for i, line := range lines {
		var resp protocol.Response
		require.NoError(t, json.Unmarshal([]byte(line), &resp), "line %d", i)
		assert.Equal(t, "2.0", resp.JSONRPC)
		assert.Nil(t, resp.Error)
	}
}

func TestLoopRunSkipsEmptyLinesAndNilResponses(t *testing.T) {
	var input bytes.Buffer
	input.WriteString("\n\n")
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}))
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 2, Method: "notifications/initialized"}))
	input.WriteString("\n")

	var output bytes.Buffer
	loop := NewLoop(&input, &output)

	err := loop.Run(context.Background(), echoHandler(t))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 1, "only the initialize request should produce a response")
}

func TestLoopRunHandlesMalformedJSON(t *testing.T) {
	var input bytes.Buffer
	input.WriteString("{not valid json}\n")
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 99, Method: "initialize"}))

	var output bytes.Buffer
	loop := NewLoop(&input, &output)

	err := loop.Run(context.Background(), echoHandler(t))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)

	var parseErr protocol.Response
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &parseErr))
	require.NotNil(t, parseErr.Error)
	assert.Equal(t, -32700, parseErr.Error.Code)
	assert.Equal(t, "Parse error", parseErr.Error.Message)
	assert.Nil(t, parseErr.ID)

	var initResp protocol.Response
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &initResp))
	assert.Nil(t, initResp.Error)
	assert.EqualValues(t, 99, initResp.ID)
}

func TestLoopRunReturnsNilOnCleanEOF(t *testing.T) {
	loop := NewLoop(strings.NewReader(""), &bytes.Buffer{})
	err := loop.Run(context.Background(), echoHandler(t))
	assert.NoError(t, err)
}

func TestLoopRunRespectsContextCancellation(t *testing.T) {
	// A request followed by more input: cancel ctx before Run so the very
	// first scanner.Scan() iteration observes ctx.Done() and returns
	// ctx.Err() without invoking handler.
	var input bytes.Buffer
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}))

	var output bytes.Buffer
	loop := NewLoop(&input, &output)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handlerCalled := false
	err := loop.Run(ctx, func(ctx context.Context, req *protocol.Request) *protocol.Response {
		handlerCalled = true
		return protocol.NewResult(req.ID, nil)
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, handlerCalled, "handler should not run once ctx is already done")
}

func TestLoopRunWithinDefaultMaxFrameSize(t *testing.T) {
	largeValue := strings.Repeat("x", 500000)
	req := protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"noop","arguments":{"data":"` + largeValue + `"}}`),
	}

	var input bytes.Buffer
	input.WriteString(marshalLine(t, req))

	var output bytes.Buffer
	loop := NewLoop(&input, &output)

	err := loop.Run(context.Background(), echoHandler(t))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 1, "a request within the 1 MiB default cap should be processed")
}

func TestLoopSetMaxFrameSizeOverride(t *testing.T) {
	loop := NewLoop(strings.NewReader(""), &bytes.Buffer{})
	assert.Equal(t, DefaultMaxFrameSize, loop.effectiveMaxFrameSize())

	loop.SetMaxFrameSize(2048)
	assert.Equal(t, 2048, loop.effectiveMaxFrameSize())

	loop.SetMaxFrameSize(0)
	assert.Equal(t, DefaultMaxFrameSize, loop.effectiveMaxFrameSize(), "non-positive override restores the default")

	loop.SetMaxFrameSize(-1)
	assert.Equal(t, DefaultMaxFrameSize, loop.effectiveMaxFrameSize())
}

func TestLoopRunOversizedFrameSurfacesScanError(t *testing.T) {
	// A single line larger than the configured cap should surface as a
	// scanner error (bufio.ErrTooLong-derived) rather than being silently
	// truncated or accepted.
	oversized := strings.Repeat("y", 4096) + "\n"
	loop := NewLoop(strings.NewReader(oversized), &bytes.Buffer{})
	loop.SetMaxFrameSize(1024)

	err := loop.Run(context.Background(), echoHandler(t))
	assert.Error(t, err)
}

func TestSendResponseWritesNewlineDelimitedJSON(t *testing.T) {
	var output bytes.Buffer
	var mu sync.Mutex

	resp := protocol.NewResult(42, map[string]interface{}{"hello": "world"})
	require.NoError(t, SendResponse(&mu, &output, resp))

	data := output.Bytes()
	assert.True(t, strings.HasSuffix(string(data), "\n"))

	var decoded protocol.Response
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "2.0", decoded.JSONRPC)
	assert.EqualValues(t, 42, decoded.ID)
}

func TestSendResponseSerializesConcurrentWriters(t *testing.T) {
	var output bytes.Buffer
	var mu sync.Mutex

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = SendResponse(&mu, &output, protocol.NewResult(i, "result"))
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, n, "every write must land as its own complete, non-interleaved line")
	for _, line := range lines {
		var resp protocol.Response
		assert.NoError(t, json.Unmarshal([]byte(line), &resp), "line must be valid, non-interleaved JSON: %q", line)
	}
}

func TestNewLoopDefaultsMaxFrameSize(t *testing.T) {
	loop := NewLoop(strings.NewReader(""), &bytes.Buffer{})
	assert.Equal(t, DefaultMaxFrameSize, loop.effectiveMaxFrameSize())
}

func TestLoopRunReturnsContextErrorBeforeReading(t *testing.T) {
	// An already-cancelled context must win over an empty reader: without
	// the pre-read check, Run would report a clean EOF (nil) and callers
	// that translate nil into "graceful shutdown" could not distinguish
	// cancellation from end-of-input.
	loop := NewLoop(strings.NewReader(""), &bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loop.Run(ctx, echoHandler(t))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestLoopSetWriteMutexSharesSerializationWithCaller(t *testing.T) {
	var input bytes.Buffer
	input.WriteString(marshalLine(t, protocol.Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}))

	var output bytes.Buffer
	var shared sync.Mutex

	loop := NewLoop(&input, &output)
	loop.SetWriteMutex(&shared)
	assert.Same(t, &shared, loop.writeMu)

	require.NoError(t, loop.Run(context.Background(), echoHandler(t)))
	// The caller's own direct write goes through the same mutex, so both
	// frames land intact on the shared writer.
	require.NoError(t, SendResponse(&shared, &output, protocol.NewResult(2, "direct")))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var resp protocol.Response
		assert.NoError(t, json.Unmarshal([]byte(line), &resp))
	}
}

func TestLoopSetWriteMutexIgnoresNil(t *testing.T) {
	loop := NewLoop(strings.NewReader(""), &bytes.Buffer{})
	original := loop.writeMu

	loop.SetWriteMutex(nil)

	assert.Same(t, original, loop.writeMu, "a nil mutex must not clear the Loop's own")
}
