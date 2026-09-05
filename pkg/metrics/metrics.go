// Package metrics provides bounded Prometheus metrics for the MCP server's
// tool-dispatch request path (see pkg/mcp/server).
//
// Metrics are always recorded in-process using closed, bounded label sets
// (tool names come from the fixed tool registry; error kinds are a short
// enum; cluster names are capped by the discovered cluster set). No raw
// error messages or unbounded values are ever used as label values.
//
// The /metrics HTTP endpoint is only served when an operator explicitly
// configures --metrics-addr. When no address is configured, no listener is
// started and no data is exposed - metrics are collected in memory only.
package metrics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ErrorKind is a closed enum used to classify tool-call failures without
// leaking raw error text into metric labels.
type ErrorKind string

// Recognized error kinds. Keep this list short and closed - never derive an
// ErrorKind from a raw error string.
const (
	ErrorKindMarshal ErrorKind = "marshal"
	ErrorKindK8sAPI  ErrorKind = "k8s_api"
	ErrorKindTimeout ErrorKind = "timeout"
	ErrorKindUnknown ErrorKind = "unknown"
)

// unknownCluster is the label value used when a tool call is not scoped to
// a specific cluster, keeping the "cluster" label bounded.
const unknownCluster = "none"

// Registry is the Prometheus registry used for MCP server metrics. It is
// intentionally separate from prometheus.DefaultRegisterer so that this
// package can be wired in without pulling in default Go-runtime collectors.
var Registry = prometheus.NewRegistry()

var (
	// ToolCallsTotal counts completed tool invocations by tool, cluster, and
	// outcome status.
	ToolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcpserver_tool_calls_total",
		Help: "Total number of MCP tool invocations.",
	}, []string{"tool", "cluster", "status"})

	// ToolDurationSeconds observes end-to-end latency per tool call.
	ToolDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcpserver_tool_duration_seconds",
		Help:    "End-to-end latency of MCP tool calls, in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tool", "cluster"})

	// ToolErrorsTotal counts tool call errors, classified by a closed
	// error-kind enum (never raw error messages).
	ToolErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcpserver_tool_errors_total",
		Help: "Total number of MCP tool call errors, by error kind.",
	}, []string{"tool", "cluster", "error_kind"})

	// ActiveClusters reports the number of clusters reachable in the most
	// recent multi-cluster discovery.
	ActiveClusters = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcpserver_active_clusters",
		Help: "Number of clusters reachable in the most recent multi-cluster discovery.",
	})
)

func init() {
	Registry.MustRegister(ToolCallsTotal, ToolDurationSeconds, ToolErrorsTotal, ActiveClusters)
}

// RecordToolCall records a completed tool invocation. cluster may be empty
// for tools that are not scoped to a single cluster; it is normalized to
// "none" to keep the label bounded. When isError is true and errKind is
// empty, ErrorKindUnknown is recorded.
func RecordToolCall(tool, cluster string, duration time.Duration, isError bool, errKind ErrorKind) {
	if cluster == "" {
		cluster = unknownCluster
	}

	status := "success"
	if isError {
		status = "error"
		if errKind == "" {
			errKind = ErrorKindUnknown
		}
		ToolErrorsTotal.WithLabelValues(tool, cluster, string(errKind)).Inc()
	}

	ToolCallsTotal.WithLabelValues(tool, cluster, status).Inc()
	ToolDurationSeconds.WithLabelValues(tool, cluster).Observe(duration.Seconds())
}

// SetActiveClusters updates the active-cluster gauge.
func SetActiveClusters(n int) {
	ActiveClusters.Set(float64(n))
}

// StartServer starts an HTTP server exposing the /metrics endpoint on addr
// and returns it so the caller can shut it down gracefully. It must only be
// called when an operator has explicitly configured a metrics address.
func StartServer(addr string) (*http.Server, error) {
	if addr == "" {
		return nil, errors.New("metrics: addr must not be empty")
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", healthzHandler)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	// Surface immediate bind failures (e.g. address already in use) to the
	// caller instead of only logging them from the goroutine.
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return nil, err
		}
	case <-time.After(50 * time.Millisecond):
	}

	return srv, nil
}

// healthzHandler answers a plain liveness check: 200 OK if this process is
// up enough to handle HTTP requests. It intentionally does not check any
// downstream dependency (the metrics listener has no fixed downstream to
// probe) - it is a liveness signal only, not a readiness/dependency check.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Shutdown gracefully stops a metrics server started by StartServer.
func Shutdown(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}
