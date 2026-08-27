package protocol

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

func TestWriterSendResult(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.SendResult("req-1", map[string]string{"status": "ok"})

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.ID != "req-1" {
		t.Errorf("ID = %v, want req-1", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %+v", resp.Error)
	}
}

func TestWriterSendError(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.SendError("req-2", -32601, "Method not found", nil)

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
	if resp.Error.Message != "Method not found" {
		t.Errorf("error message = %q", resp.Error.Message)
	}
}

func TestTextResult(t *testing.T) {
	r := TextResult("hello")
	if len(r.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(r.Content))
	}
	if r.Content[0].Type != "text" || r.Content[0].Text != "hello" {
		t.Errorf("unexpected content: %+v", r.Content[0])
	}
	if r.IsError {
		t.Error("TextResult should not be IsError")
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("boom")
	if !r.IsError {
		t.Error("ErrorResult should set IsError=true")
	}
	if r.Content[0].Text != "boom" {
		t.Errorf("text = %q, want boom", r.Content[0].Text)
	}
}

func TestRequestUnmarshal(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Method != "tools/list" {
		t.Errorf("method = %q, want tools/list", req.Method)
	}
}

// TestWriterSendMarshalError exercises the fallback branch in Writer.Send
// that fires when json.Marshal cannot encode the response payload. A `chan int`
// is not representable in JSON and forces json.Marshal to return an error, so
// Send must emit its best-effort fallback JSON-RPC error line instead of
// writing an invalid or empty payload.
func TestWriterSendMarshalError(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.Send(Response{
		JSONRPC: JSONRPCVersion,
		ID:      "req-marshal-err",
		Result:  make(chan int),
	})

	out := buf.String()
	if out == "" {
		t.Fatal("expected fallback payload, got empty output")
	}
	// Must be a single newline-terminated line.
	if out[len(out)-1] != '\n' {
		t.Errorf("expected trailing newline, got %q", out)
	}
	// Must still parse as a JSON-RPC error response.
	var resp Response
	if err := json.Unmarshal([]byte(out[:len(out)-1]), &resp); err != nil {
		t.Fatalf("fallback line is not valid JSON: %v — payload=%q", err, out)
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("fallback JSONRPC = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.Error == nil {
		t.Fatal("expected error object in fallback response")
	}
	if resp.Error.Code != -32603 {
		t.Errorf("fallback error code = %d, want -32603 (Internal error)", resp.Error.Code)
	}
	if resp.Error.Message != "marshal error" {
		t.Errorf("fallback error message = %q, want %q", resp.Error.Message, "marshal error")
	}
}

// TestWriterSendResultMarshalErrorUsesFallback verifies that the marshal-error
// fallback also fires when reached through SendResult (not just direct Send).
func TestWriterSendResultMarshalErrorUsesFallback(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.SendResult(1, func() {}) // funcs are not JSON-marshallable

	var resp Response
	line := bytes.TrimRight(buf.Bytes(), "\n")
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("fallback line is not valid JSON: %v — payload=%q", err, buf.String())
	}
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Errorf("expected fallback -32603 error, got %+v", resp.Error)
	}
}

// TestWriterSendConcurrentLinesUnbroken asserts that Writer serializes
// concurrent Send calls: each Response ends up as exactly one newline-
// terminated JSON line, with no interleaving between goroutines.
func TestWriterSendConcurrentLinesUnbroken(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	const goroutines = 16
	const perGoroutine = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				w.SendResult(id, map[string]int{"g": id, "i": i})
			}
		}(g)
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	want := goroutines * perGoroutine
	if len(lines) != want {
		t.Fatalf("line count = %d, want %d", len(lines), want)
	}
	for i, line := range lines {
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("line %d not valid JSON (interleaving?): %v — %q", i, err, line)
		}
		if resp.JSONRPC != JSONRPCVersion {
			t.Errorf("line %d JSONRPC = %q", i, resp.JSONRPC)
		}
	}
}

// TestWriterSendErrorWithData verifies that non-nil Data on SendError is
// serialized inside the error object and reaches the wire intact.
func TestWriterSendErrorWithData(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.SendError("req-data", -32602, "Invalid params", map[string]string{"field": "name"})

	var resp Response
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error object")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("code = %d, want -32602", resp.Error.Code)
	}
	data, ok := resp.Error.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Data = %T, want map[string]interface{}", resp.Error.Data)
	}
	if data["field"] != "name" {
		t.Errorf("Data[field] = %v, want name", data["field"])
	}
}

func TestCallToolParamsUnmarshal(t *testing.T) {
	raw := `{"name":"get_clusters","arguments":{"source":"all"}}`
	var params CallToolParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if params.Name != "get_clusters" {
		t.Errorf("name = %q, want get_clusters", params.Name)
	}
	if params.Arguments["source"] != "all" {
		t.Errorf("arguments[source] = %v, want all", params.Arguments["source"])
	}
}
