package tui2

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// paletteWidthFrac/paletteMinWidth/paletteMaxWidth size the floating
// completions overlay (Ctrl+P command palette, Ctrl+L model picker, and
// every "/"-triggered completion) — narrower than /help's (helpOverlayWidthFrac
// etc, help.go), since a short ranked list needs far less room than a
// two-column keybinding reference.
const (
	paletteWidthFrac = 0.6
	paletteMinWidth  = 40
	paletteMaxWidth  = 70
)

// openCommandPalette handles Ctrl+P. There is no separate "palette" state —
// Ctrl+P just ensures m.input starts with "/" (so the existing completions
// system — completionRows/handleCompletionKey/completionsVisible — has
// something to show) and un-dismisses it. Rendering that dropdown as a
// floating overlay instead of flow-laid chrome (compositeCompletionsOverlay)
// is a pure presentation change: navigation, fuzzy matching, and every input-
// editing key (Ctrl+Backspace, Ctrl+Left/Right, Home/End, ...) keep working
// exactly as they did for the inline "/" dropdown, since they're the same
// code path operating on the same m.input/m.inputCursor.
func (m *model) openCommandPalette() tea.Cmd {
	if !strings.HasPrefix(m.input, "/") {
		m.input = "/"
		m.inputCursor = 1
	}
	m.compDismissed = false
	m.compSelected = 0
	return nil
}

// openModelPalette handles Ctrl+L: prefills "/model " so the same completions
// system shows exactly what /model's argument completer already offers
// (modelCompletions), floated the same way Ctrl+P's is. Accepting a row goes
// through the normal accept-and-submit path (acceptCompletionRow +
// submitInput -> handleSlashCommand -> cmdModel), so picking a model here
// does exactly what typing "/model <id>" does.
func (m *model) openModelPalette() tea.Cmd {
	if len(m.availableModels) == 0 {
		return m.setNotification("no models available — try /refresh")
	}
	m.input = "/model "
	m.inputCursor = utf8.RuneCountInString(m.input)
	m.compDismissed = false
	m.compSelected = 0
	return nil
}

// paletteOverlayWidth returns the box width for the floating completions
// overlay, given the terminal width — mirrors helpOverlayWidth's clamp shape
// (help.go).
func paletteOverlayWidth(termWidth int) int {
	w := int(float64(termWidth) * paletteWidthFrac)
	return max(paletteMinWidth, min(w, paletteMaxWidth))
}

// completionsOverlayTitle titles the floating box from what's actually being
// browsed: the single shared group name when every visible row belongs to
// one (e.g. "Models" mid /model, "Sessions" mid /session) — otherwise (the
// top-level Commands/Agents/Extensions mix) "Commands".
func completionsOverlayTitle(rows []compRow) string {
	if len(rows) == 0 {
		return "Commands"
	}
	group := rows[0].group
	for _, r := range rows[1:] {
		if r.group != group {
			return "Commands"
		}
	}
	return group
}

// renderCompletionsOverlay renders the completions dropdown's rows (the same
// renderCompletions body the old inline chrome used) inside a titled box —
// the floating presentation Ctrl+P/Ctrl+L/typing "/" all share.
func (m *model) renderCompletionsOverlay(rows []compRow) string {
	width := paletteOverlayWidth(m.width)
	innerWidth := max(width-4, 20)

	selected := m.compSelected
	if selected < 0 || selected >= len(rows) {
		selected = 0
	}
	body := strings.Split(renderCompletions(rows, selected, innerWidth), "\n")
	return renderBoxAround(width, completionsOverlayTitle(rows), body)
}

// compositeCompletionsOverlay overlays the completions dropdown centered on
// top of base, following compositeHelpOverlay's lipgloss.Compositor shape
// (see compositeContextMenu's doc comment for why a bare Layer.Draw isn't
// enough).
func (m *model) compositeCompletionsOverlay(base string, rows []compRow, _ string) string {
	rendered := m.renderCompletionsOverlay(rows)
	bx, by := centerRect(m.width, m.height, lipgloss.Width(rendered), lipgloss.Height(rendered))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(rendered).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
