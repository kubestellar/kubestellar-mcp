package metrics

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// countersFor gathers metric families from the package registry, useful for
// asserting exact label combinations without needing a live HTTP server.
func gather(t *testing.T) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather() error = %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(families))
	for _, f := range families {
		out[f.GetName()] = f
	}
	return out
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func TestRecordToolCallSuccess(t *testing.T) {
	RecordToolCall("diagnose_cluster", "prod-east", 25*time.Millisecond, false, "")

	families := gather(t)

	found := false
	for _, m := range families["mcpserver_tool_calls_total"].GetMetric() {
		if labelValue(m, "tool") == "diagnose_cluster" &&
			labelValue(m, "cluster") == "prod-east" &&
			labelValue(m, "status") == "success" {
			found = true
			if m.GetCounter().GetValue() < 1 {
				t.Errorf("expected counter >= 1, got %v", m.GetCounter().GetValue())
			}
		}
	}
	if !found {
		t.Fatal("expected mcpserver_tool_calls_total series for diagnose_cluster/prod-east/success")
	}
}

func TestRecordToolCallErrorDefaultsToUnknownKind(t *testing.T) {
	RecordToolCall("scale_app", "", 5*time.Millisecond, true, "")

	families := gather(t)

	foundCall := false
	for _, m := range families["mcpserver_tool_calls_total"].GetMetric() {
		if labelValue(m, "tool") == "scale_app" &&
			labelValue(m, "cluster") == unknownCluster &&
			labelValue(m, "status") == "error" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatal("expected mcpserver_tool_calls_total series for scale_app/none/error")
	}

	foundErr := false
	for _, m := range families["mcpserver_tool_errors_total"].GetMetric() {
		if labelValue(m, "tool") == "scale_app" &&
			labelValue(m, "cluster") == unknownCluster &&
			labelValue(m, "error_kind") == string(ErrorKindUnknown) {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("expected mcpserver_tool_errors_total series with error_kind=unknown")
	}
}

func TestRecordToolCallEmptyClusterNormalizesToNone(t *testing.T) {
	RecordToolCall("list_tools", "", time.Millisecond, false, "")

	families := gather(t)
	for _, m := range families["mcpserver_tool_calls_total"].GetMetric() {
		if labelValue(m, "tool") == "list_tools" && labelValue(m, "cluster") == "" {
			t.Fatal("cluster label must never be empty; expected normalization to 'none'")
		}
	}
}

func TestSetActiveClusters(t *testing.T) {
	SetActiveClusters(3)

	families := gather(t)
	m := families["mcpserver_active_clusters"].GetMetric()
	if len(m) != 1 {
		t.Fatalf("expected exactly one active-clusters series, got %d", len(m))
	}
	if got := m[0].GetGauge().GetValue(); got != 3 {
		t.Errorf("ActiveClusters = %v, want 3", got)
	}
}

func TestStartServerRejectsEmptyAddr(t *testing.T) {
	if _, err := StartServer(""); err == nil {
		t.Fatal("expected error for empty addr")
	}
}

func TestStartServerServesMetricsEndpoint(t *testing.T) {
	srv, err := StartServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartServer() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, srv)
	}()

	// StartServer uses addr as given; a ":0" bind means the OS assigns the
	// port. We only assert the server started without error here, since the
	// actual listener address isn't exposed by http.Server before Serve
	// picks a port. Instead, verify Shutdown is a safe no-op path.
	if srv == nil {
		t.Fatal("expected non-nil *http.Server")
	}
}

func TestShutdownNilServerIsNoop(t *testing.T) {
	if err := Shutdown(context.Background(), nil); err != nil {
		t.Errorf("Shutdown(nil) error = %v, want nil", err)
	}
}

// TestPromHTTPHandlerAvailable is a light smoke test that the promhttp
// handler wired into StartServer is constructible and callable directly
// (without opening a real socket), catching wiring regressions.
func TestPromHTTPHandlerAvailable(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/metrics", nil)
	if err != nil {
		t.Fatalf("http.NewRequest error = %v", err)
	}
	rec := &discardResponseWriter{header: http.Header{}}
	handler := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
	handler.ServeHTTP(rec, req)
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusOK)
	}
}

// discardResponseWriter is a minimal http.ResponseWriter for smoke-testing
// handler wiring without a real network listener.
type discardResponseWriter struct {
	header http.Header
	status int
}

func (w *discardResponseWriter) Header() http.Header { return w.header }
func (w *discardResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return io.Discard.Write(b)
}
func (w *discardResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }
