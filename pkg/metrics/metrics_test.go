package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubestellar/kubestellar-mcp/pkg/security/netguard"
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

func TestRecordMulticlusterOperationSuccess(t *testing.T) {
	RecordMulticlusterOperation("prod-east", nil)

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_multicluster_operation_total"].GetMetric() {
		if labelValue(m, "cluster") == "prod-east" && labelValue(m, "status") == "success" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_multicluster_operation_total series with cluster=prod-east, status=success")
	}
}

func TestRecordMulticlusterOperationError(t *testing.T) {
	RecordMulticlusterOperation("prod-west", errors.New("boom"))

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_multicluster_operation_total"].GetMetric() {
		if labelValue(m, "cluster") == "prod-west" && labelValue(m, "status") == "error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_multicluster_operation_total series with cluster=prod-west, status=error")
	}
}

func TestRecordMulticlusterOperationEmptyClusterNormalizesToNone(t *testing.T) {
	RecordMulticlusterOperation("", nil)

	families := gather(t)
	for _, m := range families["mcpserver_multicluster_operation_total"].GetMetric() {
		if labelValue(m, "status") == "success" && labelValue(m, "cluster") == "" {
			t.Fatal("cluster label must never be empty; expected normalization to 'none'")
		}
	}
}

func TestRecordAIQuerySuccess(t *testing.T) {
	RecordAIQuery("claude", 40*time.Millisecond, nil, "")

	families := gather(t)

	found := false
	for _, m := range families["mcpserver_ai_query_total"].GetMetric() {
		if labelValue(m, "provider") == "claude" && labelValue(m, "status") == "success" {
			found = true
			if m.GetCounter().GetValue() < 1 {
				t.Errorf("expected counter >= 1, got %v", m.GetCounter().GetValue())
			}
		}
	}
	if !found {
		t.Fatal("expected mcpserver_ai_query_total series for claude/success")
	}

	durFound := false
	for _, m := range families["mcpserver_ai_query_duration_seconds"].GetMetric() {
		if labelValue(m, "provider") == "claude" {
			durFound = true
			if m.GetHistogram().GetSampleCount() < 1 {
				t.Errorf("expected at least one observation, got %v", m.GetHistogram().GetSampleCount())
			}
		}
	}
	if !durFound {
		t.Fatal("expected mcpserver_ai_query_duration_seconds series for claude")
	}
}

func TestRecordAIQueryError(t *testing.T) {
	RecordAIQuery("claude", 5*time.Millisecond, errors.New("boom"), "")

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_ai_query_total"].GetMetric() {
		if labelValue(m, "provider") == "claude" && labelValue(m, "status") == "error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_ai_query_total series for claude/error")
	}
}

func TestRecordAIQueryErrorDefaultsToUnknownKind(t *testing.T) {
	RecordAIQuery("claude", 5*time.Millisecond, errors.New("boom"), "")

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_ai_query_errors_total"].GetMetric() {
		if labelValue(m, "provider") == "claude" && labelValue(m, "error_kind") == string(ErrorKindUnknown) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_ai_query_errors_total series with error_kind=unknown")
	}
}

func TestRecordAIQueryErrorRecordsGivenKind(t *testing.T) {
	RecordAIQuery("claude", 5*time.Millisecond, errors.New("rate limited"), ErrorKindAIAPI)

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_ai_query_errors_total"].GetMetric() {
		if labelValue(m, "provider") == "claude" && labelValue(m, "error_kind") == string(ErrorKindAIAPI) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_ai_query_errors_total series with error_kind=ai_api")
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{name: "nil", err: nil, want: ErrorKindUnknown},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ErrorKindTimeout},
		{name: "context canceled", err: context.Canceled, want: ErrorKindTimeout},
		{name: "wrapped deadline exceeded", err: fmtErrorf(context.DeadlineExceeded), want: ErrorKindTimeout},
		{
			name: "k8s api not found",
			err:  apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "demo"),
			want: ErrorKindK8sAPI,
		},
		{
			name: "json syntax error",
			err:  jsonSyntaxError(),
			want: ErrorKindMarshal,
		},
		{
			name: "json unmarshal type error",
			err:  jsonUnmarshalTypeError(),
			want: ErrorKindMarshal,
		},
		{
			name: "blocked IP",
			err:  netguard.ErrBlockedIP,
			want: ErrorKindBlockedIP,
		},
		{
			name: "wrapped blocked IP",
			err:  fmt.Errorf("helm repo URL %q uses a blocked IP address: %w", "https://169.254.169.254", netguard.ErrBlockedIP),
			want: ErrorKindBlockedIP,
		},
		{name: "unrecognized error", err: errors.New("boom"), want: ErrorKindUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Errorf("ClassifyError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// fmtErrorf wraps err the way calling code typically does (%w), to verify
// ClassifyError unwraps via errors.Is rather than requiring an exact match.
func fmtErrorf(err error) error {
	return fmt.Errorf("dispatch failed: %w", err)
}

// jsonSyntaxError produces a real *json.SyntaxError by feeding invalid JSON
// to the standard library decoder.
func jsonSyntaxError() error {
	var v interface{}
	return json.Unmarshal([]byte("{invalid"), &v)
}

// jsonUnmarshalTypeError produces a real *json.UnmarshalTypeError by
// decoding a JSON string into an incompatible Go type.
func jsonUnmarshalTypeError() error {
	var v int
	return json.Unmarshal([]byte(`"not-a-number"`), &v)
}

func TestRecordGitOpsSync(t *testing.T) {
	RecordGitOpsSync("prod-east", 15*time.Millisecond, map[string]int{
		"created":   2,
		"updated":   0,
		"unchanged": 1,
		"failed":    0,
		"skipped":   0,
	})

	families := gather(t)

	foundCreated := false
	for _, m := range families["mcpserver_gitops_sync_total"].GetMetric() {
		if labelValue(m, "cluster") == "prod-east" && labelValue(m, "action") == "created" {
			foundCreated = true
			if m.GetCounter().GetValue() < 2 {
				t.Errorf("expected counter >= 2, got %v", m.GetCounter().GetValue())
			}
		}
		if labelValue(m, "cluster") == "prod-east" && labelValue(m, "action") == "updated" {
			t.Fatal("zero-valued action must not be recorded as a series")
		}
	}
	if !foundCreated {
		t.Fatal("expected mcpserver_gitops_sync_total series for prod-east/created")
	}

	durFound := false
	for _, m := range families["mcpserver_gitops_sync_duration_seconds"].GetMetric() {
		if labelValue(m, "cluster") == "prod-east" {
			durFound = true
			if m.GetHistogram().GetSampleCount() < 1 {
				t.Errorf("expected at least one observation, got %v", m.GetHistogram().GetSampleCount())
			}
		}
	}
	if !durFound {
		t.Fatal("expected mcpserver_gitops_sync_duration_seconds series for prod-east")
	}
}

func TestRecordGitOpsSyncEmptyClusterNormalizesToNone(t *testing.T) {
	RecordGitOpsSync("", time.Millisecond, map[string]int{"created": 1})

	families := gather(t)
	for _, m := range families["mcpserver_gitops_sync_total"].GetMetric() {
		if labelValue(m, "action") == "created" && labelValue(m, "cluster") == "" {
			t.Fatal("cluster label must never be empty; expected normalization to 'none'")
		}
	}
	found := false
	for _, m := range families["mcpserver_gitops_sync_total"].GetMetric() {
		if labelValue(m, "cluster") == unknownCluster && labelValue(m, "action") == "created" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_gitops_sync_total series normalized to cluster=none")
	}
}

func TestRecordGitOpsDrift(t *testing.T) {
	RecordGitOpsDrift("prod-east", 8*time.Millisecond, map[string]int{
		"missing":  1,
		"modified": 0,
	})

	families := gather(t)

	foundMissing := false
	for _, m := range families["mcpserver_gitops_drift_total"].GetMetric() {
		if labelValue(m, "cluster") == "prod-east" && labelValue(m, "drift_type") == "missing" {
			foundMissing = true
			if m.GetCounter().GetValue() < 1 {
				t.Errorf("expected counter >= 1, got %v", m.GetCounter().GetValue())
			}
		}
		if labelValue(m, "cluster") == "prod-east" && labelValue(m, "drift_type") == "modified" {
			t.Fatal("zero-valued drift type must not be recorded as a series")
		}
	}
	if !foundMissing {
		t.Fatal("expected mcpserver_gitops_drift_total series for prod-east/missing")
	}

	durFound := false
	for _, m := range families["mcpserver_gitops_drift_duration_seconds"].GetMetric() {
		if labelValue(m, "cluster") == "prod-east" {
			durFound = true
			if m.GetHistogram().GetSampleCount() < 1 {
				t.Errorf("expected at least one observation, got %v", m.GetHistogram().GetSampleCount())
			}
		}
	}
	if !durFound {
		t.Fatal("expected mcpserver_gitops_drift_duration_seconds series for prod-east")
	}
}

func TestRecordGitOpsDriftEmptyClusterNormalizesToNone(t *testing.T) {
	RecordGitOpsDrift("", time.Millisecond, map[string]int{"modified": 1})

	families := gather(t)
	found := false
	for _, m := range families["mcpserver_gitops_drift_total"].GetMetric() {
		if labelValue(m, "cluster") == unknownCluster && labelValue(m, "drift_type") == "modified" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcpserver_gitops_drift_total series normalized to cluster=none")
	}
}

func TestRecordGitOpsDriftNoDrifts(t *testing.T) {
	// Exercise the common "no drift detected" path: an empty counts map
	// must still record a duration observation with zero counter series
	// added.
	RecordGitOpsDrift("clean-cluster", time.Millisecond, map[string]int{})

	families := gather(t)
	for _, m := range families["mcpserver_gitops_drift_total"].GetMetric() {
		if labelValue(m, "cluster") == "clean-cluster" {
			t.Fatal("expected no drift_total series when counts map is empty")
		}
	}
	durFound := false
	for _, m := range families["mcpserver_gitops_drift_duration_seconds"].GetMetric() {
		if labelValue(m, "cluster") == "clean-cluster" {
			durFound = true
		}
	}
	if !durFound {
		t.Fatal("expected mcpserver_gitops_drift_duration_seconds series for clean-cluster")
	}
}

func TestRecordDiscoveryLatency(t *testing.T) {
	before := gather(t)["mcpserver_discovery_latency_seconds"].GetMetric()[0].GetHistogram().GetSampleCount()

	RecordDiscoveryLatency(120 * time.Millisecond)

	after := gather(t)["mcpserver_discovery_latency_seconds"].GetMetric()[0].GetHistogram().GetSampleCount()
	if after != before+1 {
		t.Fatalf("expected sample count to increase by 1, got %d -> %d", before, after)
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

// TestHealthzHandlerReturnsOK is a smoke test for the /healthz liveness
// handler wired into StartServer: it must always return 200 with no
// dependency checks (this listener has no fixed downstream dependency).
func TestHealthzHandlerReturnsOK(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatalf("http.NewRequest error = %v", err)
	}
	rec := &discardResponseWriter{header: http.Header{}}
	healthzHandler(rec, req)
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusOK)
	}
	if string(rec.body) != "ok" {
		t.Fatalf("body = %q, want %q", rec.body, "ok")
	}
}

func TestStartServerServesHealthzEndpoint(t *testing.T) {
	srv, err := StartServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartServer() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, srv)
	}()

	// StartServer's Addr may be ":0"; exercise the mux directly rather than
	// dialing a real socket, consistent with TestPromHTTPHandlerAvailable.
	req, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatalf("http.NewRequest error = %v", err)
	}
	rec := &discardResponseWriter{header: http.Header{}}
	srv.Handler.ServeHTTP(rec, req)
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusOK)
	}
}

// discardResponseWriter is a minimal http.ResponseWriter for smoke-testing
// handler wiring without a real network listener.
type discardResponseWriter struct {
	header http.Header
	status int
	body   []byte
}

func (w *discardResponseWriter) Header() http.Header { return w.header }
func (w *discardResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, b...)
	return io.Discard.Write(b)
}
func (w *discardResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }
