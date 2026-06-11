// Package protocol provides shared MCP (Model Context Protocol) types
// and helpers used by both the ops and deploy MCP servers.
package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const (
	// JSONRPCVersion is the JSON-RPC version used by MCP.
	JSONRPCVersion = "2.0"

	// MCPVersion is the MCP protocol version.
	MCPVersion = "2024-11-05"
)

// --- JSON-RPC types ---

// Request represents an incoming JSON-RPC/MCP request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents an outgoing JSON-RPC/MCP response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// --- MCP types ---

// ServerInfo describes the MCP server identity.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the response to an MCP initialize request.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities describes the server's MCP capabilities.
type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability describes the tool-related capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// Tool describes an MCP tool schema.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is the JSON Schema for a tool's input.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a single JSON Schema property.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Items       *Items   `json:"items,omitempty"`
}

// Items describes array item types.
type Items struct {
	Type string `json:"type"`
}

// ToolsListResult wraps the tools/list response.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams is the params for a tools/call request.
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult is the result of a tools/call invocation.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a content block in tool results.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Transport helpers ---

// Writer provides thread-safe JSON-RPC response writing over a line-delimited stream.
type Writer struct {
	w  io.Writer
	mu sync.Mutex
}

// NewWriter creates a Writer that serializes responses to the given io.Writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// SendResult sends a successful JSON-RPC response.
func (w *Writer) SendResult(id interface{}, result interface{}) {
	w.Send(Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	})
}

// SendError sends a JSON-RPC error response.
func (w *Writer) SendError(id interface{}, code int, message string, data interface{}) {
	w.Send(Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

// Send marshals and writes a Response as a newline-terminated JSON line.
func (w *Writer) Send(resp Response) {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		// Best-effort: log and continue
		fmt.Fprintf(w.w, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"marshal error"}}`+"\n")
		return
	}
	fmt.Fprintf(w.w, "%s\n", data)
}

// TextResult is a convenience helper that builds a CallToolResult with a single text block.
func TextResult(text string) CallToolResult {
	return CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

// ErrorResult is a convenience helper that builds a CallToolResult marked as error.
func ErrorResult(text string) CallToolResult {
	return CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
		IsError: true,
	}
}
