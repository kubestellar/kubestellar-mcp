package metrics

import (
	"net"
	"strings"
	"testing"
)

// TestStartServerReturnsBindError covers the immediate-bind-failure branch
// of StartServer: when the goroutine's ListenAndServe returns an error that
// is not http.ErrServerClosed within the 50ms window, StartServer must
// surface it to the caller (metrics.go:132-133).
//
// We hold a real listener on 127.0.0.1:<port> and then ask StartServer to
// bind the same address, which forces "address already in use" back through
// the select-case-err arm — the previously-uncovered non-ErrServerClosed path.
func TestStartServerReturnsBindError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer l.Close()

	addr := l.Addr().String()

	srv, err := StartServer(addr)
	if err == nil {
		if srv != nil {
			_ = srv.Close()
		}
		t.Fatalf("StartServer(%q) succeeded despite occupied port; want bind error", addr)
	}
	if srv != nil {
		t.Fatalf("StartServer returned non-nil server on error: %v", srv)
	}
	// Confirm it's the bind-time error, not the empty-addr guard.
	if strings.Contains(err.Error(), "addr must not be empty") {
		t.Fatalf("StartServer returned empty-addr error, want bind failure: %v", err)
	}
}
