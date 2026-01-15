// Package progress provides terminal progress bar utilities
package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Bar represents a terminal progress bar that overwrites itself
type Bar struct {
	mu          sync.Mutex
	writer      io.Writer
	total       int
	current     int
	width       int
	description string
	startTime   time.Time
	lastUpdate  time.Time
	done        bool
}

// New creates a new progress bar
func New(writer io.Writer, total int) *Bar {
	return &Bar{
		writer:    writer,
		total:     total,
		width:     40,
		startTime: time.Now(),
	}
}

// SetWidth sets the width of the progress bar
func (b *Bar) SetWidth(width int) *Bar {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.width = width
	return b
}

// SetDescription sets the description shown after the bar
func (b *Bar) SetDescription(desc string) *Bar {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.description = desc
	return b
}

// Set updates the current progress value
func (b *Bar) Set(current int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = current
	b.render()
}

// Increment adds to the current progress
func (b *Bar) Increment(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current += n
	b.render()
}

// Update sets both progress and description atomically
func (b *Bar) Update(current int, description string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = current
	b.description = description
	b.render()
}

// Done marks the progress bar as complete
func (b *Bar) Done() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.total
	b.done = true
	b.render()
	fmt.Fprintln(b.writer) // Move to next line
}

// Failed marks the progress bar as failed
func (b *Bar) Failed(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.done = true
	b.description = msg
	b.render()
	fmt.Fprintln(b.writer) // Move to next line
}

// render draws the progress bar (must be called with lock held)
func (b *Bar) render() {
	if b.total <= 0 {
		return
	}

	percent := float64(b.current) / float64(b.total) * 100
	if percent > 100 {
		percent = 100
	}

	filled := int(float64(b.width) * percent / 100)
	if filled > b.width {
		filled = b.width
	}

	bar := strings.Repeat("#", filled) + strings.Repeat("-", b.width-filled)

	// Calculate ETA
	elapsed := time.Since(b.startTime)
	eta := ""
	if b.current > 0 && percent < 100 {
		remaining := time.Duration(float64(elapsed) * (100 - percent) / percent)
		if remaining > time.Minute {
			eta = fmt.Sprintf(" ETA: %dm%ds", int(remaining.Minutes()), int(remaining.Seconds())%60)
		} else {
			eta = fmt.Sprintf(" ETA: %ds", int(remaining.Seconds()))
		}
	}

	// Use \r to overwrite line, \033[K to clear to end of line
	fmt.Fprintf(b.writer, "\r\033[K[%s] %3.0f%% (%d/%d) %s%s",
		bar, percent, b.current, b.total, b.description, eta)

	b.lastUpdate = time.Now()
}

// MultiBar manages multiple progress bars
type MultiBar struct {
	mu     sync.Mutex
	writer io.Writer
	bars   []*BarEntry
}

// BarEntry represents a named progress bar in a MultiBar
type BarEntry struct {
	Name        string
	Current     int
	Total       int
	Description string
	Status      string // "running", "done", "failed"
}

// NewMultiBar creates a new multi-bar manager
func NewMultiBar(writer io.Writer) *MultiBar {
	return &MultiBar{
		writer: writer,
		bars:   make([]*BarEntry, 0),
	}
}

// AddBar adds a new bar entry
func (m *MultiBar) AddBar(name string, total int) *BarEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := &BarEntry{
		Name:   name,
		Total:  total,
		Status: "running",
	}
	m.bars = append(m.bars, entry)
	return entry
}

// Update updates a bar and redraws all bars
func (m *MultiBar) Update(name string, current int, description string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, bar := range m.bars {
		if bar.Name == name {
			bar.Current = current
			bar.Description = description
			break
		}
	}
	m.render()
}

// SetStatus updates the status of a bar
func (m *MultiBar) SetStatus(name string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, bar := range m.bars {
		if bar.Name == name {
			bar.Status = status
			break
		}
	}
	m.render()
}

// render redraws all bars (must be called with lock held)
func (m *MultiBar) render() {
	// Move cursor up N lines and clear
	if len(m.bars) > 1 {
		fmt.Fprintf(m.writer, "\033[%dA", len(m.bars)-1)
	}

	for _, bar := range m.bars {
		statusIcon := "⏳"
		switch bar.Status {
		case "done":
			statusIcon = "✅"
		case "failed":
			statusIcon = "❌"
		}

		percent := 0.0
		if bar.Total > 0 {
			percent = float64(bar.Current) / float64(bar.Total) * 100
		}

		width := 20
		filled := int(float64(width) * percent / 100)
		if filled > width {
			filled = width
		}
		barStr := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)

		fmt.Fprintf(m.writer, "\r\033[K%s %-20s [%s] %3.0f%% %s\n",
			statusIcon, bar.Name, barStr, percent, bar.Description)
	}
}

// Done finalizes the multi-bar display
func (m *MultiBar) Done() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Already on new lines from render
}

// Status represents the current status of a progress operation
type Status struct {
	Label      string  // e.g., "4.18.30" or "Building"
	Percent    int     // 0-100
	Done       int     // Completed items
	Total      int     // Total items
	Current    string  // Current item being processed
	Complete   bool    // Whether operation is complete
	Failed     bool    // Whether operation failed
	FailReason string  // Reason for failure
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
	fmt.Fprintf(b.writer, "\r\033[K%s %s [%s] %3d%% %s%s",
		icon, s.Label, bar, pct, counts, suffix)

	// Add newline if complete or failed
	if s.Complete || s.Failed {
		fmt.Fprintln(b.writer)
	}

	return true
}

// RenderSimple is a convenience function for one-shot progress rendering
func RenderSimple(w io.Writer, label string, pct, done, total int, current string) {
	bar := NewLiveBar(w)
	bar.Render(Status{
		Label:   label,
		Percent: pct,
		Done:    done,
		Total:   total,
		Current: current,
	})
}

// RenderComplete renders a completed progress bar
func RenderComplete(w io.Writer, label string, total int) {
	bar := NewLiveBar(w)
	bar.Render(Status{
		Label:    label,
		Percent:  100,
		Done:     total,
		Total:    total,
		Complete: true,
	})
}

// RenderFailed renders a failed progress bar
func RenderFailed(w io.Writer, label string, reason string) {
	bar := NewLiveBar(w)
	bar.Render(Status{
		Label:      label,
		Failed:     true,
		FailReason: reason,
	})
}
