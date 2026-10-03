package progress

import (
	"bytes"
	"strings"
	"testing"
)

const (
	defaultLiveBarWidth = 50
	updatedLiveBarWidth = 32
	livePercent         = 25
	liveDone            = 1
	liveTotal           = 4
)

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
