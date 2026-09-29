package rpcloop

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

// tracerName identifies this package's instrumentation scope in exported
// trace data.
const tracerName = "github.com/kubestellar/kubestellar-mcp/pkg/mcp/rpcloop"

// tracer provides the "mcp.tool.call" span for InstrumentToolCall.
//
// No TracerProvider is registered by this package, so otel.Tracer returns
// the default no-op provider's tracer: span creation and attribute
// recording are effectively free (no allocation beyond a stack-local no-op
// span) and no trace data is collected, held in memory, or sent anywhere.
// An operator who wants real traces must register a TracerProvider (e.g.
// via otel.SetTracerProvider) from an explicitly configured exporter in
// their own wiring; this package never does so itself.
var tracer = otel.Tracer(tracerName)

// ToolCallOutcome is what a single tool dispatch reports back to
// InstrumentToolCall, so it can record consistent tracing, metrics, and
// structured logging regardless of each MCP server's own ToolDef/result
// shape. Found=false means the requested tool name was not registered; the
// remaining fields are then ignored (no metrics/log line is recorded for an
// unknown tool, matching both pre-existing servers' behavior).
type ToolCallOutcome struct {
	Found    bool
	IsError  bool
	ErrKind  metrics.ErrorKind
	Duration time.Duration
}

// InstrumentToolCall wraps a single tool dispatch with the span/timing/
// metrics/structured-logging cross-cutting concerns that were previously
// duplicated - with comments literally citing each other - between
// pkg/mcp/server.handleToolsCall and pkg/deploy/mcp.handleToolCall (see
// kubestellar-mcp#1017).
//
// dispatch performs the actual tool lookup and invocation using the traced
// ctx it is handed, and reports the result back via ToolCallOutcome;
// InstrumentToolCall never sees the tool's arguments or return value,
// only the reported outcome, so it stays agnostic to each server's
// differing ToolDef/handler signature (reconciling those shapes is a
// separate follow-up to #1017, not part of this package).
//
// cluster is an optional bounded label (see each server's own
// cluster-name-validation logic); pass "" when no single-cluster scope
// applies to the call. It is only ever used as a span attribute / metrics
// label here - InstrumentToolCall performs no validation of its own, so
// callers remain responsible for bounding it before calling in.
func InstrumentToolCall(ctx context.Context, toolName, cluster string, dispatch func(ctx context.Context) ToolCallOutcome) ToolCallOutcome {
	ctx, span := tracer.Start(ctx, "mcp.tool.call", trace.WithAttributes(
		attribute.String("tool.name", toolName),
	))
	defer span.End()

	if cluster != "" {
		span.SetAttributes(attribute.String("k8s.cluster.name", cluster))
	}

	outcome := dispatch(ctx)
	if !outcome.Found {
		span.SetStatus(codes.Error, "unknown tool")
		return outcome
	}

	metrics.RecordToolCall(toolName, cluster, outcome.Duration, outcome.IsError, outcome.ErrKind)

	if outcome.IsError {
		span.SetStatus(codes.Error, "tool call returned an error result")
		klog.ErrorS(nil, "tool call failed", "tool", toolName, "cluster", cluster, "duration", outcome.Duration)
	} else {
		klog.V(2).InfoS("tool call succeeded", "tool", toolName, "cluster", cluster, "duration", outcome.Duration)
	}

	return outcome
}
