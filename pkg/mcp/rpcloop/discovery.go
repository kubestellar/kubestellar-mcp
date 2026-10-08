package rpcloop

import (
	"sync"
	"time"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

// DiscoveryTimer measures SLO 2 (Cluster Discovery Latency, docs/slo.md):
// the time from a connection's "initialize" request receipt to its first
// "tools/list" response. Both kubestellar-ops and kubestellar-deploy share
// this type so the SLI is measured identically rather than each server
// approximating it from the general-purpose ToolDurationSeconds metric
// (see docs/slo.md's "Note on mcpserver_tool_duration_seconds vs. SLO 2").
//
// The stdio transport serves one connection per process and processes one
// request at a time, so DiscoveryTimer's internal mutex only guards against
// a server being adapted to a concurrent transport later - it is not
// exercised under contention in the current single-threaded read loop.
type DiscoveryTimer struct {
	mu       sync.Mutex
	start    time.Time
	recorded bool
}

// Initialize marks the arrival of an "initialize" request as the start of
// discovery for this connection. A later call resets the timer: the most
// recent handshake is what the next ToolsList call measures against.
func (d *DiscoveryTimer) Initialize() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.start = time.Now()
	d.recorded = false
}

// ToolsList records discovery latency the first time it is called after
// Initialize. Subsequent "tools/list" calls in the same session (a client
// re-listing tools mid-session) are no-ops, since SLO 2 only measures the
// initial handshake; a call with no preceding Initialize is also a no-op.
func (d *DiscoveryTimer) ToolsList() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.recorded || d.start.IsZero() {
		return
	}
	d.recorded = true
	metrics.RecordDiscoveryLatency(time.Since(d.start))
}
