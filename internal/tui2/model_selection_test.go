package tui2

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestMouseClickFocusesAndExpandsTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{
		{id: "t1", name: "read", status: "done"},
		{id: "t2", name: "search", status: "done"},
	}

	geom := m.computeLayout()
	if len(geom.toolBoxes) != 2 {
		t.Fatalf("toolBoxes = %d, want 2", len(geom.toolBoxes))
	}

	// The expand toggle is a click action (press+release with no drag in
	// between) — see toggleToolBoxAtY — since press alone must instead arm
	// toolsSel in case the gesture turns into a drag-to-select.
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})

	if m.focusedTool != 0 {
		t.Fatalf("focusedTool = %d, want 0", m.focusedTool)
	}
	if m.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1", m.expandedID)
	}

	// Clicking the same box again collapses it.
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})
	if m.expandedID != "" {
		t.Fatalf("expandedID = %q, want empty after second click", m.expandedID)
	}

	// Clicking inside the viewport (above the tool boxes) clears focus.
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})
	if m.focusedTool != -1 {
		t.Fatalf("focusedTool = %d, want -1 after clicking viewport", m.focusedTool)
	}
}

func TestMousePressStartsSelectionInViewport(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 1; i <= 5; i++ {
		m.appendMessage("user", fmt.Sprintf("line %d", i))
	}
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})

	if !m.viewportSel.armed() || m.viewportSel.anchor != m.viewportSel.cursor {
		t.Fatalf("expected a single-line selection anchor after press, got anchor=%d cursor=%d", m.viewportSel.anchor, m.viewportSel.cursor)
	}
	if m.viewportSel.dragging {
		t.Fatal("dragging should be false right after a press, before any motion")
	}
}

// TestMouseClickOnCommittedToolGroupUnfolds drives the fold/unfold action
// through the real mouse press/release path (handleMousePress ->
// handleMouseRelease's dragViewport case -> toggleCommittedToolAtLine),
// rather than calling toggleCommittedToolAtLine directly, to prove the
// wiring itself — not just the toggle logic — works end to end.
func TestMouseClickOnCommittedToolGroupUnfolds(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done"},
		{id: "t1", name: "search", status: "done"},
	}, nil)
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})

	if cmd != nil {
		t.Fatal("expected folding/unfolding a tool group to be a pure re-render, not a Cmd")
	}
	if !m.committedGroups[0].expanded {
		t.Fatal("expected clicking the committed group's header line to unfold it")
	}
	if m.viewportSel.armed() {
		t.Fatal("expected the click to be consumed as a toggle, not left behind as an active selection")
	}
}

func TestMouseDragExtendsSelectionWithoutCopyingOnRelease(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 1; i <= 5; i++ {
		m.appendMessage("user", fmt.Sprintf("line %d", i))
	}
	m.View()

	geom := m.computeLayout()
	startY := geom.viewportStartY
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: startY})
	anchor := m.viewportSel.anchor

	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, Y: startY + 2})
	if !m.viewportSel.dragging {
		t.Fatal("expected dragging to become true once motion is seen")
	}
	if m.viewportSel.cursor == anchor {
		t.Fatalf("expected cursor to move away from anchor %d after drag, got %d", anchor, m.viewportSel.cursor)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: startY + 2})
	if cmd != nil {
		t.Fatal("release after a real drag should finalize the selection without copying")
	}
	if m.notification != "" {
		t.Fatalf("release after a real drag should not show a copy notification, got %q", m.notification)
	}
	if !m.viewportSel.armed() {
		t.Fatal("expected the highlight to remain after release so the user can inspect it before copying")
	}

	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: startY + 2})
	if cmd == nil {
		t.Fatal("expected right-click to copy the finalized selection")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a 'copied N lines' notification, got %q", m.notification)
	}
}

func TestMouseClickWithoutDragClearsSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 1; i <= 5; i++ {
		m.appendMessage("user", fmt.Sprintf("line %d", i))
	}
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.viewportStartY})

	if cmd != nil {
		t.Fatal("a plain click (no drag) should not trigger a clipboard copy")
	}
	if m.viewportSel.armed() {
		t.Fatalf("expected selection cleared after a no-drag release, got anchor=%d", m.viewportSel.anchor)
	}
}

func TestMousePressOnToolBoxClearsSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.viewportSel.anchor, m.viewportSel.cursor = 0, 2
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})

	if m.viewportSel.armed() {
		t.Fatalf("expected pressing a tool box to clear any active text selection, got anchor=%d", m.viewportSel.anchor)
	}
}

// TestMouseDragInToolBoxSelectsLinesForRightClickCopy covers the fix that made the
// live tool-call box selectable: it used to be pure chrome outside the
// viewport's drag-select regions, so nothing inside it could be copied.
func TestMouseDragInToolBoxSelectsLinesForRightClickCopy(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{
		{id: "t1", name: "read", status: "done", result: "alpha"},
		{id: "t2", name: "search", status: "done", result: "bravo"},
	}
	m.View()

	geom := m.computeLayout()
	startY := geom.toolsStartY
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: startY})
	if !m.toolsSel.armed() {
		t.Fatal("expected pressing inside the tool box to arm toolsSel")
	}

	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, Y: geom.toolsEndY})
	if !m.toolsSel.dragging {
		t.Fatal("expected dragging to become true once motion is seen")
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.toolsEndY})
	if cmd != nil {
		t.Fatal("release after a real drag should finalize the tool selection without copying")
	}
	// A plain click (no drag) inside the tool box must still toggle
	// expand/collapse rather than being swallowed by selection handling.
	if m.expandedID != "" {
		t.Fatalf("expected a real drag not to trigger the expand-toggle click action, got expandedID=%q", m.expandedID)
	}

	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.toolsEndY})
	if cmd == nil {
		t.Fatal("expected right-click to copy the finalized tool selection")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a 'copied N lines' notification, got %q", m.notification)
	}
}

func TestRightClickWithActiveSelectionCopiesInsteadOfOpeningMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.toolsStartY})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, Y: geom.toolsEndY})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.toolsEndY})
	if !m.toolsSel.armed() {
		t.Fatal("expected the drag to leave a finalized selection")
	}

	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.toolsEndY})
	if cmd == nil {
		t.Fatal("expected right-click with an active selection to copy it")
	}
	if m.contextMenu != nil {
		t.Fatal("expected right-click with an active selection not to open a context menu")
	}
}

func TestRightClickEmptySpaceDoesNotOpenMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.statusY})

	if m.contextMenu != nil {
		t.Fatalf("expected right-click on empty/non-target space not to open a menu, got %+v", m.contextMenu)
	}
}

func TestHighlightSelectionWrapsSelectedRange(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.renderedLines = []string{"alpha", "bravo", "charlie"}
	m.viewportSel.anchor, m.viewportSel.cursor = 1, 2

	lines := append([]string{}, m.renderedLines...)
	m.highlightSelection(lines)

	if !strings.Contains(lines[1], "\x1b[7m") || !strings.Contains(lines[1], "\x1b[27m") {
		t.Fatalf("expected line 1 wrapped in reverse video, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "\x1b[7m") {
		t.Fatalf("expected line 2 wrapped in reverse video, got %q", lines[2])
	}
	if strings.Contains(lines[0], "\x1b[7m") {
		t.Fatalf("line 0 is outside the selection and must not be highlighted, got %q", lines[0])
	}
}

// TestHighlightSelectionSurvivesEmbeddedReset covers a real visual bug: a
// styled line (e.g. glamour markdown with an inline-code span) contains its
// own SGR reset partway through — "\x1b[38;5;252mNow \x1b[m\x1b[38;5;203mgit
// status\x1b[m more text". Wrapping the whole line in a single
// "\x1b[7m...\x1b[27m" pair only visually highlights up to that first
// embedded reset, since a bare reset clears every attribute including
// reverse video — everything after it silently renders unhighlighted even
// though the line is genuinely selected and copies correctly. Reverse video
// must be re-asserted after each embedded reset so highlighting covers the
// whole line.
func TestHighlightSelectionSurvivesEmbeddedReset(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.renderedLines = []string{"\x1b[38;5;252mNow \x1b[m\x1b[38;5;203mgit status\x1b[m more text"}
	m.viewportSel.anchor, m.viewportSel.cursor = 0, 0

	lines := append([]string{}, m.renderedLines...)
	m.highlightSelection(lines)

	afterFirstReset := strings.SplitN(lines[0], "\x1b[m", 2)[1]
	if !strings.HasPrefix(afterFirstReset, "\x1b[7m") {
		t.Fatalf("expected reverse video re-asserted right after the line's first embedded reset, got %q", lines[0])
	}
}

func TestCopySelectionTooLargeShowsNotificationInsteadOfCopying(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	huge := strings.Repeat("x", termkit.OSC52MaxBytes+1)
	m.renderedLines = []string{huge}
	m.viewportSel.anchor, m.viewportSel.cursor, m.viewportSel.dragging = 0, 0, true

	drainCmd(m.copySelection(&m.viewportSel, m.viewportSelectionText, "line"))

	if !strings.Contains(m.notification, "too large to copy") {
		t.Fatalf("expected an oversized-selection notification, got %q", m.notification)
	}
}

func TestEscClearsActiveSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.viewportSel.anchor, m.viewportSel.cursor = 0, 1

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.viewportSel.armed() {
		t.Fatalf("expected Esc to clear the selection, got anchor=%d", m.viewportSel.anchor)
	}
}

// TestEscClearsToolsSelection guards against a real bug: Esc's "is anything
// selected" guard checked viewportSel/inputSel/statusSel but not toolsSel,
// so a stuck-armed tools selection (e.g. from a press that never saw a
// matching release) was never reachable via Esc. copyActiveSelection
// (selection.go) checks toolsSel among the four states, and right-click
// prefers an active selection over opening the context menu — so a stuck
// toolsSel silently hijacked every right-click into a copy, with no menu
// ever appearing and no visible way to escape it.
func TestEscClearsToolsSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.toolsSel.anchor, m.toolsSel.cursor = 0, 1

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.toolsSel.armed() {
		t.Fatalf("expected Esc to clear the tools selection, got anchor=%d", m.toolsSel.anchor)
	}
}

func TestWrappedRowCount(t *testing.T) {
	if got := wrappedRowCount("short", 80); got != 1 {
		t.Fatalf("wrappedRowCount(short) = %d, want 1", got)
	}
	long := strings.Repeat("x", 200)
	if got := wrappedRowCount(long, 80); got != 3 { // ceil(200/80)
		t.Fatalf("wrappedRowCount(long) = %d, want 3", got)
	}
}

func TestLogicalLineAtRowAcrossWrappedLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 10
	m.renderedLines = []string{
		"short",                 // 1 row  -> row 0
		strings.Repeat("x", 25), // 3 rows -> rows 1-3 (ceil(25/10))
		"end",                   // 1 row  -> row 4
	}

	tests := []struct {
		row     int
		wantIdx int
	}{
		{0, 0},
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
	}
	for _, tc := range tests {
		idx, ok := m.logicalLineAtRow(tc.row)
		if !ok || idx != tc.wantIdx {
			t.Fatalf("logicalLineAtRow(%d) = (%d, %v), want (%d, true)", tc.row, idx, ok, tc.wantIdx)
		}
	}

	if _, ok := m.logicalLineAtRow(99); ok {
		t.Fatal("expected logicalLineAtRow to report not-ok for a row past all content")
	}
}

func TestRenderInputChunkWithSelectionKeepsPlainText(t *testing.T) {
	ln := []rune("hello world")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), false, -1, true, 2, 7))
	if out != "hello world" {
		t.Fatalf("selected text = %q, want unchanged plain text %q", out, "hello world")
	}
	styled := renderInputChunk(ln, 0, len(ln), false, -1, true, 2, 7)
	if !strings.Contains(styled, "\x1b[7m") {
		t.Fatalf("expected the selected range to carry reverse video, got %q", styled)
	}
}

func TestMousePressInInputPositionsCursorAndArmsSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input = "hello world"
	m.View()

	geom := m.computeLayout()
	textStartCol := 1 + promptPrefixWidth()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: textStartCol + 5, Y: geom.inputStartY + 2})

	if m.inputCursor != 5 {
		t.Fatalf("inputCursor = %d, want 5", m.inputCursor)
	}
	if !m.inputSel.armed() || m.inputSel.anchor != 5 {
		t.Fatalf("expected input selection armed at 5, got anchor=%d", m.inputSel.anchor)
	}
}

func TestMouseDragInInputSelectsSubstringForRightClickCopy(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input = "hello world"
	m.View()

	geom := m.computeLayout()
	row := geom.inputStartY + 2
	textStartCol := 1 + promptPrefixWidth()

	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: textStartCol, Y: row})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: textStartCol + 5, Y: row})
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: textStartCol + 5, Y: row})

	if cmd != nil {
		t.Fatal("release after a real drag should finalize the input selection without copying")
	}
	if m.notification != "" {
		t.Fatalf("release after a real drag should not show a copy notification, got %q", m.notification)
	}
	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: textStartCol + 5, Y: row})
	if cmd == nil {
		t.Fatal("expected right-click to copy the finalized input selection")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied selection") {
		t.Fatalf("expected a 'copied selection' notification, got %q", m.notification)
	}
	if got := m.inputSelectionText(0, 5); got != "hello" {
		t.Fatalf("selected text = %q, want %q", got, "hello")
	}
}

func TestTypingReplacesActiveInputSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputSel.anchor, m.inputSel.cursor = 0, 5 // "hello" selected
	m.inputCursor = 5

	m.insertAtCursor("X")

	if m.input != "X world" {
		t.Fatalf("input = %q, want %q", m.input, "X world")
	}
	if m.inputSel.armed() {
		t.Fatal("expected the selection to be consumed by typing")
	}
}

func TestBackspaceConsumesActiveInputSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputSel.anchor, m.inputSel.cursor = 0, 5 // "hello" selected
	m.inputCursor = 5

	m.backspaceAtCursor()

	if m.input != " world" {
		t.Fatalf("input = %q, want %q (whole selection removed, not just one char)", m.input, " world")
	}
	if m.inputCursor != 0 {
		t.Fatalf("inputCursor = %d, want 0", m.inputCursor)
	}
}

func TestDeleteConsumesActiveInputSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputSel.anchor, m.inputSel.cursor = 6, 11 // "world" selected
	m.inputCursor = 6

	m.deleteAtCursor()

	if m.input != "hello " {
		t.Fatalf("input = %q, want %q", m.input, "hello ")
	}
}

func TestArrowKeyClearsInputSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputCursor = 5
	m.inputSel.anchor, m.inputSel.cursor = 0, 5

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeyLeft})

	if m.inputSel.armed() {
		t.Fatal("expected an arrow key to clear a stale input selection")
	}
}

func TestMouseDragInStatusBarSelectsSubstringForRightClickCopy(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.View()

	geom := m.computeLayout()
	plain := stripANSI(m.computeStatusBar())
	if len(plain) < 5 {
		t.Skip("status bar text too short in this test model to exercise a substring drag")
	}

	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: geom.statusY})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 3, Y: geom.statusY})
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 3, Y: geom.statusY})

	if cmd != nil {
		t.Fatal("release after a real drag should finalize the status selection without copying")
	}
	if m.notification != "" {
		t.Fatalf("release after a real drag should not show a copy notification, got %q", m.notification)
	}
	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 3, Y: geom.statusY})
	if cmd == nil {
		t.Fatal("expected right-click to copy the finalized status selection")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied selection") {
		t.Fatalf("expected a 'copied selection' notification, got %q", m.notification)
	}
}
