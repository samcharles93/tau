package tui2

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// sessionDisplayTitle leads with the session's human-meaningful title,
// falling back to the short ID when no title was derived (e.g. a session
// with no user message yet).
func sessionDisplayTitle(s tauchat.SessionSummary) string {
	if s.Title != "" {
		return s.Title
	}
	return shortSessionID(s.ID)
}

// shortSessionID returns the first 8 characters of a session UUID - enough
// to tell sessions apart visually without the full opaque ID.
func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// relTime renders a timestamp as an easy-to-read relative age ("2 hours
// ago") via go-humanize, replacing raw duration math at display sites.
func relTime(t time.Time) string {
	return humanize.Time(t)
}

// sessionTreeRow is one flattened row of the Ctrl+O session navigator: a
// session summary plus its nesting depth (from ParentSessionID) and whether
// it's the session currently loaded in this TUI instance.
type sessionTreeRow struct {
	id        string
	label     string
	depth     int
	isActive  bool
	agentInfo string // e.g. "spec_name · instance_id"
}

// sessionTreeState is the state of an open Ctrl+O session navigator - a nil
// *sessionTreeState on model means none is open, same nil-sentinel idiom as
// contextMenu.
type sessionTreeState struct {
	selected int
}

// openSessionTree opens the Ctrl+O session navigator. If the session cache
// is empty, fires the same silent background fetch maybePrefetchSessions
// uses for the /session and /resume argument completers (completions.go) -
// the existing SessionsListedEvent handler (events.go) already populates
// m.sessionSummaries and clears sessionsFetchInFlight regardless of Silent,
// so the overlay picks up the fresh list on its next render with no
// additional event-handling code.
func (m *model) openSessionTree() tea.Cmd {
	m.closeOtherExclusiveOverlays(overlaySessionTree)
	m.sessionTreeOverlay = &sessionTreeState{}
	if len(m.sessionSummaries) == 0 && !m.sessionsFetchInFlight {
		m.startSessionsFetch()
		return sendCommand(m.runtime, tauchat.ListSessionsCommand{Silent: true})
	}
	return nil
}

// openSessionTreeFresh opens the navigator for an explicit /session or
// /resume and always refreshes the list in the background, since a typed
// command implies the user wants the current sessions, not a stale cache.
func (m *model) openSessionTreeFresh() tea.Cmd {
	if cmd := m.openSessionTree(); cmd != nil {
		return cmd
	}
	if m.sessionsFetchInFlight {
		return nil
	}
	m.startSessionsFetch()
	return sendCommand(m.runtime, tauchat.ListSessionsCommand{Silent: true})
}

// sessionInfoPageLimit is the page size for the "/session info <id>" list
// refresh - larger than the store's default page so the common case resolves
// in a single round trip; sessions beyond it are reached by following
// NextCursor (see resolveSessionInfo).
const sessionInfoPageLimit = 100

// requestSessionInfo starts answering "/session info <id>" from a freshly
// fetched session list instead of the m.sessionSummaries cache, which can be
// stale or empty in a fresh TUI process (tau-px4). The ID rides in
// pendingSessionInfo; each SessionsListedEvent page resolves it in
// handleSessionEvent (events.go) via resolveSessionInfo.
func (m *model) requestSessionInfo(id string) tea.Cmd {
	m.pendingSessionInfo = id
	m.sessionsFetchAppend = false
	m.startSessionsFetch()
	return sendCommand(m.runtime, tauchat.ListSessionsCommand{Silent: true, Limit: sessionInfoPageLimit})
}

// resolveSessionInfo resolves a pending "/session info <id>" against one
// freshly delivered page of sessions. When the ID is not in this page and a
// next page exists, it fetches that page (appending it to the cache); when
// all pages are exhausted it prints the session's info or a now-trustworthy
// "not found" notification.
func (m *model) resolveSessionInfo(e tauchat.SessionsListedEvent) tea.Cmd {
	id := m.pendingSessionInfo
	for _, s := range e.Sessions {
		if s.ID == id {
			m.pendingSessionInfo = ""
			m.appendMessage("system", sessionInfoText(s))
			return nil
		}
	}
	if e.NextCursor != "" {
		m.sessionsFetchAppend = true
		m.startSessionsFetch()
		return sendCommand(m.runtime, tauchat.ListSessionsCommand{Silent: true, Limit: sessionInfoPageLimit, Cursor: e.NextCursor})
	}
	m.pendingSessionInfo = ""
	return m.setNotification("session not found: " + id)
}

// sessionTreeRows flattens m.sessionSummaries into a depth-first
// parent/child tree, mirroring sessionSummariesText's childrenOf/roots walk
// (render.go) but emitting rows for interactive navigation instead of a
// scrollback text blob.
func (m *model) sessionTreeRows() []sessionTreeRow {
	summaries := m.sessionSummaries
	byID := make(map[string]bool, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = true
	}
	childrenOf := make(map[string][]tauchat.SessionSummary)
	var roots []tauchat.SessionSummary
	for _, s := range summaries {
		if s.ParentSessionID != "" && byID[s.ParentSessionID] {
			childrenOf[s.ParentSessionID] = append(childrenOf[s.ParentSessionID], s)
		} else {
			roots = append(roots, s)
		}
	}

	var rows []sessionTreeRow
	var walk func(s tauchat.SessionSummary, depth int)
	walk = func(s tauchat.SessionSummary, depth int) {
		parts := []string{s.ModelID}
		if s.MessageCount > 0 {
			parts = append(parts, fmt.Sprintf("%d msgs", s.MessageCount))
		}
		parts = append(parts, relTime(s.UpdatedAt))
		label := sessionDisplayTitle(s) + "  " + strings.Join(parts, " · ")
		agentInfo := agentAttribution(s.AgentSpecName, s.AgentInstanceID)
		rows = append(rows, sessionTreeRow{id: s.ID, label: label, depth: depth, isActive: s.ID == m.sessionID, agentInfo: agentInfo})
		for _, child := range childrenOf[s.ID] {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	return rows
}

// handleSessionTreeKey handles keyboard input while the session navigator
// is open. Enter loads the selected session - the same LoadSessionCommand
// cmdSession's bare-arg case and cmdResume already send (commands.go).
func (m *model) handleSessionTreeKey(msg tea.KeyPressMsg) tea.Cmd {
	st := m.sessionTreeOverlay
	rows := m.sessionTreeRows()
	if st.selected < 0 || st.selected >= len(rows) {
		st.selected = 0
	}

	switch msg.String() {
	case "esc", "q":
		m.sessionTreeOverlay = nil
		return nil

	case "up":
		if st.selected > 0 {
			st.selected--
		}
		return nil
	case "down":
		if st.selected < len(rows)-1 {
			st.selected++
		}
		return nil

	case "enter":
		if len(rows) == 0 {
			return nil
		}
		id := rows[st.selected].id
		m.sessionTreeOverlay = nil
		return sendCommand(m.runtime, tauchat.LoadSessionCommand{SessionID: id})
	}
	return nil
}

// renderSessionTreeRow renders one tree row: an indent + corner glyph for
// nested sessions, the chevron/bold-selected convention shared with the
// completions dropdown and palette (compItemStyle/compSelectedStyle,
// styles.go), and a "(current)" suffix on the active session.
func renderSessionTreeRow(r sessionTreeRow, selected bool, width int) string {
	style := compItemStyle
	if selected {
		style = compSelectedStyle
	}
	label := r.label
	if r.isActive {
		label += " (current)"
	}
	if r.agentInfo != "" {
		label += "\n  " + r.agentInfo
	}
	return style.Render(label)
}

// renderSessionTreeOverlay renders the open session navigator: a scrolling
// window over sessionTreeRows (scrollWindow, shared with renderCompletions -
// render.go) wrapped in the same titled box convention as /help and the
// palette (renderBoxAround, input.go).
func (m *model) renderSessionTreeOverlay() string {
	st := m.sessionTreeOverlay
	width := paletteOverlayWidth(m.width)
	innerWidth := max(width-4, 20)

	rows := m.sessionTreeRows()
	if st.selected < 0 || st.selected >= len(rows) {
		st.selected = 0
	}

	var body []string
	switch {
	case len(rows) == 0 && m.sessionsFetchInFlight:
		body = append(body, compDescStyle.Render("loading sessions…"))
	case len(rows) == 0:
		body = append(body, compDescStyle.Render("no saved sessions"))
	default:
		start, end := scrollWindow(st.selected, len(rows), 10)
		if start > 0 {
			body = append(body, compMoreStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
		}
		for i := start; i < end; i++ {
			body = append(body, renderSessionTreeRow(rows[i], i == st.selected, innerWidth))
		}
		if remaining := len(rows) - end; remaining > 0 {
			body = append(body, compMoreStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)))
		}
	}

	return renderBoxAround(width, "Sessions", body)
}

// agentAttribution builds a human-readable agent identity string from the
// spec name and instance ID. Returns "" when there's no instance to attribute.
func agentAttribution(specName, instanceID string) string {
	if instanceID == "" {
		return ""
	}
	name := specName
	if name == "" {
		// Fallback: parse from instance_id format "specname#suffix"
		if before, _, ok := strings.Cut(instanceID, "#"); ok {
			name = before
		}
	}
	id := instanceID
	if _, after, ok := strings.Cut(instanceID, "#"); ok {
		id = after
	}
	return "agent " + name + " · " + id
}

// compositeSessionTreeOverlay overlays the open session navigator centered
// on top of base, following compositeHelpOverlay's lipgloss.Compositor
// shape (see compositeContextMenu's doc comment for why a bare Layer.Draw
// isn't enough).
func (m *model) compositeSessionTreeOverlay(base string) string {
	rendered := m.renderSessionTreeOverlay()
	bx, by := centerRect(m.width, m.height, lipgloss.Width(rendered), lipgloss.Height(rendered))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(rendered).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
