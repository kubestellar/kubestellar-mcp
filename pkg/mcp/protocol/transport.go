package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
)

// Transport handles JSON-RPC message serialization over a writer.
// It is safe for concurrent use.
type Transport struct {
	writer io.Writer
	mu     sync.Mutex
}

// NewTransport creates a transport that writes to w.
func NewTransport(w io.Writer) *Transport {
	return &Transport{writer: w}
}

// SendResult writes a successful JSON-RPC response.
func (t *Transport) SendResult(id interface{}, result interface{}) {
	t.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// SendError writes a JSON-RPC error response.
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
