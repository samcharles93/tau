package taui

import "github.com/samcharles93/tau/pkg/taui/termkit"

// StatusRowState is the lifecycle state of a StatusRow.
type StatusRowState int

const (
	StatusRowRunning StatusRowState = iota
	StatusRowSuccess
	StatusRowFailed
	StatusRowNeutral
)

// StatusRow renders a single "<glyph> label - detail" line, reusing ToolRow's
// visual language (spinner / ✓ / ✗) without ToolRow's tool-call-specific
// "name (args)" formatting - this is what a plugin's generic StatusWidget
// renders through. Neutral renders a static bullet with no animation, for
// status that isn't an in-progress lifecycle.
type StatusRow struct {
	label  string
	detail string
	state  StatusRowState
	frame  int
}

// NewStatusRow creates a StatusRow in the given state.
func NewStatusRow(label, detail string, state StatusRowState) *StatusRow {
	return &StatusRow{label: label, detail: detail, state: state}
}

// Tick advances the spinner one frame while running; a no-op otherwise.
func (s *StatusRow) Tick() {
	if s.state == StatusRowRunning {
		s.frame++
	}
}

// Invalidate is a no-op; StatusRow holds no cached render.
func (s *StatusRow) Invalidate() {}

func (s *StatusRow) glyph() string {
	switch s.state {
	case StatusRowSuccess:
		return "✓"
	case StatusRowFailed:
		return "✗"
	case StatusRowNeutral:
		return "•"
	default:
		return termkit.SpinnerFrame(s.frame)
	}
}

// Render returns the single styled line for the row's current state.
func (s *StatusRow) Render(width int) []string {
	line := s.glyph() + " " + s.label
	if s.detail != "" {
		line += " - " + s.detail
	}
	if width > 0 {
		line = TruncateToWidth(line, width, "…")
	}
	return []string{line}
}
