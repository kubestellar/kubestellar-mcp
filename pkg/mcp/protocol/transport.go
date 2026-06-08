package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
)

// Transport wraps an io.Writer and provides thread-safe methods for sending
// JSON-RPC responses.
type Transport struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewTransport creates a new Transport that writes to w.
func NewTransport(w io.Writer) *Transport {
	return &Transport{writer: w}
}

// SendResult sends a successful JSON-RPC response with the given id and result.
func (t *Transport) SendResult(id interface{}, result interface{}) {
	t.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// SendError sends a JSON-RPC error response with the given id, error code,
// message, and optional data.
func (t *Transport) SendError(id interface{}, code int, message string, data interface{}) {
	t.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (t *Transport) send(resp Response) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to marshal MCP response: %v", err)
		return
	}
	_, _ = fmt.Fprintf(t.writer, "%s\n", data)
}
