package taui

import (
	"fmt"
	"sync"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// ToolState is the lifecycle state of a ToolRow.
type ToolState int

const (
	ToolRunning ToolState = iota
	ToolSuccess
	ToolFailed
)

// ToolStyle selects how a ToolRow renders. tau is expected to make this
// configurable (see NOTES.md).
type ToolStyle int

const (
	// ToolStyleCombined renders fg-only — a spinner / ✓ / ✗ glyph plus grey
	// text, with no embedded reset — so the row composes inside a coloured Box
	// (the look from examples/combined). This is the default.
	ToolStyleCombined ToolStyle = iota
	// ToolStyleBadge renders bg-chip SUCCESS / FAILED badges on the default
	// background (the look from examples/tooldemo). Not safe inside a coloured
	// Box because the badges embed resets.
	ToolStyleBadge
)

// ToolRow renders a single tool-call line through its lifecycle: an animated
// RUNNING line that resolves to SUCCESS or FAILED, with a status badge, the
// tool name, its arguments, and a timing/detail suffix.
//
// Unlike termkit.ToolLifecycle — which writes escape sequences straight to a
// writer and overwrites its own line — ToolRow is a taui Component: Render
// returns the current line and the engine's differential renderer handles
// positioning. That lets tool calls live *inside* a Box alongside other
// components. Advance the spinner with Tick (e.g. from a shared ticker) while
// the row is ToolRunning.
//
// ToolRow is safe for concurrent use: application goroutines call Tick/Succeed/
// Fail while the render goroutine calls Render.
type ToolRow struct {
	mu     sync.Mutex
	name   string
	args   string
	state  ToolState
	detail string
	start  time.Time
	frame  int
	style  ToolStyle
}

// NewToolRow creates a running tool row for the given tool name and arguments.
func NewToolRow(name, args string) *ToolRow {
	return &ToolRow{
		name:  name,
		args:  args,
		state: ToolRunning,
		start: time.Now(),
	}
}

// Tick advances the spinner one frame while running; a no-op once resolved.
func (r *ToolRow) Tick() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == ToolRunning {
		r.frame++
	}
}

// SetStyle selects the render style (combined or badge).
func (r *ToolRow) SetStyle(s ToolStyle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.style = s
}

// Succeed resolves the row as successful. detail is an optional suffix (e.g.
// "done in 1.2s"); empty falls back to the elapsed time.
func (r *ToolRow) Succeed(detail string) { r.resolve(ToolSuccess, detail) }

// Fail resolves the row as failed. detail is an optional suffix (e.g. "exit 1").
func (r *ToolRow) Fail(detail string) { r.resolve(ToolFailed, detail) }

func (r *ToolRow) resolve(state ToolState, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
	r.detail = detail
}

// State returns the current lifecycle state.
func (r *ToolRow) State() ToolState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Running reports whether the row is still in progress.
func (r *ToolRow) Running() bool { return r.State() == ToolRunning }

// Invalidate is a no-op; ToolRow holds no cached render.
func (r *ToolRow) Invalidate() {}

// Render returns the single styled line for the row's current state, in the
// configured style.
func (r *ToolRow) Render(width int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.style == ToolStyleBadge {
		return []string{r.renderBadge(width)}
	}
	return []string{r.renderCombined(width)}
}

// detailText returns the timing/detail suffix for the current state.
func (r *ToolRow) detailText() string {
	switch r.state {
	case ToolSuccess:
		if r.detail != "" {
			return r.detail
		}
		return fmt.Sprintf("done in %.1fs", time.Since(r.start).Seconds())
	case ToolFailed:
		if r.detail != "" {
			return r.detail
		}
		return "exit 1"
	default:
		return fmt.Sprintf("%.1fs", time.Since(r.start).Seconds())
	}
}

func (r *ToolRow) glyph() string {
	switch r.state {
	case ToolSuccess:
		return "✓"
	case ToolFailed:
		return "✗"
	default:
		return termkit.SpinnerFrame(r.frame)
	}
}

// renderCombined renders the row as plain text with no colour of its own, so it
// inherits the fg/bg of a surrounding coloured Box (the boxdemo / combined
// look): the box's colour pair styles the whole row, and the glyph shape plus
// the box background convey state. This composes cleanly because the row adds
// no fg override and no reset to fight the box.
func (r *ToolRow) renderCombined(width int) string {
	line := fmt.Sprintf("%s %s (%s) — %s", r.glyph(), r.name, r.args, r.detailText())
	if width > 0 {
		line = TruncateToWidth(line, width, "…")
	}
	return line
}

// renderBadge uses bg-chip badges, like examples/tooldemo. Not safe inside a
// coloured Box (the badges embed resets), so it is left on the default bg.
func (r *ToolRow) renderBadge(width int) string {
	name := r.name
	args := "(" + r.args + ")"
	detail := r.detailText()

	if !termkit.ColorEnabled() {
		var label string
		switch r.state {
		case ToolSuccess:
			label = "SUCCESS"
		case ToolFailed:
			label = "FAILED "
		default:
			label = "RUNNING"
		}
		line := fmt.Sprintf("%s %s %s — %s", label, name, args, detail)
		if width > 0 {
			line = TruncateToWidth(line, width, "…")
		}
		return line
	}

	switch r.state {
	case ToolSuccess:
		return fmt.Sprintf("%s %s %s %s",
			termkit.Style(" SUCCESS ", termkit.ColorGreen.Bg(), termkit.ColorObsidian.Fg(), termkit.Bold),
			termkit.Style(name, termkit.Bold),
			termkit.Style(args, termkit.ColorGrey.Fg()),
			termkit.Style("— "+detail, termkit.ColorGrey.Fg()))
	case ToolFailed:
		return fmt.Sprintf("%s %s %s %s",
			termkit.Style(" FAILED  ", termkit.ColorRed.Bg(), termkit.ColorWhite.Fg(), termkit.Bold),
			termkit.Style(name, termkit.Bold),
			termkit.Style(args, termkit.ColorGrey.Fg()),
			termkit.Style("— "+detail, termkit.ColorGrey.Fg()))
	default:
		return fmt.Sprintf("%s %s %s %s %s",
			termkit.SpinnerFrame(r.frame),
			termkit.Style(" RUNNING ", termkit.ColorAmber.Bg(), termkit.ColorYellow.Fg(), termkit.Bold),
			termkit.Style(name, termkit.Bold),
			termkit.Style(args, termkit.ColorGrey.Fg()),
			termkit.Style(detail, termkit.ColorGrey.Fg()))
	}
}
