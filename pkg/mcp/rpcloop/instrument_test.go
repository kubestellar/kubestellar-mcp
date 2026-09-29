package rpcloop

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
)

func TestInstrumentToolCallUnknownToolShortCircuits(t *testing.T) {
	dispatchCalled := false
	outcome := InstrumentToolCall(context.Background(), "missing_tool", "", func(ctx context.Context) ToolCallOutcome {
		dispatchCalled = true
		return ToolCallOutcome{Found: false}
	})

	assert.True(t, dispatchCalled, "dispatch must still run so it can perform the lookup")
	assert.False(t, outcome.Found)
	assert.False(t, outcome.IsError)
}

func TestInstrumentToolCallSuccessPath(t *testing.T) {
	before := testutil.ToFloat64(metrics.ToolCallsTotal.WithLabelValues("get_clusters", "test-cluster", "success"))

	var gotCtx context.Context
	outcome := InstrumentToolCall(context.Background(), "get_clusters", "test-cluster", func(ctx context.Context) ToolCallOutcome {
		gotCtx = ctx
		return ToolCallOutcome{Found: true, IsError: false, Duration: 5 * time.Millisecond}
	})

	require.True(t, outcome.Found)
	assert.False(t, outcome.IsError)
	assert.NotNil(t, gotCtx, "dispatch must receive the traced ctx returned by tracer.Start")

	after := testutil.ToFloat64(metrics.ToolCallsTotal.WithLabelValues("get_clusters", "test-cluster", "success"))
	assert.Equal(t, before+1, after, "a successful call must record exactly one success counter increment")
}

func TestInstrumentToolCallErrorPath(t *testing.T) {
	beforeCalls := testutil.ToFloat64(metrics.ToolCallsTotal.WithLabelValues("deploy_app", "none", "error"))
	beforeErrors := testutil.ToFloat64(metrics.ToolErrorsTotal.WithLabelValues("deploy_app", "none", string(metrics.ErrorKindK8sAPI)))

	outcome := InstrumentToolCall(context.Background(), "deploy_app", "", func(ctx context.Context) ToolCallOutcome {
		return ToolCallOutcome{Found: true, IsError: true, ErrKind: metrics.ErrorKindK8sAPI, Duration: time.Millisecond}
	})

	require.True(t, outcome.Found)
	assert.True(t, outcome.IsError)
	assert.Equal(t, metrics.ErrorKindK8sAPI, outcome.ErrKind)

	afterCalls := testutil.ToFloat64(metrics.ToolCallsTotal.WithLabelValues("deploy_app", "none", "error"))
	afterErrors := testutil.ToFloat64(metrics.ToolErrorsTotal.WithLabelValues("deploy_app", "none", string(metrics.ErrorKindK8sAPI)))
	assert.Equal(t, beforeCalls+1, afterCalls, "an error outcome must still record one tool-call counter increment")
	assert.Equal(t, beforeErrors+1, afterErrors, "an error outcome must record exactly one error counter increment")
}

func TestInstrumentToolCallEmptyClusterDoesNotPanic(t *testing.T) {
	// Regression guard: passing cluster="" must not panic and must still
	// dispatch/record normally (the "none" label normalization happens
	// inside metrics.RecordToolCall, not here).
	outcome := InstrumentToolCall(context.Background(), "list_pods", "", func(ctx context.Context) ToolCallOutcome {
		return ToolCallOutcome{Found: true, IsError: false}
	})
	assert.True(t, outcome.Found)
}

func TestInstrumentToolCallDefaultErrKindNormalizesToUnknown(t *testing.T) {
	before := testutil.ToFloat64(metrics.ToolErrorsTotal.WithLabelValues("scale_workload", "none", string(metrics.ErrorKindUnknown)))

	outcome := InstrumentToolCall(context.Background(), "scale_workload", "", func(ctx context.Context) ToolCallOutcome {
		// ErrKind intentionally left as the zero value; RecordToolCall must
		// normalize it to ErrorKindUnknown, matching both pre-existing
		// servers' behavior for unclassified errors.
		return ToolCallOutcome{Found: true, IsError: true}
	})

	require.True(t, outcome.Found)
	after := testutil.ToFloat64(metrics.ToolErrorsTotal.WithLabelValues("scale_workload", "none", string(metrics.ErrorKindUnknown)))
	assert.Equal(t, before+1, after)
}
