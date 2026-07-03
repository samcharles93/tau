package taui

import "strings"

// Divider renders a full-width horizontal rule, with an optional centered
// label.
type Divider struct {
	label string
}

// NewDivider creates a Divider component. An empty label renders a plain rule.
func NewDivider(label string) *Divider {
	return &Divider{label: label}
}

// Invalidate is a no-op; Divider holds no cached render.
func (d *Divider) Invalidate() {}

// Render returns the single rule line.
func (d *Divider) Render(width int) []string {
	if width <= 0 {
		width = 1
	}
	if d.label == "" {
		return []string{strings.Repeat("─", width)}
	}
	label := " " + d.label + " "
	labelWidth := VisibleWidth(label)
	if labelWidth >= width {
		return []string{TruncateToWidth(label, width, "")}
	}
	left := (width - labelWidth) / 2
	right := width - labelWidth - left
	return []string{strings.Repeat("─", left) + label + strings.Repeat("─", right)}
}
