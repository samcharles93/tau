package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// modeLabelLeftMargin is how many rule dashes precede a mode's label, e.g.
// "──── Shell ──────...". Unlike a static Divider label (which is centered),
// a mode label sits near the left edge — it's an indicator meant to catch
// the eye immediately, not a centered section heading.
const modeLabelLeftMargin = 4

// InputMode is a named, colored mode a Divider can render itself as — e.g.
// "Shell" while a "!" bash command is being typed, or "Planning" while
// typing "/plan". Color is used both as the rule foreground and as the
// background behind the label.
type InputMode struct {
	Label string
	Color termkit.Color
}

// Divider renders a full-width horizontal rule, with an optional centered
// label. If SetModeFunc is given, the label and color are resolved fresh on
// every render from the current mode instead of the static label passed to
// NewDivider — see SetModeFunc.
type Divider struct {
	label       string
	modeFn      func() *InputMode
	labelHidden bool
}

// NewDivider creates a Divider component. An empty label renders a plain rule.
func NewDivider(label string) *Divider {
	return &Divider{label: label}
}

// SetModeFunc installs a getter consulted on every Render to determine the
// divider's current color (and, unless HideModeLabel was called, its
// label). When fn is nil or returns nil, Render falls back to the static
// label passed to NewDivider, uncolored — so a Divider with no mode func
// behaves identically to before this existed.
func (d *Divider) SetModeFunc(fn func() *InputMode) { d.modeFn = fn }

// HideModeLabel makes this Divider render the active mode's color as a
// plain full-width rule without its name — e.g. the bottom divider around
// an input box, so only the top one names the mode.
func (d *Divider) HideModeLabel() { d.labelHidden = true }

// Invalidate is a no-op; Divider holds no cached render.
func (d *Divider) Invalidate() {}

// Render returns the single rule line.
func (d *Divider) Render(width int) []string {
	if width <= 0 {
		width = 1
	}

	var mode *InputMode
	if d.modeFn != nil {
		mode = d.modeFn()
	}

	if mode != nil && !d.labelHidden && mode.Label != "" {
		return []string{renderModeLabel(width, *mode)}
	}

	if mode != nil {
		line := strings.Repeat("─", width)
		if termkit.ColorEnabled() {
			line = termkit.FgOnly(line, mode.Color) + termkit.Reset
		}
		return []string{line}
	}

	if d.label == "" {
		return []string{strings.Repeat("─", width)}
	}
	text := " " + d.label + " "
	labelWidth := VisibleWidth(text)
	if labelWidth >= width {
		return []string{TruncateToWidth(text, width, "")}
	}
	left := (width - labelWidth) / 2
	right := width - labelWidth - left
	return []string{strings.Repeat("─", left) + text + strings.Repeat("─", right)}
}

// renderModeLabel renders a left-aligned, colored mode label — the label
// pill sits modeLabelLeftMargin dashes from the start of the line, with the
// remaining width filled by colored dashes.
func renderModeLabel(width int, mode InputMode) string {
	text := " " + mode.Label + " "
	labelWidth := VisibleWidth(text)
	if labelWidth >= width {
		t := TruncateToWidth(text, width, "")
		if !termkit.ColorEnabled() {
			return t
		}
		return termkit.FgBgOnly(t, termkit.ContrastFg(mode.Color), mode.Color) + termkit.Reset
	}

	left := modeLabelLeftMargin
	if left > width-labelWidth {
		left = 0
	}
	right := width - labelWidth - left
	dashesL := strings.Repeat("─", left)
	dashesR := strings.Repeat("─", right)

	if !termkit.ColorEnabled() {
		return dashesL + text + dashesR
	}

	// The label pill sets both fg AND bg (FgBgOnly/Wrap emits no trailing
	// Reset), so without an explicit Reset before the trailing dashes, the
	// pill's background would bleed forward — the dashesR segment only
	// overwrites the foreground, leaving stray background fill instead of
	// colored dash glyphs.
	pill := termkit.FgBgOnly(text, termkit.ContrastFg(mode.Color), mode.Color)
	return termkit.FgOnly(dashesL, mode.Color) + pill + termkit.Reset + termkit.FgOnly(dashesR, mode.Color) + termkit.Reset
}
