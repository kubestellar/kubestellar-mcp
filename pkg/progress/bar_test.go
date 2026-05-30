package progress_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/progress"
)

// ─── Bar ─────────────────────────────────────────────────────────────────────

func TestBarSet_PercentAndDescription(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 10)
	bar.SetDescription("loading")
	bar.Set(5)

	out := buf.String()
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% in output, got: %q", out)
	}
	if !strings.Contains(out, "loading") {
		t.Errorf("expected description 'loading' in output, got: %q", out)
	}
}

func TestBarIncrement_Accumulates(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 10)
	bar.Increment(3)
	bar.Increment(4)

	out := buf.String()
	if !strings.Contains(out, "70%") {
		t.Errorf("expected 70%% in output after two increments, got: %q", out)
	}
}

func TestBarUpdate_SetsCurrentAndDesc(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 4)
	bar.Update(2, "half-done")

	out := buf.String()
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% in output, got: %q", out)
	}
	if !strings.Contains(out, "half-done") {
		t.Errorf("expected description in output, got: %q", out)
	}
}

func TestBarDone_EmitsNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 5)
	bar.Set(5)
	buf.Reset()
	bar.Done()

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Done() should end with newline, got: %q", out)
	}
	if !strings.Contains(out, "100%") {
		t.Errorf("Done() should render 100%%, got: %q", out)
	}
}

func TestBarFailed_EmitsNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 5)
	bar.Failed("something went wrong")

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Failed() should end with newline, got: %q", out)
	}
	if !strings.Contains(out, "something went wrong") {
		t.Errorf("Failed() should contain the failure message, got: %q", out)
	}
}

func TestBarSetWidth_AffectsOutput(t *testing.T) {
	bufNarrow := &bytes.Buffer{}
	barNarrow := progress.New(bufNarrow, 10).SetWidth(10)
	barNarrow.Set(5)

	bufWide := &bytes.Buffer{}
	barWide := progress.New(bufWide, 10).SetWidth(80)
	barWide.Set(5)

	if bufNarrow.Len() >= bufWide.Len() {
		t.Errorf("wider bar should produce longer output; narrow=%d wide=%d",
			bufNarrow.Len(), bufWide.Len())
	}
}

func TestBarOverHundredPercent_Clamped(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 10)
	bar.Set(20) // over total — should be clamped
	out := buf.String()
	if strings.Contains(out, "200%") || strings.Contains(out, "150%") {
		t.Errorf("percent should be clamped at 100%%, got: %q", out)
	}
}

func TestBarZeroTotal_DoesNotPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	bar := progress.New(buf, 0)
	bar.Set(5) // must not divide by zero
}

// ─── LiveBar ─────────────────────────────────────────────────────────────────

func TestLiveBarRender_ShowsLabel(t *testing.T) {
	buf := &bytes.Buffer{}
	lb := progress.NewLiveBar(buf)
	lb.Render(progress.Status{
		Label:   "my-op",
		Percent: 42,
	})

	out := buf.String()
	if !strings.Contains(out, "my-op") {
		t.Errorf("expected label in output, got: %q", out)
	}
	if !strings.Contains(out, "42%") {
		t.Errorf("expected 42%% in output, got: %q", out)
	}
}

func TestLiveBarRender_CompleteEmitsNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	lb := progress.NewLiveBar(buf)
	lb.Render(progress.Status{Label: "done", Percent: 100, Complete: true})
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Complete render should end with newline, got: %q", out)
	}
}

func TestLiveBarRender_FailedEmitsNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	lb := progress.NewLiveBar(buf)
	lb.Render(progress.Status{Label: "op", Failed: true, FailReason: "disk full"})
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Failed render should end with newline, got: %q", out)
	}
	if !strings.Contains(out, "disk full") {
		t.Errorf("expected FailReason in output, got: %q", out)
	}
}

func TestLiveBarRender_DeduplicatesSamePercent(t *testing.T) {
	buf := &bytes.Buffer{}
	lb := progress.NewLiveBar(buf)
	lb.Render(progress.Status{Label: "op", Percent: 50})
	first := buf.Len()
	lb.Render(progress.Status{Label: "op", Percent: 50}) // identical — should be no-op
	second := buf.Len()
	if second != first {
		t.Errorf("second Render with same percent should not write; buf grew from %d to %d", first, second)
	}
}

func TestLiveBarSetWidth_NarrowProducesShorterBar(t *testing.T) {
	bufNarrow := &bytes.Buffer{}
	progress.NewLiveBar(bufNarrow).SetWidth(5).Render(progress.Status{Label: "x", Percent: 50})

	bufWide := &bytes.Buffer{}
	progress.NewLiveBar(bufWide).SetWidth(80).Render(progress.Status{Label: "x", Percent: 50})

	if bufNarrow.Len() >= bufWide.Len() {
		t.Errorf("narrow bar should produce shorter output; narrow=%d wide=%d",
			bufNarrow.Len(), bufWide.Len())
	}
}

func TestLiveBarRender_CountsShownWhenTotalNonZero(t *testing.T) {
	buf := &bytes.Buffer{}
	lb := progress.NewLiveBar(buf)
	lb.Render(progress.Status{
		Label:   "import",
		Percent: 60,
		Done:    6,
		Total:   10,
		Current: "item6",
	})
	out := buf.String()
	if !strings.Contains(out, "6/10") {
		t.Errorf("expected count '6/10' in output, got: %q", out)
	}
}

// ─── Convenience wrappers ────────────────────────────────────────────────────

func TestRenderSimple_WritesToBuffer(t *testing.T) {
	buf := &bytes.Buffer{}
	progress.RenderSimple(buf, "step", 30, 3, 10, "processing")
	out := buf.String()
	if !strings.Contains(out, "step") {
		t.Errorf("expected label in RenderSimple output, got: %q", out)
	}
	if !strings.Contains(out, "30%") {
		t.Errorf("expected 30%% in RenderSimple output, got: %q", out)
	}
}

func TestRenderComplete_Shows100AndNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	progress.RenderComplete(buf, "done", 5)
	out := buf.String()
	if !strings.Contains(out, "100%") {
		t.Errorf("expected 100%% in RenderComplete output, got: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("RenderComplete should end with newline, got: %q", out)
	}
}

func TestRenderFailed_ShowsReasonAndNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	progress.RenderFailed(buf, "install", "timeout")
	out := buf.String()
	if !strings.Contains(out, "timeout") {
		t.Errorf("expected reason in RenderFailed output, got: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("RenderFailed should end with newline, got: %q", out)
	}
}

// ─── MultiBar ────────────────────────────────────────────────────────────────

func TestMultiBarAddAndUpdate(t *testing.T) {
	buf := &bytes.Buffer{}
	mb := progress.NewMultiBar(buf)
	mb.AddBar("alpha", 10)
	mb.AddBar("beta", 10)
	mb.Update("alpha", 5, "halfway")

	out := buf.String()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected 'alpha' bar name in output, got: %q", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% in multi-bar output, got: %q", out)
	}
}

func TestMultiBarSetStatus_DoneIcon(t *testing.T) {
	buf := &bytes.Buffer{}
	mb := progress.NewMultiBar(buf)
	mb.AddBar("job", 10)
	mb.SetStatus("job", "done")
	out := buf.String()
	if !strings.Contains(out, "✅") {
		t.Errorf("expected done icon in output, got: %q", out)
	}
}

func TestMultiBarDone_DoesNotPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	mb := progress.NewMultiBar(buf)
	mb.AddBar("x", 1)
	mb.Done()
}
