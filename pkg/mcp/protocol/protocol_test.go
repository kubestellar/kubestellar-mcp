package protocol

import (
	"bytes"
	"encoding/json"
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
