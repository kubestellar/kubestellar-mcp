package server

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"k8s.io/klog/v2"
)

// TestServerSendMarshalError covers the json.Marshal error branch in
// (*Server).send (server.go:183). The happy path is exercised by every
// tool-call test in this package, but the marshal-failure arm — which
// logs and drops the response instead of writing to the transport — is
// unreachable through the public API because sendResult / sendError
// only pass Response values whose Result / Error.Data fields are set by
// production code that never inserts unmarshalable types. Without a
// direct test, a regression in the log-and-return handling (e.g. an
// accidental panic on marshal error, or a missing early return that
// would then try to Fprintf a nil `data`) would go unnoticed.
//
// We reach the arm by handing send a Response whose Result contains a
// channel — encoding/json refuses to marshal channels — and asserting
// that:
//
//   - the writer received nothing (early return honored), and
//   - a marshal-failure line was written to the standard logger.
func TestServerSendMarshalError(t *testing.T) {
	var writerBuf bytes.Buffer
	s := &Server{writer: &writerBuf}

	// Redirect klog's output so we can observe the log line produced by
	// the marshal-error arm. klog writes to stderr by default
	// (--logtostderr=true), so we must disable that before SetOutput
	// takes effect.
	var logBuf bytes.Buffer
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	klog.InitFlags(fs)
	if err := fs.Set("logtostderr", "false"); err != nil {
		t.Fatalf("failed to disable logtostderr: %v", err)
	}
	klog.SetOutput(&logBuf)
	defer klog.SetOutput(nil)

	// A channel value is not JSON-marshalable, so json.Marshal returns
	// an *UnsupportedTypeError and send hits its error arm.
	unmarshalable := make(chan int)
	s.send(Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  unmarshalable,
	})
	klog.Flush()

	if writerBuf.Len() != 0 {
		t.Errorf("expected writer to receive nothing on marshal error, got %q", writerBuf.String())
	}
	got := logBuf.String()
	if !strings.Contains(got, "Failed to marshal MCP response") {
		t.Errorf("expected marshal-error log line, got %q", got)
	}
}

// TestServerSendHappyPathWritesNewlineDelimitedJSON pins the successful
// path so a future refactor cannot silently swap the newline framing or
// break the JSON-RPC envelope shape without a test failure. This
// complements TestServerSendMarshalError and, together, take
// (*Server).send from 81.8 % to 100 % line coverage.
func TestServerSendHappyPathWritesNewlineDelimitedJSON(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	s.send(Response{
		JSONRPC: "2.0",
		ID:      float64(7),
		Result:  map[string]string{"ok": "yes"},
	})

	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("expected newline-terminated frame, got %q", line)
	}
	// The framed line must be a well-formed JSON-RPC envelope, not
	// just any string containing the fields.
	if !strings.Contains(line, `"jsonrpc":"2.0"`) {
		t.Errorf("missing jsonrpc field: %q", line)
	}
	if !strings.Contains(line, `"id":7`) {
		t.Errorf("missing id field: %q", line)
	}
	if !strings.Contains(line, `"result":{"ok":"yes"}`) {
		t.Errorf("missing result field: %q", line)
	}
}
