package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthzHandlerReturnsOK covers healthzHandler directly: it must always
// answer 200 OK with a plaintext body, independent of any Registry state.
func TestHealthzHandlerReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	healthzHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthzHandler status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("healthzHandler Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if rec.Body.String() != "ok\n" {
		t.Fatalf("healthzHandler body = %q, want %q", rec.Body.String(), "ok\n")
	}
}

// TestStartServerServesHealthz covers the /healthz route wired up by
// StartServer end-to-end over a real listener, alongside the pre-existing
// /metrics route.
func TestStartServerServesHealthz(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	srv, err := StartServer(addr)
	if err != nil {
		t.Fatalf("StartServer(%q) error = %v", addr, err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, srv)
	}()

	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
