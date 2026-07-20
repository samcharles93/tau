package tui2

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// key builds a tea.KeyPressMsg for a special key (Home, Left, ctrl+backspace,
// etc.) - no Text, matching what a real terminal sends for non-printable /
// modified keys (see the model_test.go sanity check in conversation history:
// setting Text on a Ctrl-modified letter suppresses the modifier in String()).
func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// charKey builds a tea.KeyPressMsg for a plain printable character.
func charKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestTypingInsertsAtCursorNotJustAppend(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	for _, r := range "helloworld" {
		m.handleKey(charKey(r))
	}
	if m.input != "helloworld" || m.inputCursor != 10 {
		t.Fatalf("input=%q cursor=%d, want %q/10", m.input, m.inputCursor, "helloworld")
	}

	// Move left 5 (to between "hello" and "world") and insert a space -
	// a pure append model would tack it onto the end instead.
	for range 5 {
		m.handleKey(key(tea.KeyLeft, 0))
	}
	m.handleKey(charKey(' '))

	if m.input != "hello world" {
		t.Fatalf("input = %q, want %q (mid-line insert)", m.input, "hello world")
	}
	if m.inputCursor != 6 {
		t.Fatalf("cursor = %d, want 6", m.inputCursor)
	}
}

func TestBackspaceAndDeleteAtCursor(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputCursor = 6 // just after "hello "

	m.handleKey(key(tea.KeyBackspace, 0))
	if m.input != "helloworld" || m.inputCursor != 5 {
		t.Fatalf("after backspace: input=%q cursor=%d, want %q/5", m.input, m.inputCursor, "helloworld")
	}

	m.handleKey(key(tea.KeyDelete, 0))
	if m.input != "helloorld" || m.inputCursor != 5 {
		t.Fatalf("after delete: input=%q cursor=%d, want %q/5", m.input, m.inputCursor, "helloorld")
	}
}

func TestCtrlBackspaceDeletesPreviousWord(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello there world"
	m.inputCursor = len([]rune("hello there ")) // cursor right before "world"

	m.handleKey(key(tea.KeyBackspace, tea.ModCtrl))

	if m.input != "hello world" {
		t.Fatalf("ctrl+backspace: input = %q, want %q", m.input, "hello world")
	}
	if want := len([]rune("hello ")); m.inputCursor != want {
		t.Fatalf("ctrl+backspace: cursor = %d, want %d", m.inputCursor, want)
	}
}

func TestCtrlWordJump(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello there world"
	m.inputCursor = len([]rune(m.input))

	m.handleKey(key(tea.KeyLeft, tea.ModCtrl))
	if want := len([]rune("hello there ")); m.inputCursor != want {
		t.Fatalf("ctrl+left: cursor = %d, want %d", m.inputCursor, want)
	}

	m.handleKey(key(tea.KeyLeft, tea.ModCtrl))
	if want := len([]rune("hello ")); m.inputCursor != want {
		t.Fatalf("ctrl+left (again): cursor = %d, want %d", m.inputCursor, want)
	}

	m.handleKey(key(tea.KeyRight, tea.ModCtrl))
	if want := len([]rune("hello there ")); m.inputCursor != want {
		t.Fatalf("ctrl+right: cursor = %d, want %d", m.inputCursor, want)
	}
}

func TestHomeEndAndKillLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"
	m.inputCursor = 5

	m.handleKey(key(tea.KeyHome, 0))
	if m.inputCursor != 0 {
		t.Fatalf("home: cursor = %d, want 0", m.inputCursor)
	}
	m.handleKey(key(tea.KeyEnd, 0))
	if m.inputCursor != len([]rune("hello world")) {
		t.Fatalf("end: cursor = %d, want %d", m.inputCursor, len([]rune("hello world")))
	}

	m.inputCursor = 5
	m.handleKey(key('k', tea.ModCtrl)) // kill to end
	if m.input != "hello" {
		t.Fatalf("ctrl+k: input = %q, want %q", m.input, "hello")
	}

	m.input = "hello world"
	m.inputCursor = 6
	m.handleKey(key('u', tea.ModCtrl)) // kill to start
	if m.input != "world" || m.inputCursor != 0 {
		t.Fatalf("ctrl+u: input=%q cursor=%d, want %q/0", m.input, m.inputCursor, "world")
	}
}

func TestShiftEnterInsertsNewlineInsteadOfSubmitting(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "line one"
	m.inputCursor = len([]rune(m.input))

	m.handleKey(key(tea.KeyEnter, tea.ModShift))

	if m.input != "line one\n" {
		t.Fatalf("shift+enter: input = %q, want a trailing newline appended", m.input)
	}
	if len(rt.sent) != 0 {
		t.Error("shift+enter must not submit - nothing should be sent to the runtime")
	}
	if m.inResponse() {
		t.Error("shift+enter must not start a turn")
	}
}

func TestShiftTabCyclesInputModeAndPreservesDraft(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "add auth flow"
	m.inputCursor = len([]rune(m.input))

	m.handleKey(key(tea.KeyTab, tea.ModShift))

	if m.inputModeCommand == "" {
		t.Fatal("expected an active agent mode")
	}
	if m.input != "add auth flow" {
		t.Fatalf("input = %q, want draft unchanged", m.input)
	}
	if m.inputCursor != len([]rune(m.input)) {
		t.Fatalf("cursor = %d, want end %d", m.inputCursor, len([]rune(m.input)))
	}
}

func TestInputModesExcludeAgentsHiddenFromModeSwitcher(t *testing.T) {
	var names []string
	for _, mode := range inputModes() {
		if mode.command != "" {
			names = append(names, mode.command)
		}
	}
	if !slices.Contains(names, "plan") {
		t.Fatalf("input modes = %v, want plan", names)
	}
	for _, hidden := range []string{"compact", "summarise", "init"} {
		if slices.Contains(names, hidden) {
			t.Fatalf("input modes = %v, did not want %s", names, hidden)
		}
	}
}

func TestShiftTabCyclesAgentModeBackToChat(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "add auth flow"
	m.inputCursor = len([]rune(m.input))
	m.inputModeCommand = "plan"

	for range len(inputModes()) {
		m.handleKey(key(tea.KeyTab, tea.ModShift))
		if m.inputModeCommand == "" {
			break
		}
	}

	if m.input != "add auth flow" {
		t.Fatalf("input = %q, want chat draft without agent prefix", m.input)
	}
}

func TestInputAreaTitleReflectsAgentMode(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 60
	m.input = "add auth flow"
	m.inputCursor = len([]rune(m.input))
	m.inputModeCommand = "plan"

	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, "╭ Planning ") {
		t.Fatalf("input area = %q, want Planning title", plain)
	}
}

func TestInputAreaShowsModeDescriptionForEmptyAgentMode(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.handleKey(key(tea.KeyTab, tea.ModShift))

	entry, ok := slashIndex[m.inputModeCommand]
	if !ok || !entry.isAgent || m.input != "" {
		t.Fatalf("mode = %q input = %q, want empty agent mode", m.inputModeCommand, m.input)
	}
	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, entry.description) {
		t.Fatalf("input area = %q, want mode description %q", plain, entry.description)
	}

	m.handleKey(charKey('x'))
	plain = stripANSI(m.renderInputArea())
	if strings.Contains(plain, entry.description) {
		t.Fatalf("input area = %q, want description hidden after typing", plain)
	}
}

func TestAgentModeSubmissionKeepsSyntheticCommandOutOfInputAndHistory(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inputModeCommand = "plan"
	m.input = "investigate the best approach"
	m.inputCursor = len([]rune(m.input))

	cmd := m.submitInput()
	drainCmd(cmd)

	if m.input != "" || m.inputModeCommand != "" {
		t.Fatalf("input = %q mode = %q, want both reset after submit", m.input, m.inputModeCommand)
	}
	if len(m.history) != 1 || m.history[0] != "investigate the best approach" {
		t.Fatalf("history = %q, want raw prompt without /plan", m.history)
	}
	if len(rt.sent) != 2 {
		t.Fatalf("sent %d commands, want agent activation and prompt", len(rt.sent))
	}
	if got, ok := rt.sent[0].(tauchat.RunAgentCommand); !ok || got.Name != "plan" {
		t.Fatalf("first command = %#v, want RunAgentCommand plan", rt.sent[0])
	}
	if got, ok := rt.sent[1].(tauchat.SubmitChatPromptCommand); !ok || got.Prompt != "investigate the best approach" {
		t.Fatalf("second command = %#v, want raw prompt submission", rt.sent[1])
	}
}

func TestEmptyAgentModeSubmissionKeepsModeSelected(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inputModeCommand = "plan"

	if cmd := m.submitInput(); cmd != nil {
		t.Fatal("empty submission returned a command")
	}
	if m.inputModeCommand != "plan" {
		t.Fatalf("mode = %q, want plan preserved", m.inputModeCommand)
	}
	if len(rt.sent) != 0 {
		t.Fatalf("sent %d commands, want none", len(rt.sent))
	}
}

func TestUnavailableAgentModePreservesDraft(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inputModeCommand = "missing"
	m.input = "keep this draft"
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.submitInput())

	if m.input != "keep this draft" || m.inputModeCommand != "missing" {
		t.Fatalf("input = %q mode = %q, want invalid state preserved for recovery", m.input, m.inputModeCommand)
	}
	if m.notification != "agent mode unavailable: missing" {
		t.Fatalf("notification = %q, want unavailable-mode error", m.notification)
	}
	if len(m.history) != 0 || len(rt.sent) != 0 {
		t.Fatalf("history = %q sent = %d, want no submission side effects", m.history, len(rt.sent))
	}
}

func TestUpDownRecallHistoryAtBufferEdgesElseMoveVertically(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first prompt", "second prompt"}
	m.historyIdx = -1

	// Single-line, cursor at start (0) - Up must recall history, not move.
	m.input = ""
	m.inputCursor = 0
	m.handleKey(key(tea.KeyUp, 0))
	if m.input != "second prompt" {
		t.Fatalf("up at buffer start: input = %q, want history recall", m.input)
	}

	// Multi-line buffer, cursor on the last line (not at line 0) - Up must
	// move the cursor vertically instead of recalling history.
	m.history = []string{"h1"}
	m.historyIdx = -1
	m.input = "line one\nline two"
	m.inputCursor = len([]rune(m.input)) // end of "line two"

	m.handleKey(key(tea.KeyUp, 0))

	if m.input != "line one\nline two" {
		t.Fatalf("vertical move must not alter buffer content: got %q", m.input)
	}
	wantCursor := len([]rune("line one")) // same column (8) on line 0
	if m.inputCursor != wantCursor {
		t.Fatalf("up on multi-line buffer: cursor = %d, want %d (vertical move, not history recall)", m.inputCursor, wantCursor)
	}
}

func TestCtrlShiftGCopiesLastResponse(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.lastAssistantText = "the answer"

	cmd := m.handleKey(key('g', tea.ModCtrl|tea.ModShift))
	if cmd == nil {
		t.Fatal("expected a Cmd from Ctrl+Shift+G")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a BatchMsg containing the clipboard Cmd, got %#v", cmd())
	}
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.Kind() != reflect.String || v.String() != "the answer" {
		t.Fatalf("clipboard payload = %#v, want %q", clip, "the answer")
	}
}

// --- focus tracking gates desktop notifications -----------------------------

func TestNotificationOnlyFiresWhenUnfocused(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	// Focused (the default): completing a response must NOT raise a
	// tea.Raw notification - the text already streamed on-screen.
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hi there"})
	cmd := m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if containsRawMsg(cmd) {
		t.Error("expected no desktop notification while focused")
	}

	// Blurred: completing a response must raise one.
	m.Update(tea.BlurMsg{})
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hi again"})
	cmd = m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if !containsRawMsg(cmd) {
		t.Error("expected a desktop notification while unfocused")
	}

	// Refocused: back to no notification.
	m.Update(tea.FocusMsg{})
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hi once more"})
	cmd = m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if containsRawMsg(cmd) {
		t.Error("expected no desktop notification after refocusing")
	}
}

// containsRawMsg drains cmd (a tea.Batch) and reports whether any leaf
// message is a tea.RawMsg (what tea.Raw produces).
func containsRawMsg(cmd tea.Cmd) bool {
	for _, msg := range drainCmd(cmd) {
		if _, ok := msg.(tea.RawMsg); ok {
			return true
		}
	}
	return false
}
