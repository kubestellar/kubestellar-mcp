package progress

import (
	"bytes"
	"strings"
	"testing"
)

// TestLiveBarRenderClampsNegativePercent covers the `pct < 0 { pct = 0 }`
// clamp inside LiveBar.Render. Sending a Status with Percent = -50
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

// TestLiveBarRenderClampsOverflowPercent covers the `pct > 100 { pct = 100 }`
// clamp inside LiveBar.Render. The Bar/MultiBar tests that previously reached
// this branch were removed with the dead Bar/MultiBar code in #1122, leaving
// the overflow clamp as the package's only uncovered statement. Sending a
// Status with Percent = 150 exercises it: every bar cell must be filled and
// the printed percent must be "100%".
func TestLiveBarRenderClampsOverflowPercent(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	changed := bar.Render(Status{
		Label:   "sync",
		Percent: 150,
		Done:    4,
		Total:   4,
	})

	if !changed {
		t.Fatal("Render returned false, want true (lastPct went from -1 to 150)")
	}
	output := buf.String()
	if !strings.Contains(output, "100%") {
		t.Fatalf("Render output = %q, want it to contain '100%%'", output)
	}
	// All cells filled — filled = pct * width / 100 with pct clamped to 100.
	if strings.Contains(output, "-") {
		t.Fatalf("Render output = %q, want all filled cells", output)
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
