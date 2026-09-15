package mcp

import "go.opentelemetry.io/otel"

// tracerName identifies this package's instrumentation scope in exported
// trace data.
const tracerName = "github.com/kubestellar/kubestellar-mcp/pkg/deploy/mcp"

// tracer provides spans for the kubestellar-deploy tool-dispatch request
// path (see handleToolCall in server.go).
//
// No TracerProvider is registered by this package, so otel.Tracer returns
// the default no-op provider's tracer: span creation and attribute
// recording are effectively free (no allocation beyond a stack-local no-op
// span) and no trace data is collected, held in memory, or sent anywhere.
// An operator who wants real traces must register a TracerProvider (e.g.
// via otel.SetTracerProvider) from an explicitly configured exporter in
// their own wiring; this package never does so itself and never sends
// telemetry off-box. This mirrors the sibling kubestellar-mcp server's
// pkg/mcp/server/tracing.go.
var tracer = otel.Tracer(tracerName)
