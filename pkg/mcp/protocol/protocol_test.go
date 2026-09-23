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
