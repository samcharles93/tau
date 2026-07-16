package tui2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/providers"
)

func TestMain(m *testing.M) {
	notificationClearDelay = time.Millisecond
	notifyWarnDuration = time.Millisecond
	notifyInfoDuration = time.Millisecond
	os.Exit(m.Run())
}

// fakeRuntime is a tauchat.ChatRuntime that records every command sent to it,
// optionally failing every Send with a fixed error.
type fakeRuntime struct {
	mu   sync.Mutex
	sent []tauchat.ChatCommand
	err  error
}

func (f *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, cmd)
	return f.err
}

func (f *fakeRuntime) Close() {}

// drainCmd executes a tea.Cmd (recursively flattening tea.Batch) and returns
// every leaf tea.Msg it produces. nil commands/messages are skipped.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func newTestModel(rt tauchat.ChatRuntime, sub *eventbus.Subscriber[tauchat.ChatEvent]) *model {
	return newModel(context.Background(), rt, sub, "sess", "gpt", "openai", nil, nil, true, "medium", false, nil, "", false)
}

func TestBashSendFailureClearsRunning(t *testing.T) {
	rt := &fakeRuntime{err: errors.New("boom")}
	m := newTestModel(rt, nil)

	cmd := m.handleBashCommand("ls")
	msgs := drainCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from the send, got %d", len(msgs))
	}
	if _, ok := msgs[0].(bashSendResultMsg); !ok {
		t.Fatalf("expected bashSendResultMsg, got %T", msgs[0])
	}

	m.Update(msgs[0])

	if m.bashRunning {
		t.Error("expected bashRunning=false after a failed send - otherwise input is locked forever")
	}
	if m.bashCallID != "" {
		t.Error("expected bashCallID cleared after a failed send")
	}
}

func TestProviderLoginStartedCopiesDeviceCode(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	_, cmd := m.Update(providerLoginStartedMsg{
		providerID:  "github-copilot",
		displayName: "GitHub Copilot",
		session: providers.OAuthLoginSession{
			DeviceCode: providers.DeviceCode{
				VerificationURI: "https://github.com/login/device",
				UserCode:        "ABCD-1234",
			},
		},
		browserOpened: true,
	})
	if cmd == nil {
		t.Fatal("expected provider login start to return clipboard and polling commands")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a BatchMsg containing the clipboard Cmd, got %#v", cmd())
	}

	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.Kind() != reflect.String || v.String() != "ABCD-1234" {
		t.Fatalf("clipboard payload = %#v, want %q", clip, "ABCD-1234")
	}
	rendered := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(rendered, "Paste code (copied)") {
		t.Fatalf("expected copied-code instruction in rendered lines, got %q", rendered)
	}
}

// TestMessageRangesSkipMessagesWithoutID verifies a message with no ID (e.g.
// a pre-migration session loaded before per-message IDs existed) gets no
// range recorded - right-clicking it should simply find nothing, not panic
// or record a bogus entry.
func TestMessageRangesSkipMessagesWithoutID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "no id here"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})

	if len(m.messageRanges) != 0 {
		t.Fatalf("expected no message ranges for an ID-less message, got %+v", m.messageRanges)
	}
}

// TestMessageRangesShiftAfterCommittedGroupToggle guards the highest-risk
// part of per-message geometry: spliceCommittedGroup mutates renderedLines
// in place (the only site that does, besides pure trailing appends) when a
// committed tool group folds/unfolds, and must shift every messageRange
// that comes after it by the same delta it already applies to other
// committedGroups' lineIdx - otherwise a message after the toggled group
// silently desyncs and a later right-click resolves to the wrong message.
func TestMessageRangesShiftAfterCommittedGroupToggle(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"},
		{
			Role: tauchat.ChatRoleAssistant,
			ToolCalls: []tauchat.ChatToolCall{
				{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-2", Function: tauchat.ChatFunctionCall{Name: "read"}},
			},
		},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-2", Content: "b"},
		{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "done"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})

	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group, got %d", len(m.committedGroups))
	}
	g := m.committedGroups[0]

	var before messageLineRange
	for _, r := range m.messageRanges {
		if r.id == "a1" {
			before = r
		}
	}
	if before.id == "" {
		t.Fatalf("expected a1's range to exist before toggling, ranges: %+v", m.messageRanges)
	}
	if before.startLine <= g.lineIdx {
		t.Fatalf("test setup invalid: a1 (startLine=%d) must come after the group (lineIdx=%d)", before.startLine, g.lineIdx)
	}

	oldLineCount := g.lineCount
	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected the header click to be handled")
	}
	delta := g.lineCount - oldLineCount
	if delta == 0 {
		t.Fatal("test setup invalid: unfolding a 2-tool group must change its line count")
	}

	var after messageLineRange
	for _, r := range m.messageRanges {
		if r.id == "a1" {
			after = r
		}
	}
	if after.startLine != before.startLine+delta || after.endLine != before.endLine+delta {
		t.Fatalf("a1 range after toggle = %+v, want startLine=%d endLine=%d (before %+v shifted by delta=%d)",
			after, before.startLine+delta, before.endLine+delta, before, delta)
	}

	// messageAtRow must still resolve to a1 at its new (shifted) position -
	// these test messages are short enough (width 80) that no line
	// soft-wraps, so a renderedLines index and a screen-row offset coincide.
	id, ok := m.messageAtRow(after.startLine)
	if !ok || id != "a1" {
		t.Fatalf("messageAtRow(%d) = (%q, %v), want (a1, true)", after.startLine, id, ok)
	}
}

// TestScrollUpDuringResponseIsNotUndoneByRender guards against a real bug:
// computeLayout forced the viewport back to the bottom on every render while
// m.inResponse was true, so a manual scroll-up made during a live turn got
// stomped by the very next tick-driven re-render - the user couldn't scroll
// at all while the agent was working.
func TestScrollUpDuringResponseIsNotUndoneByRender(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 1; i <= 60; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.inResponse = true
	m.View()

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 5, Y: 5})
	offsetAfterScroll := m.viewport.YOffset()
	if offsetAfterScroll <= 0 {
		t.Fatalf("expected wheel-up to move the viewport, offset stayed at %d", offsetAfterScroll)
	}

	// Simulate the next tick-driven re-render that happens continuously
	// while a response streams in.
	m.View()

	if got := m.viewport.YOffset(); got != offsetAfterScroll {
		t.Fatalf("YOffset = %d, want manual scroll preserved at %d during an in-flight response", got, offsetAfterScroll)
	}
}

// containsFaintSGR reports whether s contains the ANSI "faint" SGR code (2),
// either alone (\x1b[2m) or combined with other attributes in one escape
// (e.g. \x1b[2;3m for faint+italic).
func containsFaintSGR(s string) bool {
	for _, seq := range regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllString(s, -1) {
		body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
		if slices.Contains(strings.Split(body, ";"), "2") {
			return true
		}
	}
	return false
}

func indexOfSubstring(lines []string, needle string) int {
	for i, l := range lines {
		if strings.Contains(l, needle) {
			return i
		}
	}
	return -1
}

// drainCmdMsg runs cmd and returns its single tea.Msg (nil-safe).
func drainCmdMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestUpdateSendResultMsgError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true

	m.Update(sendResultMsg{err: errIntentional})

	if m.inResponse {
		t.Fatal("inResponse should be false after send failure")
	}
}

func TestUpdateSendResultMsgSuccess(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.Update(sendResultMsg{err: nil})

	// No state change expected on success.
}

func TestUpdateBashSendResultMsgError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.bashCallID = "bash-123"

	m.Update(bashSendResultMsg{err: errIntentional})

	if m.bashRunning {
		t.Fatal("bashRunning should be false after send failure")
	}
	if m.bashCallID != "" {
		t.Fatal("bashCallID should be cleared after send failure")
	}
}

func TestUpdateWindowSizeMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Fatal("expected nil Cmd from WindowSizeMsg")
	}
	if m.width != 100 {
		t.Fatalf("width = %d, want 100", m.width)
	}
	if m.height != 30 {
		t.Fatalf("height = %d, want 30", m.height)
	}
	if m.maxViewportHeight != 23 {
		t.Fatalf("maxViewportHeight = %d, want 23", m.maxViewportHeight)
	}
}

// TestViewPinsInputAreaToBottomInAltScreen verifies short conversations sit
// just above the pinned input/status chrome, with blank space above.
func TestViewPinsInputAreaToBottomInAltScreen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.appendMessage("user", "hi")

	view := m.View()

	if got := m.viewport.Height(); got != 1 {
		t.Fatalf("viewport height = %d, want 1 for short bottom-aligned content", got)
	}

	lines := strings.Split(view.Content, "\n")
	if len(lines) != 40 {
		t.Fatalf("rendered view = %d lines, want 40 (fill terminal)", len(lines))
	}
	plain := stripANSI(view.Content)
	for _, want := range []string{"⏎ hi", "╭ chat", "╰"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered view missing %q:\n%s", want, plain)
		}
	}

	hiLine := lineContaining(lines, "⏎ hi")
	if hiLine < 30 {
		t.Fatalf("content line = %d, expected short content near the bottom", hiLine)
	}
}

func TestViewKeepsCompletedResponseAtStreamingPosition(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.mdCache = nil
	m.inResponse = true
	m.streaming = "hello world"

	streamingLines := strings.Split(m.View().Content, "\n")
	streamingLine := lineContaining(streamingLines, "hello world")
	if streamingLine < 0 {
		t.Fatalf("streaming text missing:\n%s", stripANSI(strings.Join(streamingLines, "\n")))
	}

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	completedLines := strings.Split(m.View().Content, "\n")
	completedLine := lineContaining(completedLines, "hello world")
	if completedLine < 0 {
		t.Fatalf("completed text missing:\n%s", stripANSI(strings.Join(completedLines, "\n")))
	}
	if completedLine != streamingLine {
		t.Fatalf("completed response moved from line %d to line %d", streamingLine, completedLine)
	}
}

func TestViewStreamsOverflowLikeBottomFollowingScroll(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.mdCache = nil
	m.inResponse = true

	var streamed strings.Builder
	for i := 1; i <= 60; i++ {
		if i > 1 {
			streamed.WriteString("\n")
		}
		streamed.WriteString(fmt.Sprintf("stream %02d", i))
	}
	m.streaming = streamed.String()

	streamingView := m.View()
	streamingLines := strings.Split(streamingView.Content, "\n")
	if lineContaining(streamingLines, "stream 01") >= 0 {
		t.Fatalf("oldest streaming line should have left through the top:\n%s", stripANSI(streamingView.Content))
	}
	latestStreamingLine := lineContaining(streamingLines, "stream 60")
	if latestStreamingLine < 0 {
		t.Fatalf("latest streaming line missing:\n%s", stripANSI(streamingView.Content))
	}
	// Title stays "chat" (not "steer") here - a response in flight no longer
	// implies steering by default; only the Ctrl+S hotkey (m.steering) does.
	inputLine := lineContaining(streamingLines, "╭ chat")
	if inputLine < 0 {
		t.Fatalf("chat input box missing:\n%s", stripANSI(streamingView.Content))
	}
	if latestStreamingLine >= inputLine {
		t.Fatalf("latest streaming line %d should appear above input line %d", latestStreamingLine, inputLine)
	}

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	completedView := m.View()
	completedLines := strings.Split(completedView.Content, "\n")
	if lineContaining(completedLines, "stream 01") >= 0 {
		t.Fatalf("oldest completed line should remain out of view:\n%s", stripANSI(completedView.Content))
	}
	latestCompletedLine := lineContaining(completedLines, "stream 60")
	if latestCompletedLine != latestStreamingLine {
		t.Fatalf("latest line moved from %d while streaming to %d after completion", latestStreamingLine, latestCompletedLine)
	}
}

func TestViewCapsViewportAtMaxHeightForLongContent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for range 100 {
		m.appendMessage("user", "line")
	}

	m.View()

	// 100 lines overflow the available region; the viewport should expand to
	// fill the terminal minus the (always-reserved, 2-line) notification
	// area, separator, padded input block, and status.
	if got := m.viewport.Height(); got != 31 {
		t.Fatalf("viewport height = %d, want 31 (fill terminal minus chrome)", got)
	}
}

func TestViewPreservesIdleManualScrollback(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for i := 1; i <= 80; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.View()
	// Scroll via the real key path (not m.viewport.ScrollUp directly) so
	// autoFollow - which now gates GotoBottom instead of inResponse/
	// PastBottom - actually clears, matching what a real manual scroll does.
	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	offset := m.viewport.YOffset()

	view := m.View()

	if got := m.viewport.YOffset(); got != offset {
		t.Fatalf("YOffset = %d, want preserved manual scrollback offset %d", got, offset)
	}
	if lineContaining(strings.Split(view.Content, "\n"), "line 80") >= 0 {
		t.Fatalf("latest line should not be forced into view while idle after manual scroll:\n%s", stripANSI(view.Content))
	}
}

func TestViewClampsIdleViewportAfterChromeShrinks(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for i := 1; i <= 80; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.notification = "temporary notice"
	m.View()
	m.viewport.GotoBottom()
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial view to start at bottom")
	}

	m.notification = ""
	view := m.View()

	if m.viewport.PastBottom() {
		t.Fatalf("viewport remained past bottom after chrome shrank: offset=%d", m.viewport.YOffset())
	}
	if lineContaining(strings.Split(view.Content, "\n"), "line 80") < 0 {
		t.Fatalf("latest line should remain visible after clamping to bottom:\n%s", stripANSI(view.Content))
	}
}

func TestViewEnablesMouseTracking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.appendMessage("user", "hello")

	view := m.View()

	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("MouseMode = %v, want MouseModeCellMotion", view.MouseMode)
	}
}

func TestMouseWheelScrollsViewport(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 1; i <= 40; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.View()
	m.viewport.GotoBottom()

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 5})

	if m.viewport.YOffset() <= 0 {
		t.Fatalf("expected viewport to scroll up on wheel-up, offset stayed at %d", m.viewport.YOffset())
	}
}

// TestComputeLayoutRowsMatchRenderedContent guards against the row-drift bug
// class where computeLayout's own row bookkeeping disagrees with what
// actually lands on screen. Earlier mouse-hit tests fed computeLayout's
// coordinates straight back into hit-testing, which stays self-consistent
// even if the underlying math is wrong - this test instead cross-checks
// geom's row numbers against real lines split out of m.View().Content, with
// every optional chrome section (tools, prompt, completions) simultaneously
// present so any drift accumulated across sections shows up.
func TestComputeLayoutRowsMatchRenderedContent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.input = "/"
	m.inputCursor = len([]rune(m.input))
	m.syncCompletionSelection()

	view := m.View()
	geom := m.computeLayout()
	lines := strings.Split(view.Content, "\n")

	line := func(y int) string {
		if y < 0 || y >= len(lines) {
			t.Fatalf("row %d out of range (rendered content has %d lines)", y, len(lines))
		}
		return stripANSI(lines[y])
	}

	toolBoxLines := strings.Join(lines[geom.toolsStartY:geom.toolsEndY+1], "\n")
	if got := stripANSI(toolBoxLines); !strings.Contains(got, "read") {
		t.Fatalf("geom.toolsStartY..toolsEndY=%d..%d, rendered lines = %q, want the tool box title somewhere inside", geom.toolsStartY, geom.toolsEndY, got)
	}
	inputAreaLines := strings.Join(lines[geom.inputStartY:geom.inputEndY+1], "\n")
	if got := stripANSI(inputAreaLines); !strings.Contains(got, "/") {
		t.Fatalf("geom.inputStartY..inputEndY=%d..%d, rendered lines = %q, want the typed text somewhere inside", geom.inputStartY, geom.inputEndY, got)
	}
	if got := line(geom.statusY); !strings.Contains(got, "τ") {
		t.Fatalf("geom.statusY=%d, rendered line = %q, want it to contain the status bar identity segment", geom.statusY, got)
	}
}

func lineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(stripANSI(line), needle) {
			return i
		}
	}
	return -1
}

func TestUpdateFocusMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.focused = false

	m.Update(tea.FocusMsg{})
	if !m.focused {
		t.Fatal("focused should be true after FocusMsg")
	}
}

func TestUpdateBlurMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.focused = true

	m.Update(tea.BlurMsg{})
	if m.focused {
		t.Fatal("focused should be false after BlurMsg")
	}
}

// promptPrefixWidth mirrors renderInputArea's own prefixWidth computation
// for the non-steering "> " prompt, so tests can compute expected click
// columns without hardcoding a width that would silently drift if the
// prefix ever changes.
func promptPrefixWidth() int {
	return visibleWidth(stripANSI(inputPromptStyle.Render("> ")))
}

func TestUpdateUnhandledMsgReturnsNilCmd(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(struct{}{})
	if cmd != nil {
		t.Fatal("expected nil Cmd from unknown message type")
	}
}

func TestUpdateQuitMsgReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(tea.QuitMsg{})
	if cmd != nil {
		t.Fatal("expected nil Cmd from QuitMsg")
	}
}
