package protocol

import (
	"encoding/json"
	"testing"
)

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

func TestNewResult(t *testing.T) {
	resp := NewResult(42, map[string]string{"foo": "bar"})
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.ID != 42 {
		t.Errorf("ID = %v, want 42", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("Error = %v, want nil", resp.Error)
	}
	m, ok := resp.Result.(map[string]string)
	if !ok || m["foo"] != "bar" {
		t.Errorf("Result = %v, want map[foo:bar]", resp.Result)
	}
	// On-wire success shape must omit the "error" key.
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"jsonrpc":"2.0","id":42,"result":{"foo":"bar"}}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
}

func TestNewResultNilID(t *testing.T) {
	// id: null is valid JSON-RPC (notifications carry no id, but a nil id
	// on an error reply is used when the request could not even be parsed).
	resp := NewResult(nil, nil)
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.ID != nil {
		t.Errorf("ID = %v, want nil", resp.ID)
	}
}

func TestNewError(t *testing.T) {
	resp := NewError("abc", -32601, "Method not found", nil)
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, JSONRPCVersion)
	}
	if resp.ID != "abc" {
		t.Errorf("ID = %v, want abc", resp.ID)
	}
	if resp.Result != nil {
		t.Errorf("Result = %v, want nil", resp.Result)
	}
	if resp.Error == nil {
		t.Fatalf("Error = nil, want populated")
	}
	if resp.Error.Code != -32601 || resp.Error.Message != "Method not found" || resp.Error.Data != nil {
		t.Errorf("Error = %+v, want {-32601 Method not found <nil>}", resp.Error)
	}
	// data == nil must be omitted from the wire form.
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"jsonrpc":"2.0","id":"abc","error":{"code":-32601,"message":"Method not found"}}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
}

func TestNewErrorWithData(t *testing.T) {
	resp := NewError(1, -32602, "Invalid params", map[string]string{"field": "name"})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":{"field":"name"}}}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
}
