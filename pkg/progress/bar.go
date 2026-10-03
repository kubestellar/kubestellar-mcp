// Package progress provides terminal progress bar utilities
package progress

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Status represents the current status of a progress operation
type Status struct {
	Label      string // e.g., "4.18.30" or "Building"
	Percent    int    // 0-100
	Done       int    // Completed items
	Total      int    // Total items
	Current    string // Current item being processed
	Complete   bool   // Whether operation is complete
	Failed     bool   // Whether operation failed
	FailReason string // Reason for failure
}

// LiveBar renders a self-overwriting progress bar to the terminal
type LiveBar struct {
	writer    io.Writer
	width     int
	lastPct   int
	startTime time.Time
}

// NewLiveBar creates a new live progress bar
func NewLiveBar(writer io.Writer) *LiveBar {
	return &LiveBar{
		writer:    writer,
		width:     50,
		lastPct:   -1,
		startTime: time.Now(),
	}
}

// SetWidth sets the progress bar width (default 50)
func (b *LiveBar) SetWidth(width int) *LiveBar {
	b.width = width
	return b
}

// Render draws the progress bar, overwriting the current line
// Returns true if the bar was updated (percentage changed)
func (b *LiveBar) Render(s Status) bool {
	// Only update if percentage changed (reduces flickering)
	if s.Percent == b.lastPct && !s.Complete && !s.Failed {
		return false
	}
	b.lastPct = s.Percent

	// Choose icon
	icon := "⏳"
	if s.Complete {
		icon = "✅"
		s.Percent = 100
	} else if s.Failed {
		icon = "❌"
	}

	// Build progress bar
	pct := s.Percent
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	filled := pct * b.width / 100
	bar := strings.Repeat("#", filled) + strings.Repeat("-", b.width-filled)

	// Format counts
	counts := ""
	if s.Total > 0 {
		counts = fmt.Sprintf("(%d/%d) ", s.Done, s.Total)
	}

	// Current item or fail reason
	suffix := s.Current
	if s.Failed && s.FailReason != "" {
		suffix = s.FailReason
	}

	// Use \r to return to start, \033[K to clear to end of line
	_, _ = fmt.Fprintf(b.writer, "\r\033[K%s %s [%s] %3d%% %s%s",
		icon, s.Label, bar, pct, counts, suffix)

	// Add newline if complete or failed
	if s.Complete || s.Failed {
		_, _ = fmt.Fprintln(b.writer)
	}

	return true
}
