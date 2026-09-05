package upgrade

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunWatchLoopWithTicker_ExitsImmediatelyWhenContextPreCancelled covers
// the OUTER `case <-ctx.Done()` arm at the top of the for loop in
// runWatchLoopWithTicker (watch.go:141-143). Under normal operation the
// outer select has a `default:` branch that runs on every iteration, so a
// live ctx-cancel is virtually always caught by the INNER select. The
// outer arm only wins when the context is already cancelled at the moment
// the loop re-enters the top of the for — i.e. when the caller decided to
// abort before the watch even started. Prior tests exercised the
// mid-flight cancel path (inner ctx.Done) and the error-continue path but
// never the pre-cancel path, so a regression that returned a non-nil
// error or dropped the "Stopped watching." breadcrumb on this fast-exit
// branch would have shipped silently.
func TestRunWatchLoopWithTicker_ExitsImmediatelyWhenContextPreCancelled(t *testing.T) {
	dynClient := newFakeDynamicClient(newClusterVersion("4.18.30", "Cluster version is 4.18.30"))
	renderer := &recordingRenderer{}
	// Ticks channel is intentionally never sent to — if the outer ctx.Done
	// arm regresses to selecting default first, the loop would fall
	// through into the inner select and block on this empty channel.
	ticks := make(chan time.Time)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runWatchLoopWithTicker(ctx, dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err, "pre-cancelled context is a clean exit, not an error")
	assert.Contains(t, out.String(), "Stopped watching.",
		"pre-cancelled watch must still emit the Stopped-watching breadcrumb")
	assert.Empty(t, errOut.String(),
		"pre-cancelled fast-exit path must not touch errOut")
	assert.Equal(t, 0, renderer.count(),
		"pre-cancelled fast-exit path must not call bar.Render — no status was fetched")
}

