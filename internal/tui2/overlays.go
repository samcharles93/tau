package tui2

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// contextMenuTarget identifies what kind of element a context menu was
// opened against - tool calls have two distinct on-screen forms (see
// contextMenuTargetTool vs contextMenuTargetToolRow), because a live,
// uncommitted tool box and a committed group's unfolded per-tool row are
// resolved through completely different hit-testing paths.
type contextMenuTarget int

const (
	contextMenuTargetNone    contextMenuTarget = iota
	contextMenuTargetTool                      // a live geom.toolBoxes entry, or a whole (folded or unfolded) committed group
	contextMenuTargetToolRow                   // one row inside an unfolded committed group
	contextMenuTargetMessage
)

// contextMenuAction identifies what a menu item does when activated.
type contextMenuAction int

const (
	contextMenuActionCopy contextMenuAction = iota
	contextMenuActionToggleExpand
	contextMenuActionViewDiff
)

// contextMenuItem is one selectable row in an open context menu. label is
// computed at build time (e.g. "Expand" vs "Collapse" reflects current
// state), not recomputed on every render, so the menu's contents stay
// stable while it's open even if something else changes state underneath.
type contextMenuItem struct {
	label  string
	action contextMenuAction
}

// contextMenu is the state of an open right-click menu. A nil *contextMenu
// on model means no menu is open - mirrors activePrompt's nil-sentinel
// idiom. x/y are the raw click position; positioning/clamping onto the
// screen happens at render/hit-test time (see clampContextMenuPosition),
// not here, so this struct stays purely "what was clicked and what can be
// done about it."
type contextMenu struct {
	target   contextMenuTarget
	targetID string
	x, y     int
	items    []contextMenuItem
	selected int
}

// diffViewerState is the state of an open "View diff" overlay. A nil
// *diffViewerState on model means no viewer is open - mirrors contextMenu's
// nil-sentinel idiom.
type diffViewerState struct {
	title    string
	viewport viewport.Model
}

type pluginPanel struct {
	id      string
	title   string
	content string
}

// compositeContextMenu overlays the open context menu on top of base using a
// lipgloss Compositor - tui2's only (x,y)-stamping compositing path, used
// nowhere else, since every other piece of chrome is flow-laid-out below
// the viewport rather than floating on top of arbitrary content. Compositing
// happens at the decoded-cell level (via lipgloss.Layer's underlying
// uv.StyledString), so this never corrupts either base's or menuStr's ANSI
// regardless of what's already styled underneath.
//
// This must go through a Compositor, not bare Canvas.Compose(layer) calls -
// Canvas.Compose(drawer) always calls drawer.Draw(canvas, canvas.Bounds()),
// and a bare Layer.Draw ignores its own X/Y and just stamps its content
// starting at whatever area it's handed, filling that entire area (blanking
// anything beyond its own content). A Layer's X/Y/Z only get honored once
// it's inside a Compositor, which precomputes each layer's true absolute
// bounds before drawing it.
func (m *model) compositeContextMenu(base string) string {
	menuStr := renderContextMenu(m.contextMenu.items, m.contextMenu.selected)
	mx, my := m.clampContextMenuPosition(menuStr)

	// Compositor.Render() would auto-size the canvas to the union of layer
	// bounds, which could come up short of the real terminal size if base's
	// widest line is narrower than m.width - pin it explicitly instead, so
	// the composited frame always matches what bubbletea expects.
	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(menuStr).X(mx).Y(my).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}

// clampContextMenuPosition returns where the open menu should actually be
// drawn: anchored at its raw click position (m.contextMenu.x/y), flipped to
// the opposite corner when it would overflow the right or bottom edge, then
// hard-clamped into the terminal bounds as a safety net for a menu wider or
// taller than the terminal itself (which the flip alone doesn't guarantee).
// Shared by compositeContextMenu (render) and handleContextMenuClick
// (click-away hit-test) so the two can never disagree about where the menu
// actually is - same rationale as computeLayout being the single source of
// truth for the rest of the screen.
func (m *model) clampContextMenuPosition(menuStr string) (x, y int) {
	mw := lipgloss.Width(menuStr)
	mh := lipgloss.Height(menuStr)
	x, y = m.contextMenu.x, m.contextMenu.y

	if x+mw > m.width {
		x = m.contextMenu.x - mw
	}
	if y+mh > m.height {
		y = m.contextMenu.y - mh + 1
	}
	x = max(0, min(x, max(0, m.width-mw)))
	y = max(0, min(y, max(0, m.height-mh)))
	return x, y
}

// renderContextMenu renders an open context menu's items as a bordered box,
// mirroring renderPrompt/renderCompletions' signature shape. The selected
// row gets the same "▶ " chevron + bold-foreground convention as the
// completions dropdown, for a consistent selection idiom across tui2's
// keyboard-navigable lists.
func renderContextMenu(items []contextMenuItem, selected int) string {
	lines := make([]string, len(items))
	for i, item := range items {
		if i == selected {
			lines[i] = "▶ " + contextMenuSelectedStyle.Render(item.label)
		} else {
			lines[i] = "  " + item.label
		}
	}
	return contextMenuStyle.Render(strings.Join(lines, "\n"))
}

// handleContextMenuClick resolves a left-click while a context menu is open
// against the menu's clamped on-screen bounds (see clampContextMenuPosition
// - the same function compositeContextMenu uses to render it, so hit-test
// and render can never disagree about where the menu actually is). A click
// inside the menu activates the item under it; a click anywhere else closes
// the menu without performing the click's underlying region action.
func (m *model) handleContextMenuClick(x, y int) tea.Cmd {
	menuStr := renderContextMenu(m.contextMenu.items, m.contextMenu.selected)
	mx, my := m.clampContextMenuPosition(menuStr)
	mw, mh := lipgloss.Width(menuStr), lipgloss.Height(menuStr)

	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		m.contextMenu = nil
		return nil
	}

	// contextMenuStyle draws a 1-cell border on every side, so the first
	// content row (item index 0) sits at my+1, not my.
	itemIdx := y - my - 1
	if itemIdx < 0 || itemIdx >= len(m.contextMenu.items) {
		m.contextMenu = nil
		return nil
	}
	return m.activateContextMenuItem(m.contextMenu.items[itemIdx])
}

// openContextMenuAt resolves the element under (x, y) - a live tool box or a
// committed tool group/row (chat messages are added in a later change via
// messageAtRow) - and opens a context menu for it. Mirrors handleMousePress's
// region dispatch but queries state rather than mutating it. A click on
// input/status/empty space, or on nothing resolvable, leaves the menu
// closed (silent no-op), matching how right-click on those regions is
// already a no-op today.
func (m *model) openContextMenuAt(x, y int) {
	geom := m.computeLayout()

	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		for _, tb := range geom.toolBoxes {
			if y < tb.startY || y > tb.endY {
				continue
			}
			m.contextMenu = m.buildLiveToolContextMenu(tb.id, x, y)
			return
		}
		return
	}

	if y >= geom.viewportStartY && y <= geom.viewportEndY {
		row := m.viewport.YOffset() + (y - geom.viewportStartY)
		idx, ok := m.logicalLineAtRow(row)
		if !ok {
			return
		}
		if cm := m.buildCommittedToolContextMenu(idx, x, y); cm != nil {
			m.contextMenu = cm
			return
		}
		if id, ok := m.messageAtRow(row); ok {
			m.contextMenu = m.buildMessageContextMenu(id, x, y)
			return
		}
		return
	}
	// input/status/empty space: no target, menu stays closed.
}

// buildMessageContextMenu builds a menu for a right-click on a chat message.
func (m *model) buildMessageContextMenu(id string, x, y int) *contextMenu {
	return &contextMenu{
		target:   contextMenuTargetMessage,
		targetID: id,
		x:        x,
		y:        y,
		items: []contextMenuItem{
			{label: "Copy", action: contextMenuActionCopy},
		},
	}
}

// buildLiveToolContextMenu builds a menu for a right-click on the live
// (uncommitted) tool batch's tool with the given id.
func (m *model) buildLiveToolContextMenu(id string, x, y int) *contextMenu {
	i := m.findLiveTool(id)
	if i < 0 {
		return nil
	}
	expandLabel := "Expand"
	if m.expandedID == id {
		expandLabel = "Collapse"
	}
	items := []contextMenuItem{
		{label: "Copy output", action: contextMenuActionCopy},
		{label: expandLabel, action: contextMenuActionToggleExpand},
	}
	if toolSupportsDiffView(m.tools[i]) {
		items = append(items, contextMenuItem{label: "View diff", action: contextMenuActionViewDiff})
	}
	return &contextMenu{
		target:   contextMenuTargetTool,
		targetID: id,
		x:        x,
		y:        y,
		items:    items,
	}
}

// toolSupportsDiffView reports whether t's context menu should offer a "View
// diff" item - true for edit/write tool calls that carry populated
// tools.DiffDetails.
func toolSupportsDiffView(t toolState) bool {
	if t.name != "edit" && t.name != "write" {
		return false
	}
	_, ok := t.details.(tools.DiffDetails)
	return ok
}

// buildCommittedToolContextMenu resolves renderedLines index idx against
// m.committedGroups and builds the appropriate menu - a single row's menu if
// idx lands on a specific tool row within an unfolded multi-tool group, or
// the whole group's menu otherwise. Mirrors toggleCommittedToolAtLine's same
// fold/row-expand precedence exactly, so right-click and left-click always
// agree on what's "under" a given line. Returns nil if idx isn't inside any
// committed group.
func (m *model) buildCommittedToolContextMenu(idx, x, y int) *contextMenu {
	for _, g := range m.committedGroups {
		if idx < g.lineIdx || idx >= g.lineIdx+g.lineCount {
			continue
		}
		if g.expanded && len(g.tools) > 1 {
			rel := idx - g.lineIdx
			if _, rows := m.renderToolGroupBox(g.tools, g.expandedID, -1, m.width); rows != nil {
				for _, tb := range rows {
					if rel < tb.startY || rel > tb.endY {
						continue
					}
					return m.buildToolRowContextMenu(g, tb.id, x, y)
				}
			}
		}
		return m.buildCommittedGroupContextMenu(g, x, y)
	}
	return nil
}

// buildToolRowContextMenu builds a menu for one tool row inside an unfolded
// committed group.
func (m *model) buildToolRowContextMenu(g *committedToolGroup, toolID string, x, y int) *contextMenu {
	expandLabel := "Expand"
	if g.expandedID == toolID {
		expandLabel = "Collapse"
	}
	items := []contextMenuItem{
		{label: "Copy output", action: contextMenuActionCopy},
		{label: expandLabel, action: contextMenuActionToggleExpand},
	}
	for _, t := range g.tools {
		if t.id == toolID && toolSupportsDiffView(t) {
			items = append(items, contextMenuItem{label: "View diff", action: contextMenuActionViewDiff})
			break
		}
	}
	return &contextMenu{
		target:   contextMenuTargetToolRow,
		targetID: toolID,
		x:        x,
		y:        y,
		items:    items,
	}
}

// buildCommittedGroupContextMenu builds a menu for a whole committed group
// (folded, or a click that landed outside every row of an unfolded group -
// the header/border, same as the fold trigger). Uses the group's first
// tool's id as the menu's stable identity - findCommittedGroupWithTool
// resolves it back to this same group at activation time. Groups always
// have at least one tool.
func (m *model) buildCommittedGroupContextMenu(g *committedToolGroup, x, y int) *contextMenu {
	expandLabel := "Expand"
	if g.expanded {
		expandLabel = "Collapse"
	}
	items := []contextMenuItem{
		{label: "Copy output", action: contextMenuActionCopy},
		{label: expandLabel, action: contextMenuActionToggleExpand},
	}
	// Only offer "View diff" from the group-level menu for a single-tool
	// group - a multi-tool group's individual tools are reachable (and
	// disambiguated) via buildToolRowContextMenu once unfolded.
	if len(g.tools) == 1 && toolSupportsDiffView(g.tools[0]) {
		items = append(items, contextMenuItem{label: "View diff", action: contextMenuActionViewDiff})
	}
	return &contextMenu{
		target:   contextMenuTargetTool,
		targetID: g.tools[0].id,
		x:        x,
		y:        y,
		items:    items,
	}
}

// handleContextMenuKey handles keyboard input while a context menu is open.
// Up/Down wrap (matches focusNextTool's wraparound - a menu conventionally
// wraps, unlike the completions dropdown's clamped window). Enter activates
// the selected item and closes the menu. Esc performs a real, unconditional
// dismiss - unlike the completions dropdown's swallow-without-closing, this
// is an explicit modal the user deliberately opened. Every other key is
// swallowed while the menu is open rather than falling through to input
// editing.
func (m *model) handleContextMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	n := len(m.contextMenu.items)
	if n == 0 {
		m.contextMenu = nil
		return nil
	}
	switch msg.String() {
	case "up":
		m.contextMenu.selected = (m.contextMenu.selected - 1 + n) % n
	case "down":
		m.contextMenu.selected = (m.contextMenu.selected + 1) % n
	case "enter":
		return m.activateContextMenuItem(m.contextMenu.items[m.contextMenu.selected])
	case "esc":
		m.contextMenu = nil
	}
	return nil
}

// activateContextMenuItem performs item's action against the menu's current
// target and closes the menu. Re-resolves the target by id rather than
// caching a pointer/index at open time, so an action taken a while after
// the menu opened (or from a click, once click-to-activate lands) still
// acts on live state.
func (m *model) activateContextMenuItem(item contextMenuItem) tea.Cmd {
	cm := m.contextMenu
	m.contextMenu = nil
	if cm == nil {
		return nil
	}
	switch cm.target {
	case contextMenuTargetTool:
		return m.activateToolContextAction(cm.targetID, item.action)
	case contextMenuTargetToolRow:
		return m.activateToolRowContextAction(cm.targetID, item.action)
	case contextMenuTargetMessage:
		return m.activateMessageContextAction(cm.targetID, item.action)
	}
	return nil
}

// activateMessageContextAction performs action against the message
// identified by id, looked up in messageRanges for its raw (unstyled)
// content - Copy is currently the only message action.
func (m *model) activateMessageContextAction(id string, action contextMenuAction) tea.Cmd {
	if action != contextMenuActionCopy {
		return nil
	}
	for _, r := range m.messageRanges {
		if r.id == id {
			return m.copyText(r.content)
		}
	}
	return nil
}

// activateToolContextAction performs action against either the live tool or
// the whole committed group identified by id - whichever collection
// currently contains it (see findLiveTool/findCommittedGroupWithTool).
func (m *model) activateToolContextAction(id string, action contextMenuAction) tea.Cmd {
	if i := m.findLiveTool(id); i >= 0 {
		switch action {
		case contextMenuActionCopy:
			return m.copyText(m.tools[i].result)
		case contextMenuActionToggleExpand:
			if m.expandedID == id {
				m.expandedID = ""
			} else {
				m.expandedID = id
				m.tools[i].expanded = true
			}
		case contextMenuActionViewDiff:
			return m.openDiffViewer(m.tools[i])
		}
		return nil
	}
	if g := m.findCommittedGroupWithTool(id); g != nil {
		switch action {
		case contextMenuActionCopy:
			results := make([]string, len(g.tools))
			for i, t := range g.tools {
				results[i] = t.result
			}
			return m.copyText(strings.Join(results, "\n"))
		case contextMenuActionToggleExpand:
			g.expanded = !g.expanded
			if !g.expanded {
				g.expandedID = ""
			}
			m.spliceCommittedGroup(g)
		case contextMenuActionViewDiff:
			if len(g.tools) > 0 {
				return m.openDiffViewer(g.tools[0])
			}
		}
	}
	return nil
}

// activateToolRowContextAction performs action against one tool row within
// its committed group, identified by the row's own tool id.
func (m *model) activateToolRowContextAction(id string, action contextMenuAction) tea.Cmd {
	g := m.findCommittedGroupWithTool(id)
	if g == nil {
		return nil
	}
	switch action {
	case contextMenuActionCopy:
		for _, t := range g.tools {
			if t.id == id {
				return m.copyText(t.result)
			}
		}
	case contextMenuActionToggleExpand:
		if g.expandedID == id {
			g.expandedID = ""
		} else {
			g.expandedID = id
		}
		m.spliceCommittedGroup(g)
	case contextMenuActionViewDiff:
		for _, t := range g.tools {
			if t.id == id {
				return m.openDiffViewer(t)
			}
		}
	}
	return nil
}

// copyText copies text to the clipboard via OSC 52, matching copySelection's
// size-guard and notification pattern - context-menu Copy actions have no
// selectionState/bounds of their own to route through copySelection itself.
func (m *model) copyText(text string) tea.Cmd {
	if text == "" {
		return m.setNotification("nothing to copy")
	}
	if _, ok := termkit.OSC52Copy(text); !ok {
		return m.setNotification(fmt.Sprintf("selection too large to copy (over %d chars)", termkit.OSC52MaxBytes))
	}
	return tea.Batch(tea.SetClipboard(text), m.setNotification("copied to clipboard"))
}

// activePanel returns the first active plugin panel (if any).
func (m *model) activePanel() *pluginPanel {
	for _, p := range m.panels {
		return &p
	}
	return nil
}
