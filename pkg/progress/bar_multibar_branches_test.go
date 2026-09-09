package progress

import (
	"bytes"
	"strings"
	"testing"
)

// TestMultiBarSetStatusDoneRendersCheckIcon guards the "done" arm of the
// three-way status switch inside MultiBar.render. The existing
// TestMultiBarSetStatusUpdatesStatus test only exercises the "failed" arm;
// the default "⏳" and "❌" icons are covered, but the "✅" branch is
// unexercised. This keeps future refactors from silently swapping the
// icon or collapsing the switch.
func TestMultiBarSetStatusDoneRendersCheckIcon(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)
	multi.AddBar("alpha", barTotal)

	multi.SetStatus("alpha", "done")

	entry := multi.bars[0]
	if entry.Status != "done" {
		t.Fatalf("status = %q, want %q", entry.Status, "done")
	}
	if !strings.Contains(buf.String(), "✅") {
		t.Fatalf("SetStatus(done) output = %q, want ✅ icon", buf.String())
	}
	if strings.Contains(buf.String(), "❌") || strings.Contains(buf.String(), "⏳") {
		t.Fatalf("SetStatus(done) output = %q, must not contain failed/pending icon", buf.String())
	}
}

// TestMultiBarRenderClampsFilledWhenOverflowingBarWidth guards the
// `if filled > width { filled = width }` clamp inside MultiBar.render.
// The clamp fires only when Current is *greater than* Total — a state
// callers can reach with Update() when a task reports more items than
// declared. Without the clamp, strings.Repeat("-", width-filled) would
// receive a negative count and panic. This test drives Current well
// past Total and asserts the rendered bar is exactly `width` '#'
// characters (no dashes) and that the render did not panic.
func TestMultiBarRenderClampsFilledWhenOverflowingBarWidth(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)
	multi.AddBar("alpha", barTotal)

	// Current > Total → percent > 100 → filled = int(20 * 200/100) = 40,
	// which must be clamped down to width=20 before strings.Repeat runs.
	multi.Update("alpha", barTotal*2, "overflow")

	out := buf.String()

	// The bar body is delimited by [ and ]. Use the LAST render (from
	// Update) — AddBar also emits an initial render. Extract that final
	// section and assert length == 20 and every rune is '#'.
	start := strings.LastIndex(out, "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("could not locate [bar] section in output %q", out)
	}
	body := out[start+1 : end]
	if len(body) != 20 {
		t.Fatalf("rendered bar body length = %d, want 20 (clamp failed): %q", len(body), body)
	}
	if strings.ContainsRune(body, '-') {
		t.Fatalf("rendered bar body still contains '-' after clamp: %q", body)
	}
	for _, r := range body {
		if r != '#' {
			t.Fatalf("rendered bar body has non-'#' rune %q: %q", r, body)
		}
	}
}
