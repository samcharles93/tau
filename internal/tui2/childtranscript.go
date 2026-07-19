package tui2

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// childTranscriptViewerState is the state of an open child-agent transcript
// overlay (drill-down into a child's conversation, live or finished). See
// docs/specs/agents/05-ui.md "Drill-down". A nil *childTranscriptViewerState
// on model means none is open - mirrors diffViewerState's nil-sentinel idiom.
type childTranscriptViewerState struct {
	title     string
	sessionID string
	callID    string // the agent tool call ID, for live transcript lookup
	live      bool   // true if child is still running (render from childMessages)
	viewport  viewport.Model
	loading   bool
}

// openChildTranscriptViewer opens the transcript overlay for the child agent
// behind callID. For live children, messages are read from the in-memory
// childMessages buffer; for finished children, the persisted session is loaded
// asynchronously. Returns nil (no overlay, an inline notification instead) if
// the child has no session ID and no live messages.
func (m *model) openChildTranscriptViewer(callID string) tea.Cmd {
	child, ok := m.childAgents[callID]
	if !ok {
		return m.setNotification("agent not found")
	}

	m.closeOtherExclusiveOverlays(overlayChildTranscript)

	boxW := max(20, int(float64(m.width)*diffViewerWidthFrac))
	boxH := max(10, int(float64(m.height)*diffViewerHeightFrac))
	innerWidth := max(20, boxW-4)
	innerHeight := max(3, boxH-6)

	vp := viewport.New(viewport.WithWidth(innerWidth), viewport.WithHeight(innerHeight))

	if child.status.IsTerminal() {
		// Finished child: load from persisted session.
		if child.sessionID == "" {
			return m.setNotification("agent " + child.instanceID + " has no recorded session to open")
		}
		vp.SetContent("Loading transcript…")
		m.childTranscriptViewer = &childTranscriptViewerState{
			title:     "agent " + child.instanceID,
			sessionID: child.sessionID,
			callID:    callID,
			viewport:  vp,
			loading:   true,
		}
		return sendCommand(m.runtime, tauchat.LoadChildTranscriptCommand{SessionID: child.sessionID})
	}

	// Live child: render from in-memory childMessages buffer.
	msgs := m.childMessages[callID]
	if len(msgs) == 0 {
		// No messages yet, but set up for live streaming.
		vp.SetContent("Waiting for agent output…")
	} else {
		lines := m.renderChildTranscriptLines(msgs, innerWidth)
		vp.SetContent(strings.Join(lines, "\n"))
		vp.GotoBottom()
	}

	m.childTranscriptViewer = &childTranscriptViewerState{
		title:     "agent " + child.instanceID + " (live)",
		sessionID: child.sessionID,
		callID:    callID,
		live:      true,
		viewport:  vp,
	}
	return nil
}

// refreshLiveChildTranscript rebuilds the live transcript overlay content from
// m.childMessages[callID]. Called automatically when a ChildAgentMessageEvent
// arrives while the overlay is open.
func (m *model) refreshLiveChildTranscript() {
	if m.childTranscriptViewer == nil || !m.childTranscriptViewer.live {
		return
	}
	msgs := m.childMessages[m.childTranscriptViewer.callID]
	innerWidth := max(20, m.childTranscriptViewer.viewport.Width())
	lines := m.renderChildTranscriptLines(msgs, innerWidth)
	m.childTranscriptViewer.viewport.SetContent(strings.Join(lines, "\n"))
	m.childTranscriptViewer.viewport.GotoBottom()
}

// handleChildTranscriptViewerKey handles keyboard input while the child
// transcript overlay is open. Esc/q closes it; 'o' opens the child as a full
// resumable session; everything else is forwarded to the embedded viewport.
func (m *model) handleChildTranscriptViewerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.childTranscriptViewer = nil
		return nil
	case "o":
		if m.childTranscriptViewer.sessionID != "" {
			m.childTranscriptViewer = nil
			return sendCommand(m.runtime, tauchat.LoadSessionCommand{SessionID: m.childTranscriptViewer.sessionID})
		}
		return nil
	}
	var cmd tea.Cmd
	m.childTranscriptViewer.viewport, cmd = m.childTranscriptViewer.viewport.Update(msg)
	return cmd
}

// compositeChildTranscriptViewer overlays the open child transcript viewer
// centered on top of base. Mirrors compositeDiffViewer's shape.
func (m *model) compositeChildTranscriptViewer(base string) string {
	help := "Esc: close  ↑/↓ PgUp/PgDn: scroll"
	if m.childTranscriptViewer.sessionID != "" {
		help += "  o: open session"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(themeHex(theme.SecondaryColor)).
		Padding(0, 1)

	body := boxStyle.Render(m.childTranscriptViewer.title + "\n\n" +
		m.childTranscriptViewer.viewport.View() + "\n\n" +
		toolMetaStyle.Render(help))

	bx, by := centerRect(m.width, m.height, lipgloss.Width(body), lipgloss.Height(body))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(body).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
