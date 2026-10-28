// Package protocol provides shared MCP (Model Context Protocol) types
// and helpers used by both the ops and deploy MCP servers.
package protocol

import (
	"encoding/json"
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

// ClusterProperty returns the standard "cluster" string property shared by
// every tool that accepts an optional cluster name (uses current context if
// not specified). Before this helper existed, each domain's register.go
// re-declared this exact literal (same type, same description)
// independently, letting the wording drift across packages without any
// single place to fix it (see kubestellar-mcp#1302 for the same pattern in
// the sibling pkg/deploy/mcp/tooldef package).
func ClusterProperty() Property {
	return Property{
		Type:        "string",
		Description: "Cluster name (uses current context if not specified)",
	}
}

// PodNamespaceProperty returns the standard "namespace" string property
// shared by every tool that targets a specific pod. Before this helper
// existed, describe_pod and get_pod_logs each re-declared this exact
// literal (same type, same description) independently in register.go.
func PodNamespaceProperty() Property {
	return Property{
		Type:        "string",
		Description: "Namespace of the pod",
	}
}

// PodNameProperty returns the standard "name" string property shared by
// every tool that targets a specific pod. Before this helper existed,
// describe_pod and get_pod_logs each re-declared this exact literal (same
// type, same description) independently in register.go.
func PodNameProperty() Property {
	return Property{
		Type:        "string",
		Description: "Name of the pod",
	}
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

// --- JSON-RPC response envelope constructors ---
//
// Both MCP servers (pkg/mcp/server and pkg/deploy/mcp) hand-roll
// Response{JSONRPC: "2.0", ...} literals at every reply site. Centralizing
// the envelope construction here removes that duplication and gives future
// consolidation work (see kubestellar-mcp#1017) a single place to enforce
// invariants such as the fixed JSONRPC version tag. The helpers do NOT
// perform I/O; callers still choose their own transport and locking policy.

// NewResult returns a JSON-RPC success Response with the shared JSONRPC
// version tag pre-filled. The Result value is used as-is; callers should
// pass a shape defined in this package (e.g. InitializeResult,
// ToolsListResult, CallToolResult) so that on-wire output stays typed.
func NewResult(id interface{}, result interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// NewError returns a JSON-RPC error Response with the shared JSONRPC
// version tag pre-filled. Pass data == nil to omit the "data" field on the
// wire (Error.Data has omitempty).
func NewError(id interface{}, code int, message string, data interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
