package server

import "go.opentelemetry.io/otel"

// tracerName identifies this package's instrumentation scope in exported
// trace data.
const tracerName = "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"

// tracer provides spans for the MCP tool-dispatch request path.
//
// No TracerProvider is registered by this package, so otel.Tracer returns
// the default no-op provider's tracer: span creation and attribute
// recording are effectively free (no allocation beyond a stack-local no-op
// span) and no trace data is collected, held in memory, or sent anywhere.
// An operator who wants real traces must register a TracerProvider (e.g.
// via otel.SetTracerProvider) from an explicitly configured exporter in
// their own wiring; this package never does so itself and never sends
// telemetry off-box.
var tracer = otel.Tracer(tracerName)
