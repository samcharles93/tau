package tui2

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// childTranscriptViewerState is the state of an open child-agent transcript
// overlay (drill-down into a finished child's full conversation, see
// docs/specs/agents/05-ui.md "Drill-down"). A nil *childTranscriptViewerState
// on model means none is open — mirrors diffViewerState's nil-sentinel idiom.
type childTranscriptViewerState struct {
	title     string
	sessionID string
	viewport  viewport.Model
	loading   bool
}

// openChildTranscriptViewer opens the transcript overlay for the finished
// child agent behind callID, sizing it the same as the diff viewer overlay
// and kicking off an async load of the child's persisted session. Mirrors
// openDiffViewer's shape. Returns nil (no overlay, an inline notification
// instead) if the child isn't terminal yet or never got a session ID (e.g.
// it errored before the child process could report one).
func (m *model) openChildTranscriptViewer(callID string) tea.Cmd {
	child, ok := m.childAgents[callID]
	if !ok || !isChildTerminal(child.status) {
		return m.setNotification("agent still running — drill-down is available once it finishes")
	}
	if child.sessionID == "" {
		return m.setNotification("agent " + child.instanceID + " has no recorded session to open")
	}

	m.contextMenu = nil
	m.diffViewer = nil

	boxW := max(20, int(float64(m.width)*diffViewerWidthFrac))
	boxH := max(10, int(float64(m.height)*diffViewerHeightFrac))
	innerWidth := max(20, boxW-4)
	innerHeight := max(3, boxH-6)

	vp := viewport.New(viewport.WithWidth(innerWidth), viewport.WithHeight(innerHeight))
	vp.SetContent("Loading transcript…")

	m.childTranscriptViewer = &childTranscriptViewerState{
		title:     "agent " + child.instanceID,
		sessionID: child.sessionID,
		viewport:  vp,
		loading:   true,
	}

	return sendCommand(m.runtime, tauchat.LoadChildTranscriptCommand{SessionID: child.sessionID})
}

// handleChildTranscriptViewerKey handles keyboard input while the child
// transcript overlay is open. Esc/q closes it; everything else is forwarded
// to the embedded viewport for scrolling. Mirrors handleDiffViewerKey.
func (m *model) handleChildTranscriptViewerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.childTranscriptViewer = nil
		return nil
	}
	var cmd tea.Cmd
	m.childTranscriptViewer.viewport, cmd = m.childTranscriptViewer.viewport.Update(msg)
	return cmd
}

// compositeChildTranscriptViewer overlays the open child transcript viewer
// centered on top of base. Mirrors compositeDiffViewer's shape.
func (m *model) compositeChildTranscriptViewer(base string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(themeHex(theme.SecondaryColor)).
		Padding(0, 1)

	body := boxStyle.Render(m.childTranscriptViewer.title + "\n\n" +
		m.childTranscriptViewer.viewport.View() + "\n\n" +
		toolMetaStyle.Render("Esc: close  ↑/↓ PgUp/PgDn: scroll"))

	bx, by := centerRect(m.width, m.height, lipgloss.Width(body), lipgloss.Height(body))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(body).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
