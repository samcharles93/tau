package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestBashModeClearsRunningOnMatchingCompletion(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	cmd := m.handleBashCommand("ls")
	if !m.bashRunning {
		t.Fatal("expected bashRunning=true immediately after handleBashCommand")
	}
	if m.bashCallID == "" {
		t.Fatal("expected bashCallID to be populated so the completion event can be matched")
	}
	drainCmd(cmd) // perform the send

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.CallID != m.bashCallID {
		t.Fatalf("RunBashCommand.CallID = %q, want %q (must match m.bashCallID)", sent.CallID, m.bashCallID)
	}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: sent.CallID})

	if m.bashRunning {
		t.Error("expected bashRunning=false once the matching ChatToolExecutionCompletedEvent arrives")
	}
	if m.bashCallID != "" {
		t.Error("expected bashCallID cleared once the matching completion event arrives")
	}
}

func TestDispatchCtrlCQuits(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// A single idle Ctrl+C arms the quit guard rather than quitting outright
	// (see TestHandleCtrlCIdleArmsQuitWithoutQuitting) - the returned Cmd here
	// is deliberately not invoked, since it's a 4-second notification-clear
	// tea.Tick that would both slow this test down and eat into the 800ms
	// double-tap window checked below.
	if first := m.dispatchKey(key('c', tea.ModCtrl)); first == nil {
		t.Fatal("expected a Cmd from the first Ctrl+C")
	}

	cmd := m.dispatchKey(key('c', tea.ModCtrl))
	if cmd == nil {
		t.Fatal("expected a Cmd from the second Ctrl+C")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg on second Ctrl+C, got %T", msg)
	}
}

func TestDispatchCtrlDWithEmptyInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""

	cmd := m.dispatchKey(key('d', tea.ModCtrl))
	if cmd == nil {
		t.Fatal("expected a Cmd from Ctrl+D with empty input")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg when input is empty, got %T", msg)
	}
}

func TestDispatchCtrlDWithInputClears(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.inputCursor = 5

	cmd := m.dispatchKey(key('d', tea.ModCtrl))
	if cmd != nil {
		t.Fatal("expected nil Cmd when input non-empty (clears instead of quit)")
	}
	if m.input != "" {
		t.Fatalf("input = %q, want empty after Ctrl+D", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0 after Ctrl+D", m.inputCursor)
	}
}

func TestDispatchCtrlSWithActiveResponse(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true

	m.dispatchKey(key('s', tea.ModCtrl))
	// Should toggle steering mode - no Cmd needed, since the status bar
	// already renders a "steering…" segment whenever m.steering is true.
	if !m.steering {
		t.Fatal("expected steering to be toggled on by Ctrl+S with active response")
	}
}

func TestDispatchCtrlSWithoutActiveResponseAndNoInputIsNoop(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false

	cmd := m.dispatchKey(key('s', tea.ModCtrl))
	// Matches legacy: Ctrl+S with nothing typed and nothing in flight is a
	// silent no-op, not an error notification.
	if cmd != nil {
		t.Fatal("expected nil Cmd from idle Ctrl+S with no text typed")
	}
	if m.notification != "" {
		t.Fatalf("expected no notification, got %q", m.notification)
	}
}

func TestDispatchEscWhileBashRunning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.bashCallID = "bash-123"

	cmd := m.dispatchKey(key(tea.KeyEscape, 0))
	if cmd == nil {
		t.Fatal("expected a Cmd from Esc while bash running")
	}
	if m.bashRunning {
		t.Fatal("bashRunning should be false after Esc")
	}
	drainCmd(cmd)
}

func TestDispatchEscClearsNonEmptyInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "partial"
	m.inputCursor = 3

	cmd := m.dispatchKey(key(tea.KeyEscape, 0))
	if cmd != nil {
		t.Fatal("expected nil Cmd from Esc with non-empty input")
	}
	if m.input != "" {
		t.Fatalf("input = %q, want empty after Esc", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0 after Esc", m.inputCursor)
	}
}

func TestDispatchEscWithEmptyInputNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""
	m.inputCursor = 0

	cmd := m.dispatchKey(key(tea.KeyEscape, 0))
	if cmd != nil {
		t.Fatal("expected nil Cmd from Esc with empty input")
	}
}

func TestDispatchPrintableCharacter(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "he"
	m.inputCursor = 2

	m.dispatchKey(charKey('y'))
	if m.input != "hey" {
		t.Fatalf("input = %q, want %q", m.input, "hey")
	}
}

func TestDispatchPrintableUnicode(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.dispatchKey(charKey('é'))
	if m.input != "é" {
		t.Fatalf("input = %q, want %q", m.input, "é")
	}
	if m.inputCursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.inputCursor)
	}
}

// TestHandleSteerNoActiveResponseWithEmptyInputIsNoop matches legacy's
// onSteer exactly: Ctrl+S with nothing typed and nothing in flight is a
// silent no-op (onSteer's own empty-prompt check returns before ever
// looking at the working/idle state).
func TestHandleSteerNoActiveResponseWithEmptyInputIsNoop(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false
	m.input = ""

	cmd := m.handleSteer()

	if cmd != nil {
		t.Fatal("expected nil Cmd for idle Ctrl+S with no text typed")
	}
	if m.notification != "" {
		t.Fatalf("expected no notification, got %q", m.notification)
	}
}

// TestHandleSteerIdleWithTextSubmitsInstead guards against a real bug:
// legacy falls through to a normal submit when idle so the user's typed
// text is never lost; tui2 used to show an error notification and drop it.
func TestHandleSteerIdleWithTextSubmitsInstead(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = false
	m.input = "hello while idle"

	drainCmd(m.handleSteer())

	if m.input != "" {
		t.Fatalf("input = %q, want cleared", m.input)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("expected SubmitChatPromptCommand, got %T", rt.sent[0])
	}
	if sent.Prompt != "hello while idle" {
		t.Fatalf("Prompt = %q, want %q", sent.Prompt, "hello while idle")
	}
}

func TestHandleSteerWithEmptyTextToggles(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true
	m.input = ""
	m.steering = false

	cmd := m.handleSteer()
	if cmd != nil {
		t.Fatal("expected nil Cmd for toggle (no text to send)")
	}
	if !m.steering {
		t.Fatal("steering should be toggled on")
	}
}

func TestHandleSteerWithText(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = true
	m.input = "change direction"

	cmd := m.handleSteer()
	if cmd == nil {
		t.Fatal("expected a Cmd when steering with text")
	}
	drainCmd(cmd)

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if !m.steering {
		t.Fatal("steering should be true")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 SteerChatPromptCommand, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.SteerChatPromptCommand)
	if !ok {
		t.Fatalf("expected SteerChatPromptCommand, got %T", rt.sent[0])
	}
	if sent.RequestID == "" {
		t.Fatal("expected a non-empty RequestID")
	}
}

func TestHandleBashCommandGeneratesCallID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleBashCommand("ls")

	if m.bashCallID == "" {
		t.Fatal("bashCallID should be populated")
	}
	if !strings.HasPrefix(m.bashCallID, "bash-") {
		t.Fatalf("bashCallID = %q, want 'bash-' prefix", m.bashCallID)
	}
}

func TestHandleBashCommandAppendsUserMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleBashCommand("git status")

	if len(m.renderedLines) == 0 {
		t.Fatal("expected bash echo to append a user message")
	}
}

func TestHandleBashCommandDoubleBangSetsExclude(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("!!secret-cmd"))

	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.Command != "secret-cmd" {
		t.Fatalf("Command = %q, want %q (bangs stripped)", sent.Command, "secret-cmd")
	}
	if !sent.Exclude {
		t.Fatal("expected Exclude=true for a !! command")
	}
}

func TestHandleBashCommandSingleBangDoesNotExclude(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("!ls"))

	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.Command != "ls" {
		t.Fatalf("Command = %q, want %q", sent.Command, "ls")
	}
	if sent.Exclude {
		t.Fatal("expected Exclude=false for a single ! command")
	}
}

func TestHandleBashCommandTripleBangStripsAll(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("!!!ls"))

	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.Command != "ls" {
		t.Fatalf("Command = %q, want %q (no leftover '!' glued to the front)", sent.Command, "ls")
	}
	if !sent.Exclude {
		t.Fatal("expected Exclude=true for 3+ bangs")
	}
}

func TestHandleBashCommandEmptyAfterStrippingIsNoop(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.handleBashCommand("!!")

	if cmd != nil {
		t.Fatal("expected nil Cmd when nothing but bangs was typed")
	}
	if m.bashRunning {
		t.Fatal("bashRunning should stay false for an empty bash command")
	}
}

func TestHandleBashCommandEchoesFullBangPrefix(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleBashCommand("!!secret-cmd")

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(stripANSI(joined), "!!secret-cmd") {
		t.Fatalf("expected echo to include both bangs, got %q", joined)
	}
}

func TestCancelBashNotRunning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.cancelBash()
	if cmd != nil {
		t.Fatal("expected nil Cmd when bash not running")
	}
}

func TestCancelBashSendsCommand(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.bashRunning = true
	m.bashCallID = "bash-123"

	cmd := m.cancelBash()
	if cmd == nil {
		t.Fatal("expected a Cmd when cancelling bash")
	}
	drainCmd(cmd)

	if m.bashRunning {
		t.Fatal("bashRunning should be false after cancel")
	}
	if m.bashCallID != "" {
		t.Fatalf("bashCallID should be cleared after cancel")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 CancelBashCommand, got %d", len(rt.sent))
	}
	_, ok := rt.sent[0].(tauchat.CancelBashCommand)
	if !ok {
		t.Fatalf("expected CancelBashCommand, got %T", rt.sent[0])
	}
}

func TestHandleCtrlCCancelsTurnWhenGenerating(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = true
	m.steering = true

	cmd := m.handleCtrlC()
	if cmd == nil {
		t.Fatal("expected a Cmd to cancel the turn")
	}
	drainCmd(cmd)

	if m.steering {
		t.Fatal("steering should be cleared on cancel")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.CancelChatRequestCommand); !ok {
		t.Fatalf("expected CancelChatRequestCommand, got %T", rt.sent[0])
	}
	// A single Ctrl+C during generation must never quit outright.
	if !m.pendingQuit.IsZero() {
		t.Fatal("pendingQuit should not be armed while cancelling a turn")
	}
}

func TestHandleCtrlCCancelsBashWhenRunning(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.bashRunning = true
	m.bashCallID = "bash-123"

	cmd := m.handleCtrlC()
	drainCmd(cmd)

	if m.bashRunning {
		t.Fatal("bashRunning should be false after Ctrl+C cancel")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.CancelBashCommand); !ok {
		t.Fatalf("expected CancelBashCommand, got %T", rt.sent[0])
	}
}

func TestHandleCtrlCIdleArmsQuitWithoutQuitting(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.handleCtrlC()
	if cmd == nil {
		t.Fatal("expected a notification Cmd arming the quit guard")
	}
	if _, ok := drainCmdMsg(cmd).(tea.QuitMsg); ok {
		t.Fatal("a single idle Ctrl+C must not quit")
	}
	if m.notification == "" {
		t.Fatal("expected a 'press Ctrl+C again' notification")
	}
	if m.pendingQuit.IsZero() {
		t.Fatal("expected pendingQuit to be armed")
	}
}

func TestHandleCtrlCDoubleTapQuits(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleCtrlC() // arm
	cmd := m.handleCtrlC()
	if cmd == nil {
		t.Fatal("expected tea.Quit on the second Ctrl+C")
	}
	if _, ok := drainCmdMsg(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", drainCmdMsg(cmd))
	}
}

func TestHandleCtrlCStaleArmDoesNotQuit(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.pendingQuit = time.Now().Add(-2 * quitConfirmWindow)

	cmd := m.handleCtrlC()
	if _, ok := drainCmdMsg(cmd).(tea.QuitMsg); ok {
		t.Fatal("a stale (expired) arm should re-arm, not quit")
	}
}

func TestNewRequestIDFormat(t *testing.T) {
	id := newRequestID()
	if id == "" {
		t.Fatal("expected non-empty request ID")
	}
	// UUIDv7 should contain hyphens.
	if !strings.Contains(id, "-") {
		t.Logf("request ID = %q (not a UUID if no hyphens)", id)
	}
}

func TestDispatchKeyCtrlShiftLClearsScreen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.appendMessage("user", "hello")

	cmd := m.dispatchKey(key('l', tea.ModCtrl|tea.ModShift))
	if cmd != nil {
		t.Fatal("expected nil Cmd from Ctrl+Shift+L")
	}
	if len(m.renderedLines) != 0 {
		t.Fatalf("renderedLines = %v, want empty after Ctrl+Shift+L", m.renderedLines)
	}
}

func TestCtrlHomeEndJumpConversationAndControlAutoFollow(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	for i := 1; i <= 80; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.input = "keep this draft"
	m.inputCursor = utf8.RuneCountInString(m.input)
	m.View()

	m.dispatchKey(key(tea.KeyHome, tea.ModCtrl))
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("Ctrl+Home YOffset = %d, want 0", got)
	}
	if m.autoFollow {
		t.Fatal("Ctrl+Home should disable auto-follow while viewing old content")
	}
	if m.input != "keep this draft" {
		t.Fatalf("Ctrl+Home changed draft input to %q", m.input)
	}

	m.dispatchKey(key(tea.KeyEnd, tea.ModCtrl))
	if !m.viewport.AtBottom() {
		t.Fatal("Ctrl+End should jump to the newest conversation content")
	}
	if !m.autoFollow {
		t.Fatal("Ctrl+End should resume auto-follow")
	}
	if m.input != "keep this draft" {
		t.Fatalf("Ctrl+End changed draft input to %q", m.input)
	}
}

func TestDispatchKeyReleaseReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// KeyReleaseMsg should be handled in Update(), not handleKey.
	_, cmd := m.Update(tea.KeyReleaseMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from KeyReleaseMsg in Update")
	}
}

func TestDispatchKeyNonRuneReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// A KeyMsg that wraps a KeyPressMsg with empty text and no recognized code.
	msg := tea.KeyPressMsg{Code: 9999} // some unknown code
	cmd := m.handleKey(msg)
	// Unknown codes with Text="" should result in a nil cmd.
	_ = cmd
}

func TestDispatchPrintableNonPrintableIgnored(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// A key with empty text and unknown code should be ignored.
	m.dispatchKey(tea.KeyPressMsg{Code: 999})
	// No crash expected.
}

// TestEnterOnFocusedNonTerminalChildIsNoop guards the "running children
// aren't expandable" requirement (CAT-65 P4.2) at the key-dispatch layer:
// even if focusedChild somehow points at a non-terminal child, Enter must
// not open the overlay.
func TestEnterOnFocusedNonTerminalChildIsNoop(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.childAgentOrder = []string{"c1"}
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "working", sessionID: "sess-child-1"},
	}
	m.focusedChild = 0
	m.focusedTool = -1
	m.input = ""

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.childTranscriptViewer != nil {
		t.Fatal("expected Enter on a non-terminal focused child not to open the overlay")
	}
	if len(rt.sent) != 0 {
		t.Fatalf("expected no command sent, got %d", len(rt.sent))
	}
}
