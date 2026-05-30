<<<<<<< HEAD
package progress
=======
package progress_test
>>>>>>> 8286331 (test: add unit tests for pkg/progress/bar.go)

import (
	"bytes"
	"strings"
	"testing"
<<<<<<< HEAD
)

const (
	defaultBarWidth     = 40
	defaultLiveBarWidth = 50
	updatedBarWidth     = 24
	updatedLiveBarWidth = 32
	barTotal            = 10
	barSetCurrent       = 3
	barIncrementBy      = 2
	renderCurrent       = 5
	renderPercent       = 50
	overflowCurrent     = 15
	completePercent     = 100
	livePercent         = 25
	liveDone            = 1
	liveTotal           = 4
	helperDone          = 2
	helperTotal         = 8
)

func TestNewBarDefaults(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)

	if bar.writer != &buf {
		t.Fatalf("writer = %v, want %v", bar.writer, &buf)
	}
	if bar.total != barTotal {
		t.Fatalf("total = %d, want %d", bar.total, barTotal)
	}
	if bar.current != 0 {
		t.Fatalf("current = %d, want 0", bar.current)
	}
	if bar.width != defaultBarWidth {
		t.Fatalf("width = %d, want %d", bar.width, defaultBarWidth)
	}
	if bar.done {
		t.Fatal("done = true, want false")
	}
	if bar.startTime.IsZero() {
		t.Fatal("startTime was not initialized")
	}
}

func TestBarSettersAreChainable(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)

	if got := bar.SetWidth(updatedBarWidth); got != bar {
		t.Fatal("SetWidth() did not return the bar instance")
	}
	if got := bar.SetDescription("loading"); got != bar {
		t.Fatal("SetDescription() did not return the bar instance")
	}
	if bar.width != updatedBarWidth {
		t.Fatalf("width = %d, want %d", bar.width, updatedBarWidth)
	}
	if bar.description != "loading" {
		t.Fatalf("description = %q, want %q", bar.description, "loading")
	}
}

func TestBarSetAndIncrement(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)

	bar.Set(barSetCurrent)
	if bar.current != barSetCurrent {
		t.Fatalf("current after Set() = %d, want %d", bar.current, barSetCurrent)
	}

	bar.Increment(barIncrementBy)
	wantCurrent := barSetCurrent + barIncrementBy
	if bar.current != wantCurrent {
		t.Fatalf("current after Increment() = %d, want %d", bar.current, wantCurrent)
	}
}

func TestBarDoneSetsTotalAndDone(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)
	bar.Set(barSetCurrent)

	bar.Done()

	if bar.current != bar.total {
		t.Fatalf("current = %d, want total %d", bar.current, bar.total)
	}
	if !bar.done {
		t.Fatal("done = false, want true")
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("Done() output = %q, want trailing newline", buf.String())
	}
}

func TestBarFailedSetsDoneAndDescription(t *testing.T) {
	const failureMessage = "download failed"

	var buf bytes.Buffer
	bar := New(&buf, barTotal)

	bar.Failed(failureMessage)

	if !bar.done {
		t.Fatal("done = false, want true")
	}
	if bar.description != failureMessage {
		t.Fatalf("description = %q, want %q", bar.description, failureMessage)
	}
	if !strings.Contains(buf.String(), failureMessage) {
		t.Fatalf("Failed() output = %q, want %q", buf.String(), failureMessage)
	}
}

func TestBarRenderContainsPercentage(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)
	bar.current = renderCurrent
	bar.description = "copying"

	bar.render()

	output := buf.String()
	if !strings.Contains(output, "50%") {
		t.Fatalf("render() output = %q, want percentage", output)
	}
	if !strings.Contains(output, "copying") {
		t.Fatalf("render() output = %q, want description", output)
	}
	if !strings.Contains(output, "\r\x1b[K[") {
		t.Fatalf("render() output = %q, want cursor control prefix", output)
	}
	if bar.lastUpdate.IsZero() {
		t.Fatal("lastUpdate was not set")
	}
}

func TestBarRenderWithZeroTotalIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, 0)

	bar.render()

	if buf.Len() != 0 {
		t.Fatalf("render() wrote %q, want no output", buf.String())
	}
}

func TestBarRenderCapsPercentAtOneHundred(t *testing.T) {
	var buf bytes.Buffer
	bar := New(&buf, barTotal)
	bar.current = overflowCurrent

	bar.render()

	output := buf.String()
	if !strings.Contains(output, "100%") {
		t.Fatalf("render() output = %q, want capped percentage", output)
	}
	if !strings.Contains(output, "(15/10)") {
		t.Fatalf("render() output = %q, want actual counts", output)
	}
}

func TestNewMultiBarStartsEmpty(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)

	if multi.writer != &buf {
		t.Fatalf("writer = %v, want %v", multi.writer, &buf)
	}
	if len(multi.bars) != 0 {
		t.Fatalf("len(bars) = %d, want 0", len(multi.bars))
	}
}

func TestMultiBarAddBar(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)

	entry := multi.AddBar("alpha", barTotal)

	if len(multi.bars) != 1 {
		t.Fatalf("len(bars) = %d, want 1", len(multi.bars))
	}
	if multi.bars[0] != entry {
		t.Fatal("AddBar() entry was not stored")
	}
	if entry.Name != "alpha" || entry.Total != barTotal || entry.Status != "running" {
		t.Fatalf("entry = %#v, want initialized running bar", entry)
	}
}

func TestMultiBarUpdateFindsBarByName(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)
	multi.AddBar("alpha", barTotal)
	multi.AddBar("beta", barTotal)

	multi.Update("beta", renderCurrent, "working")

	alpha := multi.bars[0]
	beta := multi.bars[1]
	if alpha.Current != 0 || alpha.Description != "" {
		t.Fatalf("alpha = %#v, want unchanged", alpha)
	}
	if beta.Current != renderCurrent {
		t.Fatalf("beta current = %d, want %d", beta.Current, renderCurrent)
	}
	if beta.Description != "working" {
		t.Fatalf("beta description = %q, want %q", beta.Description, "working")
	}
	output := buf.String()
	if !strings.Contains(output, "beta") || !strings.Contains(output, "50%") {
		t.Fatalf("Update() output = %q, want updated bar rendering", output)
	}
}

func TestMultiBarSetStatusUpdatesStatus(t *testing.T) {
	var buf bytes.Buffer
	multi := NewMultiBar(&buf)
	multi.AddBar("alpha", barTotal)

	multi.SetStatus("alpha", "failed")

	entry := multi.bars[0]
	if entry.Status != "failed" {
		t.Fatalf("status = %q, want %q", entry.Status, "failed")
	}
	if !strings.Contains(buf.String(), "❌") {
		t.Fatalf("SetStatus() output = %q, want failed icon", buf.String())
	}
}

func TestNewLiveBarDefaults(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	if bar.writer != &buf {
		t.Fatalf("writer = %v, want %v", bar.writer, &buf)
	}
	if bar.width != defaultLiveBarWidth {
		t.Fatalf("width = %d, want %d", bar.width, defaultLiveBarWidth)
	}
	if bar.lastPct != -1 {
		t.Fatalf("lastPct = %d, want -1", bar.lastPct)
	}
	if bar.startTime.IsZero() {
		t.Fatal("startTime was not initialized")
	}
}

func TestLiveBarSetWidth(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)

	if got := bar.SetWidth(updatedLiveBarWidth); got != bar {
		t.Fatal("SetWidth() did not return the live bar instance")
	}
	if bar.width != updatedLiveBarWidth {
		t.Fatalf("width = %d, want %d", bar.width, updatedLiveBarWidth)
	}
}

func TestLiveBarRenderOnlyOnPercentChange(t *testing.T) {
	var buf bytes.Buffer
	bar := NewLiveBar(&buf)
	status := Status{Label: "build", Percent: livePercent, Done: liveDone, Total: liveTotal, Current: "step-1"}

	if updated := bar.Render(status); !updated {
		t.Fatal("first Render() = false, want true")
	}
	firstOutput := buf.String()
	if firstOutput == "" {
		t.Fatal("first Render() wrote no output")
	}

	buf.Reset()
	if updated := bar.Render(status); updated {
		t.Fatal("second Render() = true, want false")
	}
	if buf.Len() != 0 {
		t.Fatalf("second Render() wrote %q, want no output", buf.String())
	}
}

func TestLiveBarRenderStatusIconsAndNewlines(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		var buf bytes.Buffer
		bar := NewLiveBar(&buf)

		updated := bar.Render(Status{Label: "build", Percent: livePercent, Done: liveTotal, Total: liveTotal, Complete: true})
		if !updated {
			t.Fatal("Render() = false, want true")
		}
		output := buf.String()
		if !strings.Contains(output, "✅") {
			t.Fatalf("complete output = %q, want success icon", output)
		}
		if !strings.Contains(output, "100%") {
			t.Fatalf("complete output = %q, want 100%%", output)
		}
		if !strings.HasSuffix(output, "\n") {
			t.Fatalf("complete output = %q, want trailing newline", output)
		}
	})

	t.Run("failed", func(t *testing.T) {
		const failReason = "boom"

		var buf bytes.Buffer
		bar := NewLiveBar(&buf)

		updated := bar.Render(Status{Label: "build", Failed: true, FailReason: failReason})
		if !updated {
			t.Fatal("Render() = false, want true")
		}
		output := buf.String()
		if !strings.Contains(output, "❌") {
			t.Fatalf("failed output = %q, want failure icon", output)
		}
		if !strings.Contains(output, failReason) {
			t.Fatalf("failed output = %q, want fail reason", output)
		}
		if !strings.HasSuffix(output, "\n") {
			t.Fatalf("failed output = %q, want trailing newline", output)
		}
	})
}

func TestHelperRenderFunctions(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		var buf bytes.Buffer
		RenderSimple(&buf, "sync", renderPercent, helperDone, helperTotal, "phase-1")

		output := buf.String()
		if !strings.Contains(output, "sync") || !strings.Contains(output, "50%") || !strings.Contains(output, "phase-1") {
			t.Fatalf("RenderSimple() output = %q, want label, percentage, and current item", output)
		}
	})

	t.Run("complete", func(t *testing.T) {
		var buf bytes.Buffer
		RenderComplete(&buf, "sync", helperTotal)

		output := buf.String()
		if !strings.Contains(output, "✅") || !strings.Contains(output, "100%") || !strings.HasSuffix(output, "\n") {
			t.Fatalf("RenderComplete() output = %q, want completion rendering", output)
		}
	})

	t.Run("failed", func(t *testing.T) {
		const failReason = "network error"

		var buf bytes.Buffer
		RenderFailed(&buf, "sync", failReason)

		output := buf.String()
		if !strings.Contains(output, "❌") || !strings.Contains(output, failReason) || !strings.HasSuffix(output, "\n") {
			t.Fatalf("RenderFailed() output = %q, want failure rendering", output)
		}
	})
