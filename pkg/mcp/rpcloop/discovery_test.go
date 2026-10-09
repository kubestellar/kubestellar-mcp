package rpcloop

import (
	"testing"
	"time"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

func sampleCount(t *testing.T) uint64 {
	t.Helper()
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather() error = %v", err)
	}
	for _, f := range families {
		if f.GetName() == "mcpserver_discovery_latency_seconds" {
			ms := f.GetMetric()
			if len(ms) != 1 {
				t.Fatalf("expected exactly one mcpserver_discovery_latency_seconds series, got %d", len(ms))
			}
			return ms[0].GetHistogram().GetSampleCount()
		}
	}
	t.Fatal("mcpserver_discovery_latency_seconds metric family not found")
	return 0
}

func TestDiscoveryTimerRecordsOnFirstToolsList(t *testing.T) {
	before := sampleCount(t)

	var dt DiscoveryTimer
	dt.Initialize()
	time.Sleep(time.Millisecond)
	dt.ToolsList()

	after := sampleCount(t)
	if after != before+1 {
		t.Fatalf("expected one observation after Initialize+ToolsList, got %d -> %d", before, after)
	}
}

func TestDiscoveryTimerOnlyRecordsOnce(t *testing.T) {
	before := sampleCount(t)

	var dt DiscoveryTimer
	dt.Initialize()
	dt.ToolsList()
	dt.ToolsList() // a client re-listing tools mid-session must not double-count
	dt.ToolsList()

	after := sampleCount(t)
	if after != before+1 {
		t.Fatalf("expected exactly one observation despite 3 ToolsList calls, got %d -> %d", before, after)
	}
}

func TestDiscoveryTimerToolsListWithoutInitializeIsNoop(t *testing.T) {
	before := sampleCount(t)

	var dt DiscoveryTimer
	dt.ToolsList()

	after := sampleCount(t)
	if after != before {
		t.Fatalf("expected no observation without a preceding Initialize, got %d -> %d", before, after)
	}
}

func TestDiscoveryTimerReInitializeResetsRecordedFlag(t *testing.T) {
	before := sampleCount(t)

	var dt DiscoveryTimer
	dt.Initialize()
	dt.ToolsList()
	dt.Initialize() // a second handshake on the same connection
	dt.ToolsList()

	after := sampleCount(t)
	if after != before+2 {
		t.Fatalf("expected two observations across two Initialize/ToolsList pairs, got %d -> %d", before, after)
	}
}
