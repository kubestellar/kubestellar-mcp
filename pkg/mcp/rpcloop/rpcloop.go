// Package rpcloop provides the shared stdio JSON-RPC dispatch loop for
// KubeStellar's MCP servers.
//
// Both kubestellar-ops (pkg/mcp/server) and kubestellar-deploy
// (pkg/deploy/mcp) used to hand-roll their own newline-delimited JSON-RPC
// read loop, response writer, and per-tool-call tracing/metrics/logging
// wrapper (see kubestellar-mcp#1017); both now run on this package, so
// cross-cutting changes (framing policy, write safety, telemetry
// attributes) have a single owner and a single test suite.
//
// Framing policy: Loop reads newline-delimited requests via bufio.Scanner
// with a buffer capped at MaxFrameSize (default DefaultMaxFrameSize, 1 MiB).
// This adopts pkg/deploy/mcp's pre-existing bounded-buffer behavior rather
// than pkg/mcp/server's uncapped bufio.Reader.ReadBytes('\n') read, because
// an uncapped read lets a single oversized frame grow the process's memory
// without limit. A frame at or beyond the cap surfaces as a scanner error
// (bufio.ErrTooLong) and ends Run - callers that need the loop to recover
// from an oversized frame and keep serving subsequent requests should raise
// MaxFrameSize instead of relying on scanner recovery.
package rpcloop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

// DefaultMaxFrameSize is the default cap on a single newline-delimited
// JSON-RPC request, in bytes. It matches pkg/deploy/mcp's pre-existing
// 1 MiB bufio.Scanner buffer.
const DefaultMaxFrameSize = 1024 * 1024

// initialScannerBufferSize is the scanner's starting buffer size; it grows
// up to MaxFrameSize as needed, mirroring pkg/deploy/mcp's prior literal.
const initialScannerBufferSize = 64 * 1024

// Handler decodes and dispatches a single JSON-RPC request, returning the
// response Loop.Run should write, or nil for requests that expect no reply
// (e.g. the "initialized"/"notifications/initialized" lifecycle
// notifications).
type Handler func(ctx context.Context, req *protocol.Request) *protocol.Response

// Loop owns the stdio transport for an MCP server: reading newline-delimited
// JSON-RPC requests up to MaxFrameSize, decoding them, invoking a Handler,
// and writing any resulting response back to the writer under a write
// mutex - its own by default, or a caller-supplied one via SetWriteMutex -
// so concurrent Loop.Run/SendResponse-style writers never interleave
// partial output.
type Loop struct {
	reader       io.Reader
	writer       io.Writer
	writeMu      *sync.Mutex
	maxFrameSize int
}

// NewLoop constructs a Loop reading requests from r and writing responses to
// w. MaxFrameSize defaults to DefaultMaxFrameSize; call SetMaxFrameSize
// before Run to override it.
func NewLoop(r io.Reader, w io.Writer) *Loop {
	return &Loop{reader: r, writer: w, writeMu: &sync.Mutex{}}
}

// SetWriteMutex makes the Loop serialize its own writes on mu instead of on
// its private mutex, so a server that also writes responses to the same
// writer outside the read loop (e.g. a synchronous reply built by a handler)
// shares one serialization domain with Run rather than racing it through a
// second, independent mutex. A nil mu is ignored.
func (l *Loop) SetWriteMutex(mu *sync.Mutex) {
	if mu == nil {
		return
	}
	l.writeMu = mu
}

// SetMaxFrameSize overrides the maximum accepted request size, in bytes. A
// value <= 0 restores DefaultMaxFrameSize.
func (l *Loop) SetMaxFrameSize(n int) {
	l.maxFrameSize = n
}

func (l *Loop) effectiveMaxFrameSize() int {
	if l.maxFrameSize <= 0 {
		return DefaultMaxFrameSize
	}
	return l.maxFrameSize
}

// Run reads newline-delimited JSON-RPC requests until EOF, ctx is done, or a
// read error occurs, dispatching each decoded request to handler and
// writing any non-nil response. Blank lines are skipped without invoking
// handler. Malformed JSON on a line produces a JSON-RPC -32700 "Parse
// error" response (id: null) and Run continues with the next line.
//
// Run returns nil on a clean EOF (matching both pre-existing servers'
// behavior of treating end-of-input as graceful shutdown, not an error),
// ctx.Err() if ctx was done, or the underlying scan error otherwise.
func (l *Loop) Run(ctx context.Context, handler Handler) error {
	// Checked before the first read as well as on every iteration: a
	// caller that cancels ctx before Run starts must observe ctx.Err(),
	// never a nil "clean EOF" just because the reader happened to be
	// empty. This preserves pkg/mcp/server's pre-existing
	// check-context-before-reading semantics.
	if err := ctx.Err(); err != nil {
		return err
	}

	maxFrameSize := l.effectiveMaxFrameSize()

	scanner := bufio.NewScanner(l.reader)
	// bufio.Scanner.Buffer documents that the effective maximum token size
	// is the larger of the two arguments - cap(buf) and max - so an
	// initial buffer capacity greater than maxFrameSize would silently
	// raise the real cap above what SetMaxFrameSize configured. Keep the
	// initial buffer no larger than maxFrameSize itself so small
	// maxFrameSize values (e.g. in tests) are actually enforced, while
	// still starting below the default cap to avoid over-allocating for
	// the common case.
	initialSize := initialScannerBufferSize
	if initialSize > maxFrameSize {
		initialSize = maxFrameSize
	}
	buf := make([]byte, 0, initialSize)
	scanner.Buffer(buf, maxFrameSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var req protocol.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			// err's message comes from encoding/json and never embeds the
			// raw line content, so logging it stays bounded: it cannot
			// leak arbitrary client-supplied payload data into logs.
			klog.V(2).InfoS("mcp request parse error", "error", err)
			l.send(protocol.NewError(nil, -32700, "Parse error", nil))
			continue
		}

		if resp := handler(ctx, &req); resp != nil {
			l.send(resp)
		}
	}

	if err := scanner.Err(); err != nil {
		// Run's godoc documents this as the "underlying scan error"
		// return path (e.g. a frame at/beyond maxFrameSize, or a
		// transport read failure). Previously this reason was only
		// visible if a caller happened to print the returned error;
		// logging it here means the loop's shutdown cause is always
		// captured in structured logs, regardless of caller.
		klog.ErrorS(err, "mcp rpc loop stopped due to a read/scan error")
		return err
	}

	return nil
}

// send marshals and writes resp followed by a newline, serialized by l's
// internal mutex.
func (l *Loop) send(resp *protocol.Response) {
	_ = SendResponse(l.writeMu, l.writer, resp)
}

// SendResponse marshals resp to JSON and writes it followed by a newline to
// w, serialized by mu so concurrent callers writing to the same w cannot
// interleave partial writes. mu is caller-owned so a server can share
// serialization between its own direct response-sending call sites (e.g. a
// synchronous error reply built outside Run) and Loop.Run's own writes by
// passing the same *sync.Mutex to both, though most callers only need one
// or the other.
func SendResponse(mu *sync.Mutex, w io.Writer, resp *protocol.Response) error {
	mu.Lock()
	defer mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("rpcloop: failed to marshal response: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
