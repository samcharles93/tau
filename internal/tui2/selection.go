package tui2

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// dragRegion identifies which UI region a mouse press/drag/release is
// operating on, so a single input event stream can drive three independent
// selectionStates (viewport, input box, status bar) without them
// interfering.
type dragRegion int

const (
	dragNone dragRegion = iota
	dragViewport
	dragInput
	dragStatus
	dragTools
)

// selectionState is a press→drag→release text-selection gesture over some
// region's content, addressed by a single ordered integer position - a
// line index, a rune index, a column, whatever that region's own
// coordinate space is. Any UI region gets full drag-to-select behavior (via
// finalizeSelection) just by driving one of these plus a small
// position-mapping function and a text-extraction function, instead of
// hand-rolling its own anchor/cursor/dragging fields and copy logic -
// see viewportSel/inputSel/statusSel for the three current regions.
type selectionState struct {
	anchor   int // -1 = no selection
	cursor   int
	dragging bool
}

func newSelectionState() selectionState {
	return selectionState{anchor: -1, cursor: -1}
}

// clear drops any in-progress or finalized selection.
func (s *selectionState) clear() {
	s.anchor, s.cursor, s.dragging = -1, -1, false
}

// armed reports whether a press has staked an anchor (regardless of
// whether a real drag has extended it yet).
func (s *selectionState) armed() bool {
	return s.anchor >= 0
}

// press arms the anchor at pos, ready to become a drag.
func (s *selectionState) press(pos int) {
	s.anchor, s.cursor, s.dragging = pos, pos, false
}

// drag extends the selection to pos and marks the gesture as a real drag
// (as opposed to a plain click with no movement).
func (s *selectionState) drag(pos int) {
	s.dragging = true
	s.cursor = pos
}

// bounds returns the ordered [lo,hi] range, and false if there's no
// selection at all. Callers interpret lo/hi in their own domain - the
// viewport treats them as inclusive line indices, input/status as a
// half-open position range - selectionState itself is domain-agnostic.
func (s *selectionState) bounds() (lo, hi int, ok bool) {
	if s.anchor < 0 || s.cursor < 0 {
		return 0, 0, false
	}
	lo, hi = s.anchor, s.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// handleMousePress maps a left-button-down terminal position to a UI
// action: pressing on the live tool-call box arms its text selection anchor
// (and focuses whichever box, if any, sits under the cursor) - the actual
// focus/expand-toggle click action fires on release, once handleMouseRelease
// knows whether the gesture turned into a drag (see toggleToolBoxAtY);
// pressing in the viewport, input box, or status bar arms that region's
// text selection anchor the same way, in case the press turns into a drag
// (see dragRegion). Recomputing the layout here (rather than caching
// View()'s geometry) keeps hit-testing correct even if the event arrives
// before the next render - cheap enough since it only runs per mouse event,
// not per frame.
func (m *model) handleMousePress(x, y int) tea.Cmd {
	// A left-click while the /help overlay is open resolves against it -
	// see handleHelpOverlayClick - before anything underneath (context
	// menu, tool boxes) ever sees the click.
	if m.helpOverlay != nil {
		return m.handleHelpOverlayClick(x, y)
	}

	// A left-click while a context menu is open resolves against the menu
	// itself - inside its bounds activates whatever's under the click,
	// outside closes it - and never also fires the region's own press
	// action underneath (e.g. re-focusing a different tool box).
	if m.contextMenu != nil {
		return m.handleContextMenuClick(x, y)
	}

	geom := m.computeLayout()

	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.clearAllSelections()
		m.dragRegion = dragTools
		m.toolsSel.press(y)
		for _, tb := range geom.toolBoxes {
			if y >= tb.startY && y <= tb.endY {
				m.focusedTool = m.findLiveTool(tb.id)
				break
			}
		}
		return nil
	}

	switch {
	case y >= geom.viewportStartY && y <= geom.viewportEndY:
		m.clearAllSelections()
		m.focusedTool = -1
		m.expandedID = ""
		m.dragRegion = dragViewport
		row := m.viewport.YOffset() + (y - geom.viewportStartY)
		if idx, ok := m.logicalLineAtRow(row); ok {
			m.viewportSel.press(idx)
		}

	case y >= geom.inputStartY && y <= geom.inputEndY:
		m.clearAllSelections()
		m.dragRegion = dragInput
		pos := m.inputPositionAt(y-geom.inputStartY, x)
		m.inputCursor = pos
		m.inputSel.press(pos)

	case y == geom.statusY:
		m.clearAllSelections()
		m.dragRegion = dragStatus
		m.statusSel.press(x)

	default:
		m.clearAllSelections()
	}
	// The region's selectionState.dragging stays false until
	// handleMouseDrag actually sees motion - a plain click (press+release,
	// no movement) must not leave a stray highlight or copy anything.
	return nil
}

// handleMouseDrag extends whichever selection dragRegion says is active to
// the position under the mouse. For the viewport, it also auto-scrolls when
// the drag reaches the top/bottom edge - the whole point of building
// selection ourselves rather than relying on the terminal's native
// (single-screen, can't-scroll) selection: a selection can now extend
// across content beyond one screen. For the input box, the drag is clamped
// to its own rows even if the mouse leaves them, matching ordinary GUI
// text-field behavior (you can't drag-select out of the field you're in).
func (m *model) handleMouseDrag(x, y int) {
	// A context menu is never a drag target - defensive guard, since a
	// menu never sets m.dragRegion itself.
	if m.contextMenu != nil {
		return
	}
	switch m.dragRegion {
	case dragViewport:
		if !m.viewportSel.armed() {
			return
		}
		geom := m.computeLayout()
		switch {
		case y <= geom.viewportStartY:
			m.viewport.ScrollUp(1)
			m.autoFollow = false
		case y >= geom.viewportEndY:
			m.viewport.ScrollDown(1)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
		}
		rowInViewport := max(min(y-geom.viewportStartY, geom.viewportEndY-geom.viewportStartY), 0)
		row := m.viewport.YOffset() + rowInViewport
		if idx, ok := m.logicalLineAtRow(row); ok {
			m.viewportSel.drag(idx)
		}

	case dragInput:
		if !m.inputSel.armed() {
			return
		}
		geom := m.computeLayout()
		row := max(min(y, geom.inputEndY), geom.inputStartY) - geom.inputStartY
		pos := m.inputPositionAt(row, x)
		m.inputSel.drag(pos)
		m.inputCursor = pos

	case dragStatus:
		if !m.statusSel.armed() {
			return
		}
		m.statusSel.drag(x)

	case dragTools:
		if !m.toolsSel.armed() {
			return
		}
		m.toolsSel.drag(y)
	}
}

// handleMouseRelease finalizes whichever selection dragRegion says was active
// via the shared finalizeSelection path. A real drag leaves the highlight in
// place; copying is an explicit right-click action handled by
// copyActiveSelection.
func (m *model) handleMouseRelease() tea.Cmd {
	defer func() { m.dragRegion = dragNone }()

	switch m.dragRegion {
	case dragViewport:
		// A plain click (no drag) landing on a committed tool-call group is
		// the fold/unfold/row-expand action (see toggleCommittedToolAtLine),
		// not a selection - finalizeSelection's generic "no-op on click"
		// doesn't know about that, so it's checked here first. A click on
		// ordinary text still just clears the selection, same as before.
		if !m.viewportSel.dragging {
			m.toggleCommittedToolAtLine(m.viewportSel.anchor)
			m.viewportSel.clear()
			return nil
		}
		return m.finalizeSelection(&m.viewportSel, m.viewportSelectionText, "line")
	case dragInput:
		return m.finalizeSelection(&m.inputSel, m.inputSelectionText, "")
	case dragStatus:
		return m.finalizeSelection(&m.statusSel, m.statusSelectionText, "")
	case dragTools:
		// A plain click (no drag) is the pre-existing focus/expand-toggle
		// action, not a selection - finalizeSelection's generic "no-op on
		// click" doesn't know about that domain-specific behavior, so it's
		// handled here before falling back to the shared copy path for an
		// actual drag.
		if !m.toolsSel.dragging {
			m.toggleToolBoxAtY(m.toolsSel.anchor)
			m.toolsSel.clear()
			return nil
		}
		return m.finalizeSelection(&m.toolsSel, m.toolsSelectionText, "line")
	}
	return nil
}

// clearAllSelections drops every region's selection - used whenever an
// interaction (a new press, a tool-box click) makes all of them stale.
func (m *model) clearAllSelections() {
	m.viewportSel.clear()
	m.inputSel.clear()
	m.statusSel.clear()
	m.toolsSel.clear()
}

// finalizeSelection is the shared "mouse released" behavior for any
// selectionState: a real drag freezes the selected range so it stays
// highlighted for an explicit right-click copy; a plain click (no drag)
// clears the selection instead of leaving a stray highlight behind.
func (m *model) finalizeSelection(s *selectionState, extract func(lo, hi int) string, _ string) tea.Cmd {
	dragging := s.dragging
	s.dragging = false
	if !dragging {
		s.clear()
		return nil
	}
	lo, hi, ok := s.bounds()
	if !ok || extract(lo, hi) == "" {
		s.clear()
	}
	return nil
}

// copyActiveSelection copies whichever region currently has a finalized
// selection. Only one selection is normally armed at a time because every
// left-button press clears all other regions first.
func (m *model) copyActiveSelection() tea.Cmd {
	if cmd := m.copySelection(&m.viewportSel, m.viewportSelectionText, "line"); cmd != nil {
		return cmd
	}
	if cmd := m.copySelection(&m.inputSel, m.inputSelectionText, ""); cmd != nil {
		return cmd
	}
	if cmd := m.copySelection(&m.statusSel, m.statusSelectionText, ""); cmd != nil {
		return cmd
	}
	return m.copySelection(&m.toolsSel, m.toolsSelectionText, "line")
}

// statusSelectionText returns the substring of the status bar's plain
// (ANSI-stripped) text between columns lo and hi (half-open), for
// finalizeSelection. The status bar has no left border/prefix, so a screen
// column maps directly to a column in its plain text.
func (m *model) statusSelectionText(lo, hi int) string {
	runes := []rune(stripANSI(m.computeStatusBar()))
	lo = max(min(lo, len(runes)), 0)
	hi = max(min(hi, len(runes)), 0)
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

// The OSC 52 size guard mirrors legacy taui's /copy - tea.SetClipboard
// itself has no such guard, and many terminals silently drop or corrupt
// oversized payloads rather than truncating. unit names what was selected
// for the notification ("line" -> "copied 3 lines"); pass "" for a region
// where a count isn't meaningful ("copied selection").
func (m *model) copySelection(s *selectionState, extract func(lo, hi int) string, unit string) tea.Cmd {
	lo, hi, ok := s.bounds()
	if !ok {
		return nil
	}
	text := extract(lo, hi)
	if text == "" {
		return nil
	}

	if _, ok := termkit.OSC52Copy(text); !ok {
		return m.setNotification(fmt.Sprintf("selection too large to copy (over %d chars)", termkit.OSC52MaxBytes))
	}
	notice := "copied selection"
	if unit != "" {
		n := hi - lo + 1
		u := unit
		if n != 1 {
			u += "s"
		}
		notice = fmt.Sprintf("copied %d %s", n, u)
	}
	return tea.Batch(tea.SetClipboard(text), m.setNotification(notice))
}

// highlightSelection wraps the selected range of lines (indices 0..
// len(m.renderedLines)-1 of lines, which viewportLinesForView builds from
// m.renderedLines first and unconditionally) in reverse-video SGR codes, in
// place. Reverse video composes with whatever foreground/background colors
// are already set in the styled line rather than requiring us to parse and
// re-inject them, so the line's normal styling still shows through,
// visually inverted. This only affects rendering - viewportSelectionText
// reads the unmodified m.renderedLines, so highlight quirks on complex
// multi-segment lines (rare - see comment there) never affect what gets
// copied.
func (m *model) highlightSelection(lines []string) {
	lo, hi, ok := m.viewportSel.bounds()
	if !ok {
		return
	}
	for i := lo; i <= hi && i < len(lines); i++ {
		lines[i] = reverseVideo(lines[i])
	}
}

// reverseVideoReset re-asserts reverse video after any SGR reset the line
// already contains internally - glamour ends each styled span (e.g. an
// inline-code span's colors) with a bare reset ("\x1b[m", equivalent to
// "\x1b[0m"), and a reset clears every active attribute, not just the one
// that span set. Without re-asserting, wrapping the whole line in
// "\x1b[7m...\x1b[27m" only highlights up to the line's first embedded
// reset - everything after it silently loses the reverse attribute, even
// though the whole line is genuinely selected and copies correctly (copy
// reads the unmodified, un-highlighted line via stripANSI).
var reverseVideoReset = strings.NewReplacer(
	"\x1b[m", "\x1b[m\x1b[7m",
	"\x1b[0m", "\x1b[0m\x1b[7m",
)

func reverseVideo(line string) string {
	return "\x1b[7m" + reverseVideoReset.Replace(line) + "\x1b[27m"
}

// viewportSelectionText returns the plain (ANSI-stripped) text of
// m.renderedLines[lo..hi] inclusive, for finalizeSelection.
func (m *model) viewportSelectionText(lo, hi int) string {
	if lo < 0 || hi >= len(m.renderedLines) {
		return ""
	}
	lines := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		lines = append(lines, stripANSI(m.renderedLines[i]))
	}
	return strings.Join(lines, "\n")
}

// toolsSelectionText returns the plain (ANSI-stripped) text of the live
// tool-call box's own lines between absolute view rows lo and hi inclusive
// (toolsSel presses/drags in that same coordinate space - see
// handleMousePress). The box isn't cached between frames like
// m.renderedLines, so this recomputes it fresh; cheap enough at
// mouse-release frequency.
func (m *model) toolsSelectionText(lo, hi int) string {
	geom := m.computeLayout()
	toolsStr, _ := m.renderToolGroup()
	if toolsStr == "" {
		return ""
	}
	boxLines := strings.Split(toolsStr, "\n")
	loRow, hiRow := lo-geom.toolsStartY, hi-geom.toolsStartY
	if loRow < 0 {
		loRow = 0
	}
	if hiRow >= len(boxLines) {
		hiRow = len(boxLines) - 1
	}
	if loRow > hiRow {
		return ""
	}
	lines := make([]string, 0, hiRow-loRow+1)
	for i := loRow; i <= hiRow; i++ {
		lines = append(lines, stripANSI(boxLines[i]))
	}
	return strings.Join(lines, "\n")
}

// wrappedRowCount estimates how many visual rows a styled line occupies
// once soft-wrapped to width, matching (approximately) how bubbles/viewport
// wraps it. This is character-width-based, not word-boundary-based like the
// viewport's actual wrap, so it can be off by a row for a long line that
// wraps near a word boundary. That only affects which whole line a
// click/drag lands on (logicalLineAtRow), never what gets copied once a
// line is selected, and most renderedLines entries are already pre-wrapped
// to width (glamour output, tool boxes) - the risk is concentrated in a
// long single-line, unwrapped user/system message.
func wrappedRowCount(line string, width int) int {
	if width < 1 {
		return 1
	}
	w := visibleWidth(stripANSI(line))
	if w == 0 {
		return 1
	}
	return (w + width - 1) / width
}

// logicalLineAtRow maps an absolute wrapped-row offset (0 = the viewport's
// first content row - the same coordinate space as m.viewport.YOffset()) to
// an index into m.renderedLines, accounting for lines that soft-wrap across
// more than one visual row (see wrappedRowCount's caveat).
func (m *model) logicalLineAtRow(targetRow int) (int, bool) {
	if targetRow < 0 {
		return 0, false
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	row := 0
	for i, line := range m.renderedLines {
		h := wrappedRowCount(line, width)
		if targetRow < row+h {
			return i, true
		}
		row += h
	}
	return 0, false
}

// messageAtRow maps an absolute wrapped-row offset (see logicalLineAtRow) to
// the id of the message whose recorded range contains it, if any. Used to
// resolve a right-click in the viewport to "which message" rather than just
// "which renderedLines index."
func (m *model) messageAtRow(row int) (string, bool) {
	idx, ok := m.logicalLineAtRow(row)
	if !ok {
		return "", false
	}
	for _, r := range m.messageRanges {
		if idx >= r.startLine && idx < r.endLine {
			return r.id, true
		}
	}
	return "", false
}
