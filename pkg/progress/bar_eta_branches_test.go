package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestBarRenderShowsMinuteETA covers the uncovered `remaining > time.Minute`
// branch inside Bar.render (bar.go:120). Existing renderer tests set
// current on a bar constructed at "now", so remaining ETA is always
// sub-second and only the else-branch (seconds) is exercised. Backdating
// startTime forces the minute-formatted branch and asserts on the observable
// "ETA: 1m..." token that a user would see in the CLI.
func TestBarRenderShowsMinuteETA(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, 100)
	// 10 seconds elapsed at 10% => remaining = elapsed * (100-10)/10 = 90s > 1m
	bar.startTime = time.Now().Add(-10 * time.Second)
	bar.current = 10

	bar.render()

	output := buf.String()
	if !strings.Contains(output, "ETA: 1m") {
		t.Fatalf("render() output = %q, want it to contain minute-formatted ETA", output)
	}
	// Regression guard against reintroducing a seconds-only formatter.
	if strings.Contains(output, "ETA: 90s") {
		t.Fatalf("render() output = %q, want minutes, not raw seconds", output)
	}
}

// TestBarRenderOmitsETAWhenComplete covers the `percent < 100` guard on the
// ETA arm: when the bar is exactly at 100%, no ETA suffix should render.
// This branch is otherwise easy to regress into "ETA: 0s" noise when the
// remaining-time math is refactored.
func TestBarRenderOmitsETAWhenComplete(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, 10)
	bar.startTime = time.Now().Add(-5 * time.Second)
	bar.current = 10

	bar.render()

	output := buf.String()
	if strings.Contains(output, "ETA") {
		t.Fatalf("render() output = %q, want no ETA when complete", output)
	}
	if !strings.Contains(output, "100%") {
		t.Fatalf("render() output = %q, want 100%%", output)
	}
}

// TestBarRenderOmitsETAWhenCurrentIsZero covers the `b.current > 0` guard on
// the ETA arm. Immediately after construction the bar has current=0 and the
// division-by-zero-guard should prevent an ETA token from rendering.
func TestBarRenderOmitsETAWhenCurrentIsZero(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, 10)
	bar.startTime = time.Now().Add(-5 * time.Second)
	// current stays at zero

	bar.render()

	output := buf.String()
	if strings.Contains(output, "ETA") {
		t.Fatalf("render() output = %q, want no ETA when current==0", output)
	}
	if !strings.Contains(output, "0%") {
		t.Fatalf("render() output = %q, want 0%%", output)
	}
}

// TestLiveBarRenderClampsNegativePercent covers the `pct < 0 { pct = 0 }`
// clamp inside LiveBar.Render (bar.go:296-298). Existing tests only reach
// the overflow (pct > 100) clamp. Sending a Status with Percent = -50
// exercises the lower clamp: bar text must be all "-" (0 filled cells),
// and the printed percent must be "  0%".
func TestLiveBarRenderClampsNegativePercent(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	changed := bar.Render(Status{
		Label:   "sync",
		Percent: -50,
		Done:    0,
		Total:   4,
	})

	if !changed {
		t.Fatal("Render returned false, want true (lastPct went from -1 to -50)")
	}
	output := buf.String()
	if !strings.Contains(output, "  0%") {
		t.Fatalf("Render output = %q, want it to contain '  0%%'", output)
	}
	// No # characters — filled = pct * width / 100 with pct clamped to 0.
	if strings.Contains(output, "#") {
		t.Fatalf("Render output = %q, want zero filled cells", output)
	}
}

// TestLiveBarRenderSkipsWhenPercentUnchanged covers the early-return branch
// at the top of LiveBar.Render (bar.go:279): a second Render with the same
// Percent and neither Complete nor Failed must return false and write
// nothing new. This is the anti-flicker guard that keeps LiveBar cheap in
// tight polling loops.
func TestLiveBarRenderSkipsWhenPercentUnchanged(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	if changed := bar.Render(Status{Label: "sync", Percent: 42, Total: 10, Done: 4}); !changed {
		t.Fatal("first Render returned false, want true")
	}
	firstLen := buf.Len()

	if changed := bar.Render(Status{Label: "sync", Percent: 42, Total: 10, Done: 4}); changed {
		t.Fatal("second Render at same percent returned true, want false")
	}
	if buf.Len() != firstLen {
		t.Fatalf("second Render wrote additional bytes (before=%d, after=%d)", firstLen, buf.Len())
	}
}

// TestLiveBarRenderFailedShowsReason covers the failure-branch prose swap at
// bar.go:317-319 — when Failed is set with a FailReason, the rendered suffix
// must be the fail reason (not the Current field), and a trailing newline
// must be emitted so subsequent shell output starts on a fresh line.
func TestLiveBarRenderFailedShowsReason(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	bar.Render(Status{
		Label:      "sync",
		Percent:    30,
		Current:    "should-not-appear",
		Failed:     true,
		FailReason: "connection refused",
	})

	output := buf.String()
	if !strings.Contains(output, "connection refused") {
		t.Fatalf("Render output = %q, want failure reason", output)
	}
	if strings.Contains(output, "should-not-appear") {
		t.Fatalf("Render output = %q, must not contain Current when Failed", output)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("Render output = %q, want trailing newline for Failed status", output)
	}
}
