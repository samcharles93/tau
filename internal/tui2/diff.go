package tui2

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/theme"
)

// diffViewerWidthFrac and diffViewerHeightFrac size the overlay relative to
// the terminal - big enough to actually read a diff, small enough to keep
// the surrounding chrome visible as context that something is layered on
// top of it.
const (
	diffViewerWidthFrac  = 0.9
	diffViewerHeightFrac = 0.85
)

// openDiffViewer builds and opens the "View diff" overlay for t, which must
// satisfy toolSupportsDiffView. Replaces any other open exclusive overlay,
// since activating a menu item always closes the menu it came from.
func (m *model) openDiffViewer(t toolState) tea.Cmd {
	m.closeOtherExclusiveOverlays(overlayDiff)

	details, ok := t.details.(tools.DiffDetails)
	if !ok {
		return nil
	}

	boxW := max(20, int(float64(m.width)*diffViewerWidthFrac))
	boxH := max(10, int(float64(m.height)*diffViewerHeightFrac))
	// Account for the border + padding compositeDiffViewer wraps the body
	// in, plus the title/footer lines rendered alongside the viewport.
	innerWidth := max(20, boxW-4)
	innerHeight := max(3, boxH-6)

	content := m.renderUnifiedDiff(details.Path, details.OldContent, details.NewContent, innerWidth)

	vp := viewport.New(viewport.WithWidth(innerWidth), viewport.WithHeight(innerHeight))
	vp.SetContent(content)

	m.diffViewer = &diffViewerState{
		title:    details.Path,
		viewport: vp,
	}
	return nil
}

// renderUnifiedDiff builds a unified diff between old and new, wraps it in a
// fenced "diff" code block, and renders it through a glamour renderer
// memoized for width in m.mdCache - the same cache used for tool-result
// markdown elsewhere (see renderToolBox), lazily created via ensureMDRenderer
// if this exact width hasn't been requested before.
func (m *model) renderUnifiedDiff(path, oldContent, newContent string, width int) string {
	diffText, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: path,
		ToFile:   path,
		Context:  3,
	})
	if err != nil {
		diffText = fmt.Sprintf("error computing diff: %v", err)
	}
	if strings.TrimSpace(diffText) == "" {
		diffText = "(no textual differences)"
	}

	ensureMDRenderer(m.mdCache, width)
	renderer, ok := m.mdCache[width]
	if !ok || renderer == nil {
		return diffText
	}
	out, err := renderer.Render("```diff\n" + diffText + "\n```")
	if err != nil {
		return diffText
	}
	return strings.TrimRight(out, "\n")
}

// handleDiffViewerKey handles keyboard input while the diff viewer overlay
// is open. Esc/q closes it; everything else is forwarded to the embedded
// viewport for scrolling.
func (m *model) handleDiffViewerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.diffViewer = nil
		return nil
	}
	var cmd tea.Cmd
	m.diffViewer.viewport, cmd = m.diffViewer.viewport.Update(msg)
	return cmd
}

// compositeDiffViewer overlays the open diff viewer centered on top of base,
// following compositeContextMenu's lipgloss.Compositor shape (see that
// function's doc comment for why a bare Layer.Draw isn't enough).
func (m *model) compositeDiffViewer(base string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(themeHex(theme.SecondaryColor)).
		Padding(0, 1)

	title := m.diffViewer.title
	body := boxStyle.Render(title + "\n\n" + m.diffViewer.viewport.View() + "\n\n" + toolMetaStyle.Render("Esc: close  ↑/↓ PgUp/PgDn: scroll"))

	bx, by := centerRect(m.width, m.height, lipgloss.Width(body), lipgloss.Height(body))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(body).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}

// centerRect returns the top-left position to center a boxW x boxH box
// within a w x h area, clamped so it never renders off-screen.
func centerRect(w, h, boxW, boxH int) (x, y int) {
	x = max(0, (w-boxW)/2)
	y = max(0, (h-boxH)/2)
	return x, y
}
