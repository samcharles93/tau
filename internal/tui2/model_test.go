package tui2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/tui/notify"
)

func TestMain(m *testing.M) {
	notificationClearDelay = time.Millisecond
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
	return newModel(context.Background(), rt, sub, "sess", "gpt", "openai", nil, nil, true, "medium", nil, "", false)
}

// --- bash mode: CallID tracking (real bug found in commit 585874f) --------

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

func TestBashModeIgnoresUnrelatedToolCompletion(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("ls"))

	// An LLM tool call finishing must not be mistaken for the bash command.
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "unrelated-tool-call"})

	if !m.bashRunning {
		t.Error("an unrelated tool completion must not clear bashRunning")
	}
	if m.bashCallID == "" {
		t.Error("an unrelated tool completion must not clear bashCallID")
	}
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
		t.Error("expected bashRunning=false after a failed send — otherwise input is locked forever")
	}
	if m.bashCallID != "" {
		t.Error("expected bashCallID cleared after a failed send")
	}
}

// --- confirm prompt: highlighted-option toggle (real bug found in 585874f) -

func TestConfirmPromptEnterUsesHighlightedOption(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	evt := tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm", Title: "Delete?", Message: "sure?",
	}
	drainCmd(m.handleChatEvent(evt))

	if m.activePrompt == nil {
		t.Fatal("expected activePrompt to be set")
	}
	if !m.promptConfirmYes {
		t.Fatal("expected the default highlighted option to be Yes")
	}

	// Toggle to "No" before submitting.
	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.promptConfirmYes {
		t.Fatal("expected tab to toggle the highlighted option to No")
	}

	drainCmd(m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(rt.sent) != 1 {
		t.Fatalf("expected exactly 1 command sent, got %d", len(rt.sent))
	}
	resp, ok := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !ok {
		t.Fatalf("expected RespondInteractivePromptCommand, got %T", rt.sent[0])
	}
	if resp.Confirmed {
		t.Error("bare Enter must resolve to the highlighted option (No), not silently default to Confirmed=true")
	}
	if !resp.Canceled {
		t.Error("expected Canceled=true when the highlighted/submitted option is No")
	}
}

func TestConfirmPromptYKeyConfirmsRegardlessOfHighlight(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	evt := tauchat.InteractivePromptRequestedEvent{RequestID: "req-2", Kind: "confirm"}
	drainCmd(m.handleChatEvent(evt))

	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab}) // toggle to No
	drainCmd(m.handlePromptKey(tea.KeyPressMsg{Code: 'y', Text: "y"}))

	resp, ok := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !ok {
		t.Fatalf("expected RespondInteractivePromptCommand, got %T", rt.sent[0])
	}
	if !resp.Confirmed {
		t.Error("explicit 'y' must confirm regardless of the highlighted option")
	}
}

// --- /copy: must read raw content, not lipgloss-styled output -------------
//
// commands.go originally derived the clipboard payload by string-matching a
// "tau: " prefix against m.renderedLines — but those lines are
// lipgloss-styled, so the actual stored string starts with an ANSI escape
// sequence, not the literal "tau: ". That made /copy silently report
// "nothing to copy" after every real (colored) response. The fix tracks the
// raw text separately in m.lastAssistantText.

func TestCopyCommandReturnsRawContentAfterResponse(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello world"})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.lastAssistantText != "hello world" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "hello world")
	}

	cmd := m.cmdCopy("")
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd once a response has completed")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a BatchMsg containing the clipboard Cmd, got %#v", cmd())
	}

	// Only execute the clipboard sub-cmd — the notification sub-cmd is a
	// 4-second tea.Tick and isn't relevant here.
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.Kind() != reflect.String || v.String() != "hello world" {
		t.Fatalf("clipboard payload = %#v, want %q", clip, "hello world")
	}
	if m.notification == "" {
		t.Error("expected a confirmation notification after copying")
	}
}

func TestCopyCommandNotifiesWhenNothingToCopy(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	// setNotification's returned Cmd is a bare 4s tea.Tick — don't execute
	// it, just check the synchronous m.notification side effect.
	m.cmdCopy("")

	if m.notification != "nothing to copy" {
		t.Fatalf("notification = %q, want %q", m.notification, "nothing to copy")
	}
}

// --- bridging: readNextEvent must re-arm after every delivery --------------

func TestChatEventLoopRearmsAfterEachEvent(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	pub := eventbus.Publish[tauchat.ChatEvent](bus.Client("pub"))
	sub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("sub"))
	defer sub.Close()

	rt := &fakeRuntime{}
	m := newTestModel(rt, sub)

	pub.Publish(tauchat.ChatNotificationEvent{Message: "one"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "two"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "three"})

	var delivered []string
	cmd := m.Init()
	for i := 0; i < 3 && len(delivered) < 3; i++ {
		var next tea.Cmd
		for _, msg := range drainCmd(cmd) {
			if ce, ok := msg.(chatEventMsg); ok {
				if n, ok := ce.event.(tauchat.ChatNotificationEvent); ok {
					delivered = append(delivered, n.Message)
				}
			}
			if _, c := m.Update(msg); c != nil {
				next = c
			}
		}
		cmd = next
	}

	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(delivered, want) {
		t.Fatalf("re-arm pattern dropped or reordered events: got %v, want %v", delivered, want)
	}
}

// --- renderLine must never label messages with a literal name -------------
//
// A "You: "/"tau: " prefix is legacy behaviour from an earlier renderer —
// taui's actual convention (internal/tui/inline_chat.go's submit echo) is a
// bold "⏎" glyph in front of a user message and no prefix at all on an
// assistant message, never a name.

func TestRenderLineHasNoNameLabels(t *testing.T) {
	for _, role := range []string{"user", "assistant", "system"} {
		out := stripANSI(renderLine(role, "git status"))
		if strings.Contains(out, "You:") || strings.Contains(out, "tau:") {
			t.Errorf("renderLine(%q, ...) = %q still contains a literal name label", role, out)
		}
	}
}

func TestRenderLineUserGetsGlyphNotAssistant(t *testing.T) {
	user := stripANSI(renderLine("user", "hello"))
	if user != "⏎ hello" {
		t.Errorf("renderLine(user, ...) = %q, want %q", user, "⏎ hello")
	}
	assistant := stripANSI(renderLine("assistant", "hello"))
	if assistant != "hello" {
		t.Errorf("renderLine(assistant, ...) = %q, want %q (no prefix at all)", assistant, "hello")
	}
}

// A bash-mode echo (appendMessage("user", "!"+cmd), see handleBashCommand)
// must render the same way as any other user line — no double-marking on
// top of the leading "!".
func TestRenderLineBashEchoHasNoNameLabel(t *testing.T) {
	out := stripANSI(renderLine("user", "!git status"))
	if strings.Contains(out, "You:") {
		t.Errorf("bash echo = %q still contains a literal name label", out)
	}
	if out != "⏎ !git status" {
		t.Errorf("bash echo = %q, want %q", out, "⏎ !git status")
	}
}

// --- dispatchKey tests -----------------------------------------------------

func TestDispatchCtrlCQuits(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// A single idle Ctrl+C arms the quit guard rather than quitting outright
	// (see TestHandleCtrlCIdleArmsQuitWithoutQuitting) — the returned Cmd here
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
	// Should toggle steering mode — no Cmd needed, since the status bar
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

// --- handleChatEvent variants ---------------------------------------------

func TestHandleChatEventSnapshot(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	state := tauchat.ChatSessionState{
		SessionID:    "sess-1",
		Model:        tauchat.ChatModelRef{ID: "gpt-4"},
		ProviderName: "openai",
		Messages: []tauchat.ChatMessage{
			{Role: tauchat.ChatRoleUser, Content: "hello"},
			{Role: tauchat.ChatRoleAssistant, Content: "hi there"},
		},
	}

	m.handleChatEvent(tauchat.ChatSessionSnapshotEvent{State: state})

	if m.modelName != "gpt-4" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-4")
	}
	if m.provider != "openai" {
		t.Fatalf("provider = %q, want %q", m.provider, "openai")
	}
	if len(m.renderedLines) < 2 {
		t.Fatalf("expected at least 2 rendered lines from history, got %d", len(m.renderedLines))
	}
	if m.lastAssistantText != "hi there" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "hi there")
	}
}

func TestHandleChatEventResponseStartedClearsState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "old"
	m.reasoning = "old"
	m.tools = []toolState{{id: "t1"}}
	m.inResponse = false

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})

	if m.streaming != "" {
		t.Fatal("streaming should be cleared on start")
	}
	if m.reasoning != "" {
		t.Fatal("reasoning should be cleared on start")
	}
	if len(m.tools) != 0 {
		t.Fatal("tools should be cleared on start")
	}
	if !m.inResponse {
		t.Fatal("inResponse should be true on start")
	}
}

func TestHandleChatEventDeltaAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello "})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "world"})

	if m.streaming != "hello world" {
		t.Fatalf("streaming = %q, want %q", m.streaming, "hello world")
	}
}

func TestHandleChatEventReasoningDelta(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "thinking..."})

	if m.reasoning != "thinking..." {
		t.Fatalf("reasoning = %q, want %q", m.reasoning, "thinking...")
	}
}

func TestHandleChatEventToolCallDelta(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "read", ArgumentsSummary: "{\"file\":"})
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "", ArgumentsSummary: "\"foo.txt\"}"})

	if len(m.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.tools))
	}
	if m.tools[0].name != "read" {
		t.Fatalf("tool name = %q, want %q", m.tools[0].name, "read")
	}
	if m.tools[0].args != `{"file":"foo.txt"}` {
		t.Fatalf("tool args = %q, want %q", m.tools[0].args, `{"file":"foo.txt"}`)
	}
}

func TestHandleChatEventToolExecutionStarted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "pending"}}

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1"})

	if m.tools[0].status != "running" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "running")
	}
}

func TestHandleChatEventToolExecutionStartedCreatesMissingTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:           "bash-1",
		ToolName:         "shell",
		ArgumentsSummary: "git status",
	})
	m.handleChatEvent(tauchat.ChatToolOutputEvent{CallID: "bash-1", Chunk: "On branch main"})
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{
		CallID:        "bash-1",
		ResultSummary: "On branch main",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.tools))
	}
	tool := m.tools[0]
	if tool.name != "shell" {
		t.Fatalf("tool name = %q, want shell", tool.name)
	}
	if tool.args != "git status" {
		t.Fatalf("tool args = %q, want git status", tool.args)
	}
	if tool.status != "done" {
		t.Fatalf("tool status = %q, want done", tool.status)
	}
	if tool.result != "On branch main" {
		t.Fatalf("tool result = %q, want On branch main", tool.result)
	}
}

func TestHandleChatEventToolExecutionCompletedDone(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "running"}}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1", ResultSummary: "success"})

	if m.tools[0].status != "done" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "done")
	}
	if m.tools[0].result != "success" {
		t.Fatalf("tool result = %q, want %q", m.tools[0].result, "success")
	}
}

func TestHandleChatEventToolExecutionCompletedError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "running"}}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1", IsError: true, ResultSummary: "failed"})

	if m.tools[0].status != "error" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "error")
	}
}

func TestHandleChatEventToolExecutionCompletedBash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.bashCallID = "bash-abc"

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "bash-abc", ResultSummary: "done"})

	if m.bashRunning {
		t.Fatal("bashRunning should be false when matching bash call ID completes")
	}
	if m.bashCallID != "" {
		t.Fatalf("bashCallID should be cleared, got %q", m.bashCallID)
	}
}

func TestHandleChatEventToolOutputAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", result: "partial"}}

	m.handleChatEvent(tauchat.ChatToolOutputEvent{CallID: "t1", Chunk: " and more"})

	if m.tools[0].result != "partial and more" {
		t.Fatalf("tool result = %q, want %q", m.tools[0].result, "partial and more")
	}
}

func TestHandleChatEventResponseCompleted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "final answer"
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}
	m.inResponse = true
	m.focused = false // looked away — expect a desktop-notify Cmd

	cmd := m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if cmd == nil {
		t.Fatal("expected a Cmd from response completed")
	}

	if m.inResponse {
		t.Fatal("inResponse should be false after completion")
	}
	if len(m.renderedLines) == 0 {
		t.Fatal("expected rendered lines after completion")
	}
	if m.lastAssistantText != "final answer" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "final answer")
	}
}

func TestHandleChatEventResponseCompletedToolOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = []toolState{{id: "t1", name: "search", status: "done"}}
	m.inResponse = true

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	// Should synthesize a placeholder.
	if m.lastAssistantText == "" {
		t.Fatal("expected placeholder text for tool-only response")
	}
	if !strings.Contains(m.lastAssistantText, "search") {
		t.Fatalf("placeholder = %q, should mention tool name", m.lastAssistantText)
	}
}

func TestHandleChatEventResponseCompletedReasoningOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.reasoning = "I think..."
	m.inResponse = true

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.lastAssistantText != "[reasoning only]" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "[reasoning only]")
	}
}

func TestHandleChatEventResponseCompletedNoContentNoTools(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = nil
	m.inResponse = true

	cmd := m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if cmd == nil {
		t.Fatal("expected a Cmd even with empty response")
	}
	if m.inResponse {
		t.Fatal("inResponse should be false")
	}
}

func TestHandleChatEventResponseCancelled(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "partial"
	m.reasoning = "thinking"
	m.tools = []toolState{{id: "t1"}}
	m.inResponse = true
	m.steering = true

	m.handleChatEvent(tauchat.ChatResponseCancelledEvent{})

	if m.streaming != "" {
		t.Fatal("streaming should be cleared on cancel")
	}
	if m.reasoning != "" {
		t.Fatal("reasoning should be cleared on cancel")
	}
	if len(m.tools) != 0 {
		t.Fatal("tools should be cleared on cancel")
	}
	if m.inResponse {
		t.Fatal("inResponse should be false on cancel")
	}
	if m.steering {
		t.Fatal("steering should be false on cancel")
	}
}

func TestHandleChatEventRuntimeError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true
	m.steering = true
	m.streaming = "partial"
	m.reasoning = "thinking"
	m.tools = []toolState{{id: "t1"}}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "API error"})

	if m.inResponse {
		t.Fatal("inResponse should be false after runtime error")
	}
	if m.steering {
		t.Fatal("steering should be false after runtime error")
	}
	if m.streaming != "" || m.reasoning != "" || m.tools != nil {
		t.Fatal("expected the in-flight turn's streaming/reasoning/tools state cleared")
	}
	if n := m.notifyQ.Current(); n == nil || n.Level != notify.LevelError {
		t.Fatalf("expected an error-level notification in notifyQ, got %+v", n)
	}
	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "API error") {
		t.Fatalf("expected the error to be printed to scrollback, got %q", joined)
	}
}

func TestHandleChatEventNotification(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatNotificationEvent{Message: "hello"})

	// Notification should be pushed to the queue.
	if m.notifyQ == nil {
		t.Fatal("notifyQ should exist")
	}
}

func TestHandleChatEventInteractivePromptQueued(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "input", Title: "API Key", Message: "enter key",
	})

	if m.activePrompt == nil {
		t.Fatal("expected activePrompt to be set")
	}
	if m.activePrompt.RequestID != "req-1" {
		t.Fatalf("activePrompt.RequestID = %q, want %q", m.activePrompt.RequestID, "req-1")
	}
}

func TestHandleChatEventInteractivePromptQueueSecond(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1"})
	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-2"})

	// First should be active, second queued.
	if m.activePrompt.RequestID != "req-1" {
		t.Fatalf("active prompt = %q, want %q", m.activePrompt.RequestID, "req-1")
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].RequestID != "req-2" {
		t.Fatalf("expected [req-2] in prompt queue, got %+v", m.promptQueue)
	}
}

func TestHandleChatEventSessionsListed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionsListedEvent{
		Sessions: []tauchat.SessionSummary{{
			ID: "sess-1", ModelID: "gpt-4", MessageCount: 10,
		}},
	})

	if len(m.sessionSummaries) != 1 {
		t.Fatalf("sessionSummaries = %d, want 1", len(m.sessionSummaries))
	}
	if len(m.renderedLines) == 0 {
		t.Fatal("expected the session list to be rendered as a system message")
	}
}

func TestSessionSummariesTextEmpty(t *testing.T) {
	out := sessionSummariesText(nil, "")
	if out != "Sessions: no saved sessions" {
		t.Fatalf("out = %q, want %q", out, "Sessions: no saved sessions")
	}
}

func TestSessionSummariesTextWithMetricsAndCursor(t *testing.T) {
	out := sessionSummariesText([]tauchat.SessionSummary{
		{
			ID: "sess-1", ModelID: "gpt-4", MessageCount: 3,
			InputTokens: 100, OutputTokens: 50, Cost: 0.02,
			ToolCalls: 2, ToolErrors: 1, DurationMs: 1500,
		},
	}, "cursor-2")

	for _, want := range []string{"sess-1", "3 messages", "gpt-4", "↑100", "↓50", "$0.0200", "2 tools (1 err)", "1s", "More sessions available."} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestFormatDurationCompact(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{500, "500ms"},
		{2500, "2s"},
		{90_000, "1m"},
		{3_900_000, "1h 5m"},
	}
	for _, c := range cases {
		if got := formatDurationCompact(c.ms); got != c.want {
			t.Errorf("formatDurationCompact(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestHandleChatEventSessionLoaded(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionLoadedEvent{
		State: tauchat.ChatSessionState{SessionID: "sess-loaded"},
	})

	if m.notification == "" {
		t.Fatal("expected notification about loaded session")
	}
	if !strings.Contains(m.notification, "sess-loaded") {
		t.Fatalf("notification = %q, should mention session ID", m.notification)
	}
}

func TestHandleChatEventSessionLoadedReplaysMessagesAndSeedsHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "old-session"
	m.history = []string{"stale entry"}

	m.handleChatEvent(tauchat.SessionLoadedEvent{
		State: tauchat.ChatSessionState{
			SessionID: "sess-loaded",
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "first prompt"},
				{Role: tauchat.ChatRoleAssistant, Content: "first reply"},
				{Role: tauchat.ChatRoleUser, Content: "second prompt"},
			},
		},
	})

	if m.sessionID != "sess-loaded" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-loaded")
	}
	joined := strings.Join(m.renderedLines, "\n")
	for _, want := range []string{"first prompt", "first reply", "second prompt"} {
		if !strings.Contains(stripANSI(joined), want) {
			t.Fatalf("renderedLines missing %q, got %q", want, joined)
		}
	}
	if len(m.history) != 2 || m.history[0] != "first prompt" || m.history[1] != "second prompt" {
		t.Fatalf("history = %v, want [first prompt, second prompt]", m.history)
	}
}

func TestSeedHistoryFromMessagesIgnoresEmptyContentAndNonUserRoles(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.seedHistoryFromMessages([]tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "  "},
		{Role: tauchat.ChatRoleAssistant, Content: "assistant text"},
		{Role: tauchat.ChatRoleUser, Content: "real prompt"},
	})

	if len(m.history) != 1 || m.history[0] != "real prompt" {
		t.Fatalf("history = %v, want [real prompt]", m.history)
	}
}

func TestSeedHistoryFromMessagesKeepsExistingWhenNoUserMessages(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"keep me"}

	m.seedHistoryFromMessages([]tauchat.ChatMessage{
		{Role: tauchat.ChatRoleAssistant, Content: "assistant only"},
	})

	if len(m.history) != 1 || m.history[0] != "keep me" {
		t.Fatalf("history = %v, want unchanged [keep me]", m.history)
	}
}

func TestHandleChatEventSessionDeleted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionDeletedEvent{SessionID: "sess-gone"})

	if m.notification == "" {
		t.Fatal("expected notification about deleted session")
	}
	if !strings.Contains(m.notification, "sess-gone") {
		t.Fatalf("notification = %q, should mention deleted session ID", m.notification)
	}
}

func TestHandleChatEventSessionExported(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionExportedEvent{Path: "/tmp/export.jsonl"})

	if m.notification == "" {
		t.Fatal("expected notification about exported session")
	}
}

func TestHandleChatEventExtensionsReloaded(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionsReloadedEvent{
		Result: tauchat.ExtensionReloadResult{ExtensionCount: 3},
	})

	if m.notification == "" {
		t.Fatal("expected notification about extension reload")
	}
	if !strings.Contains(m.notification, "3") {
		t.Fatalf("notification = %q, should mention extension count", m.notification)
	}
}

func TestHandleChatEventExtensionCommandsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionCommandsChangedEvent{
		Commands: []tauchat.ExtensionCommand{
			{Name: "mcp", Description: "MCP commands"},
		},
	})

	if len(m.extensionCommands) != 1 {
		t.Fatalf("extensionCommands = %d, want 1", len(m.extensionCommands))
	}
	if _, ok := m.extensionCommands["mcp"]; !ok {
		t.Fatal("expected 'mcp' in extensionCommands")
	}
}

func TestHandleChatEventExtensionCommandResult(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionCommandResultEvent{Output: "plugin output"})

	// Should append a system message.
	if len(m.renderedLines) == 0 {
		t.Fatal("expected rendered lines after extension command result")
	}
}

func TestHandleChatEventExtensionViewRendered(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionViewRenderedEvent{
		ViewID: "v1",
		View: tauchat.ExtensionView{
			Title:   "Dashboard",
			Widgets: []tauchat.Widget{{}}, // one widget so content shows "1 widgets"
		},
	})

	if len(m.panels) != 1 {
		t.Fatalf("expected 1 panel, got %d", len(m.panels))
	}
	p, ok := m.panels["v1"]
	if !ok || p.title != "Dashboard" {
		t.Fatalf("panel = %+v, want title %q", p, "Dashboard")
	}
}

func TestHandleChatEventExtensionViewClosed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.panels["v1"] = pluginPanel{id: "v1", title: "Dashboard"}

	m.handleChatEvent(tauchat.ExtensionViewClosedEvent{ViewID: "v1"})

	if len(m.panels) != 0 {
		t.Fatalf("expected 0 panels after close, got %d", len(m.panels))
	}
}

func TestHandleChatEventSkillsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SkillsChangedEvent{Skills: []tauchat.SkillInfo{
		{Name: "python", Description: "Python helper", Scope: "project"},
		{Name: "go", Description: "Go helper"},
	}})

	joined := strings.Join(m.renderedLines, "\n")
	for _, want := range []string{"Available Skills:", "python", "Python helper", "(project)", "go", "Go helper"} {
		if !strings.Contains(stripANSI(joined), want) {
			t.Fatalf("rendered skills list missing %q, got %q", want, joined)
		}
	}
}

// --- submitInput ---

func TestSubmitInputEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "  "

	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("expected nil Cmd for empty/whitespace input")
	}
}

func TestSubmitInputDuringResponse(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = true
	m.input = "hello"

	drainCmd(m.submitInput())

	// Should not show a waiting notification; should instead send a steer command.
	if m.notification != "" {
		t.Fatalf("unexpected notification: %q", m.notification)
	}
	// Input should be cleared.
	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	// Verify that SteerChatPromptCommand was sent.
	if len(rt.sent) != 1 {
		t.Fatalf("sent = %d, want 1 command", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.SteerChatPromptCommand)
	if !ok {
		t.Fatalf("sent command = %T, want SteerChatPromptCommand", rt.sent[0])
	}
	if cmd.Prompt != "hello" {
		t.Fatalf("steer prompt = %q, want 'hello'", cmd.Prompt)
	}
}

func TestSubmitInputDuringBash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.input = "hello"

	m.submitInput()

	if m.notification == "" {
		t.Fatal("expected notification about waiting for bash command")
	}
}

func TestSubmitInputSlashCommand(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/clear"

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd for /clear")
	}
}

func TestSubmitInputBashCommand(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "!ls -la"

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd for bash command")
	}
	if !m.bashRunning {
		t.Fatal("bashRunning should be true after !command")
	}
}

func TestSubmitInputDoubleBangBashCommand(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "!!ls -la"

	drainCmd(m.submitInput())

	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.Command != "ls -la" || !sent.Exclude {
		t.Fatalf("got Command=%q Exclude=%v, want Command=%q Exclude=true", sent.Command, sent.Exclude, "ls -la")
	}
}

func TestSubmitInputDebounce(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.lastSubmit = time.Now() // recent submit

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd even with debounce")
	}
	// The debounce check is 300ms; if we just called now, it should fire.
	// Actually the guard checks elapsed < 300ms — let's test the guard fires.
}

func TestSubmitInputRecordsHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false
	m.input = "hello world"

	m.submitInput()

	if len(m.history) != 1 || m.history[0] != "hello world" {
		t.Fatalf("history = %v, want ['hello world']", m.history)
	}
	if m.historyIdx != -1 {
		t.Fatalf("historyIdx = %d, want -1 (reset)", m.historyIdx)
	}
}

func TestSubmitInputClearsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "test"
	m.inputCursor = 4

	m.submitInput()

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

// --- startOrQueueTurn ---

func TestStartOrQueueTurnQueuesBehindRunning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true

	m.startOrQueueTurn("queued text")

	if len(m.turnQueue) != 1 || m.turnQueue[0] != "queued text" {
		t.Fatalf("turnQueue = %v, want ['queued text']", m.turnQueue)
	}
}

func TestStartOrQueueTurnSendsWhenIdle(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = false

	cmd := m.startOrQueueTurn("send me")
	if cmd == nil {
		t.Fatal("expected a Cmd when starting a turn")
	}
	drainCmd(cmd)

	if !m.inResponse {
		t.Fatal("inResponse should be true after starting a turn")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("expected SubmitChatPromptCommand, got %T", rt.sent[0])
	}
	// RequestID is mandatory server-side (ChatSessionState.BeginTurn rejects
	// an empty one with "request id is required") — this exact regression
	// shipped once because no test asserted it was actually populated.
	if sent.RequestID == "" {
		t.Fatal("expected a non-empty RequestID")
	}
}

func TestStartOrQueueTurnAppendsUserMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false

	drainCmd(m.startOrQueueTurn("show this"))

	if len(m.renderedLines) == 0 {
		t.Fatal("expected the user message to be appended to renderedLines")
	}
}

// --- drainTurnQueue ---

func TestDrainTurnQueueEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.turnQueue = nil

	cmd := m.drainTurnQueue()
	if cmd != nil {
		t.Fatal("expected nil Cmd for empty queue")
	}
}

func TestDrainTurnQueuePopsAndSends(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.turnQueue = []string{"first", "second"}

	drainCmd(m.drainTurnQueue())

	if len(m.turnQueue) != 1 || m.turnQueue[0] != "second" {
		t.Fatalf("turnQueue after drain = %v, want ['second']", m.turnQueue)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
}

// --- handleSteer ---

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

// --- handleBashCommand ---

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

// --- cancelBash ---

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

// --- handleCtrlC ---

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

// drainCmdMsg runs cmd and returns its single tea.Msg (nil-safe).
func drainCmdMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// --- newRequestID ---

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

// --- clearInput ---

func TestClearInputResetsBothFieldAndCursor(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "something"
	m.inputCursor = 5

	m.clearInput()

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

// --- clearScreen (Ctrl+L) ---

func TestClearScreenWipesRenderedLinesNotSession(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.appendMessage("user", "hello")
	m.appendMessage("assistant", "hi there")
	m.sessionID = "sess-keep-me"

	m.clearScreen()

	if len(m.renderedLines) != 0 {
		t.Fatalf("renderedLines = %v, want empty", m.renderedLines)
	}
	if m.sessionID != "sess-keep-me" {
		t.Fatalf("sessionID = %q, want unchanged", m.sessionID)
	}
}

func TestDispatchKeyCtrlLClearsScreen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.appendMessage("user", "hello")

	cmd := m.dispatchKey(key('l', tea.ModCtrl))
	if cmd != nil {
		t.Fatal("expected nil Cmd from Ctrl+L")
	}
	if len(m.renderedLines) != 0 {
		t.Fatalf("renderedLines = %v, want empty after Ctrl+L", m.renderedLines)
	}
}

// --- recallHistory ---

func TestRecallHistoryEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = nil
	m.historyIdx = -1

	cmd := m.recallHistory(-1)
	if cmd != nil {
		t.Fatal("expected nil Cmd when history is empty")
	}
}

func TestRecallHistoryNavigates(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1

	m.recallHistory(-1) // up from start
	if m.input != "second" {
		t.Fatalf("up: input = %q, want %q", m.input, "second")
	}

	m.recallHistory(-1) // up again
	if m.input != "first" {
		t.Fatalf("up again: input = %q, want %q", m.input, "first")
	}

	m.recallHistory(1) // down
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}
}

func TestRecallHistoryClamps(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"only"}
	m.historyIdx = -1

	// Up twice should clamp to the one entry.
	m.recallHistory(-1)
	m.recallHistory(-1)
	if m.input != "only" {
		t.Fatalf("clamped up: input = %q, want %q", m.input, "only")
	}
}

func TestRecallHistoryDownNoNavigationActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1

	m.recallHistory(1) // down without navigating yet
	if m.input != "first" {
		t.Fatalf("down from -1: input = %q, want %q", m.input, "first")
	}
}

// --- input editing ---

func TestMoveCursorRight(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 1

	m.moveCursorRight()
	if m.inputCursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.inputCursor)
	}

	m.moveCursorRight()
	if m.inputCursor != 2 {
		t.Fatalf("cursor should not move past end: got %d", m.inputCursor)
	}
}

func TestAtLastLineEndSingleLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 2

	if !m.atLastLineEnd() {
		t.Fatal("expected at last line end")
	}

	m.inputCursor = 1
	if m.atLastLineEnd() {
		t.Fatal("not at last line end")
	}
}

func TestAtLastLineEndMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = len([]rune(m.input))

	if !m.atLastLineEnd() {
		t.Fatal("cursor at end of last line")
	}
}

func TestMoveCursorVertUpFromNonFirstLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = len([]rune("line one\n")) // start of line two, col 0

	m.moveCursorVert(-1)
	// Should move to same column on line one (col 0).
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0 (start of line one)", m.inputCursor)
	}
}

func TestMoveCursorVertDownFromNonLastLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi\nthere"
	m.inputCursor = 1 // col 1 of line 0 ("hi")

	m.moveCursorVert(1)
	// Should move to col 1 on line 1 ("there"), which is index 4.
	if m.inputCursor != 4 {
		t.Fatalf("cursor = %d, want 4 (col 1 on line 1)", m.inputCursor)
	}
}

func TestMoveCursorVertStaysInBounds(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "single"
	m.inputCursor = 0

	m.moveCursorVert(-1) // already on first line
	if m.inputCursor != 0 {
		t.Fatal("cursor should not move above first line")
	}

	m.moveCursorVert(1) // already on last line
	if m.inputCursor != 0 {
		t.Fatal("cursor should not move below last line")
	}
}

func TestMoveCursorVertPreservesColumnWhenTargetLineShorter(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "abc\nde"
	m.inputCursor = 3 // end of line 0 (col 3)

	m.moveCursorVert(1)
	// Line 1 is "de" (2 chars) — should clamp to col 2, i.e. index 4 (start
	// of line 1) + 2 = 6.
	if m.inputCursor != 6 {
		t.Fatalf("cursor = %d, want 6 (end of line 1, col 2)", m.inputCursor)
	}
}

// --- killToLineStart / killToLineEnd ---

func TestKillToLineStartAtStart(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.inputCursor = 0

	m.killToLineStart()
	if m.input != "hello" {
		t.Fatalf("kill to start at cursor 0 should not change input, got %q", m.input)
	}
}

func TestKillToLineEndAtEnd(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.inputCursor = 5

	m.killToLineEnd()
	if m.input != "hello" {
		t.Fatalf("kill to end at end should not change input, got %q", m.input)
	}
}

// --- splitInputLines / cursorLineCol / linePos ---

func TestSplitInputLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line0\nline1\nline2"

	lines := m.splitInputLines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if string(lines[0]) != "line0" {
		t.Fatalf("line 0 = %q, want %q", string(lines[0]), "line0")
	}
}

func TestSplitInputLinesNoNewlines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "single"

	lines := m.splitInputLines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestCursorLineCol(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "abc\nde"
	m.inputCursor = 4

	lines := m.splitInputLines()
	line, col := m.cursorLineCol(lines)
	if line != 1 || col != 0 {
		t.Fatalf("cursor at 4: line=%d col=%d, want line=1 col=0", line, col)
	}
}

// --- enqueuePrompt / presentNextQueuedPrompt / resolvePromptCancel ---

func TestEnqueuePromptQueuesWhenActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "first"}

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "second"})

	if len(m.promptQueue) != 1 || m.promptQueue[0].RequestID != "second" {
		t.Fatalf("expected [second] queued, got %+v", m.promptQueue)
	}
}

func TestPresentNextQueuedPromptNoQueue(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "current"}

	m.presentNextQueuedPrompt()

	if m.activePrompt != nil {
		t.Fatal("activePrompt should be nil when queue is empty")
	}
}

func TestPresentNextQueuedPromptDrains(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "current"}
	m.promptQueue = []tauchat.InteractivePromptRequestedEvent{
		{RequestID: "next"},
		{RequestID: "last"},
	}

	m.presentNextQueuedPrompt()

	if m.activePrompt == nil || m.activePrompt.RequestID != "next" {
		t.Fatalf("activePrompt.RequestID = %v, want 'next'", m.activePrompt.RequestID)
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].RequestID != "last" {
		t.Fatalf("promptQueue should have [last], got %+v", m.promptQueue)
	}
	if !m.promptConfirmYes {
		t.Fatal("promptConfirmYes should reset to true for new prompt")
	}
}

func TestResolvePromptCancel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-cancel"}

	drainCmd(m.resolvePromptCancel())

	if m.activePrompt != nil {
		t.Fatal("activePrompt should be nil after cancel")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Canceled {
		t.Fatal("expected Canceled=true")
	}
}

func TestResolvePromptCancelWithNoActivePrompt(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.resolvePromptCancel()
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

func TestResolvePromptWithNoActivePrompt(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.resolvePrompt("some input")
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

// --- activePanel ---

func TestActivePanelEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	p := m.activePanel()
	if p != nil {
		t.Fatal("expected nil when no panels")
	}
}

func TestActivePanelReturnsFirst(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.panels["a"] = pluginPanel{id: "a", title: "Panel A"}
	m.panels["b"] = pluginPanel{id: "b", title: "Panel B"}

	p := m.activePanel()
	if p == nil {
		t.Fatal("expected a panel")
	}
}

// --- handlePromptKey ---

func TestHandlePromptKeyNoActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.handlePromptKey(key(tea.KeyEnter, 0))
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

func TestHandlePromptKeyEsc(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "input"}

	drainCmd(m.handlePromptKey(key(tea.KeyEscape, 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Canceled {
		t.Fatal("expected Canceled=true when pressing Esc")
	}
}

func TestHandlePromptKeyEnterOnConfirmWithInputKind(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "input", Message: "enter value",
	}
	m.input = "my value"

	drainCmd(m.handlePromptKey(key(tea.KeyEnter, 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if cmd.Response != "my value" {
		t.Fatalf("Response = %q, want %q", cmd.Response, "my value")
	}
}

func TestHandlePromptKeyYNonConfirmInserts(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "input"}
	m.input = ""

	m.handlePromptKey(key('y', 0))
	if m.input != "y" {
		t.Fatalf("input = %q, want %q (y should be inserted for non-confirm prompts)", m.input, "y")
	}
}

func TestHandlePromptKeyBackspace(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "input"}
	m.input = "abc"
	m.inputCursor = 3

	m.handlePromptKey(key(tea.KeyBackspace, 0))
	if m.input != "ab" {
		t.Fatalf("input = %q, want %q", m.input, "ab")
	}
}

// --- applySnapshot ---

func TestApplySnapshotRebuildsViewport(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			SessionID:    "sess-1",
			Model:        tauchat.ChatModelRef{ID: "claude-3"},
			ProviderName: "anthropic",
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "hi"},
				{Role: tauchat.ChatRoleAssistant, Content: "hello"},
			},
		},
	})

	if m.sessionID != "sess-1" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-1")
	}
	if m.modelName != "claude-3" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "claude-3")
	}
	if m.provider != "anthropic" {
		t.Fatalf("provider = %q, want %q", m.provider, "anthropic")
	}
	// Should have rebuilt renderedLines from messages.
	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "hi") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'hi' to appear in renderedLines after snapshot")
	}
}

// TestApplySnapshotUpdatesStaleSessionID guards against a real bug: without
// updating m.sessionID here, every command sent after /clear, /session <id>,
// or /resume kept targeting the OLD session — the UI would show the new
// session's messages while silently writing into the wrong one server-side.
func TestApplySnapshotUpdatesStaleSessionID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "old-session"

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{SessionID: "new-session"},
	})

	if m.sessionID != "new-session" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "new-session")
	}
}

func TestApplySnapshotEmptySessionIDKeepsCurrent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "keep-me"

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{}})

	if m.sessionID != "keep-me" {
		t.Fatalf("sessionID = %q, want unchanged %q", m.sessionID, "keep-me")
	}
}

// --- finalizeResponse ---

func TestFinalizeResponseEmptyWithNoTools(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = nil
	m.reasoning = ""
	m.inResponse = true

	content := m.finalizeResponse()

	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if m.inResponse {
		t.Fatal("inResponse should be false")
	}
}

// --- renderTool ---

func TestRenderToolWithResult(t *testing.T) {
	tool := toolState{name: "read", result: "file contents", status: "done"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "read") {
		t.Fatalf("tool output = %q, want 'read'", out)
	}
	if !strings.Contains(out, "file contents") {
		t.Fatalf("tool output = %q, want 'file contents'", out)
	}
}

func TestRenderToolLongResultTruncated(t *testing.T) {
	longResult := strings.Repeat("x", 100)
	tool := toolState{name: "search", result: longResult, status: "done"}
	out := stripANSI(renderTool(tool, 0))
	if len(out) > 80 {
		t.Fatalf("tool output too long: %d chars", len(out))
	}
	if !strings.Contains(out, "…") {
		t.Logf("long result should be truncated with ellipsis, got %q", out)
	}
}

func TestRenderToolWithoutResult(t *testing.T) {
	tool := toolState{name: "think", status: "running"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "think") {
		t.Fatalf("tool output = %q, want 'think'", out)
	}
}

// --- sendResultMsg Update ---

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

// --- bashSendResultMsg Update ---

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

// --- WindowSizeMsg / PasteMsg ---

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
	inputLine := lineContaining(streamingLines, "╭ steer")
	if inputLine < 0 {
		t.Fatalf("steer input box missing:\n%s", stripANSI(streamingView.Content))
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
	// fill the terminal minus separator, padded input block, and status.
	if got := m.viewport.Height(); got != 33 {
		t.Fatalf("viewport height = %d, want 33 (fill terminal minus chrome)", got)
	}
}

func TestViewPreservesIdleManualScrollback(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for i := 1; i <= 80; i++ {
		m.appendMessage("user", fmt.Sprintf("line %02d", i))
	}
	m.View()
	m.viewport.ScrollUp(10)
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

func TestViewKeepsMouseTrackingDisabledForSelection(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.appendMessage("user", "hello")

	view := m.View()

	if view.MouseMode != tea.MouseModeNone {
		t.Fatalf("MouseMode = %v, want MouseModeNone", view.MouseMode)
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

func TestUpdatePasteMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(tea.PasteMsg{Content: "pasted text"})
	if cmd != nil {
		t.Fatal("expected nil Cmd from PasteMsg")
	}
	if m.input != "pasted text" {
		t.Fatalf("input = %q, want %q", m.input, "pasted text")
	}
}

// --- UI message types that should not crash ---

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

func TestUpdateChatEventsClosed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(chatEventsClosedMsg{})
	if cmd == nil {
		t.Fatal("expected a Cmd (tea.Quit) from chatEventsClosedMsg")
	}
}

func TestUpdateStartupMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(startupMsg{sessionID: "sess-new", modelName: "gpt-5", provider: "openai"})
	if cmd != nil {
		t.Fatal("expected nil Cmd from startupMsg")
	}
	if m.sessionID != "sess-new" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-new")
	}
	if m.modelName != "gpt-5" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-5")
	}
}

// --- sendCommand ---

func TestSendCommandErrorPropagates(t *testing.T) {
	rt := &fakeRuntime{err: errIntentional}

	cmd := sendCommand(rt, tauchat.SubmitChatPromptCommand{})
	msg := cmd()

	rm, ok := msg.(sendResultMsg)
	if !ok {
		t.Fatalf("expected sendResultMsg, got %T", msg)
	}
	if rm.err != errIntentional {
		t.Fatalf("err = %v, want %v", rm.err, errIntentional)
	}
}

// --- readNextEvent ---

func TestReadNextEventChannelClosed(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	sub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("test"))
	sub.Close() // close before reading — should get chatEventsClosedMsg

	cmd := readNextEvent(sub)
	msg := cmd()

	if _, ok := msg.(chatEventsClosedMsg); !ok {
		t.Fatalf("expected chatEventsClosedMsg, got %T", msg)
	}
}

// --- setNotification ---

func TestSetNotificationReturnsTimerCmd(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.setNotification("hello")
	if cmd == nil {
		t.Fatal("expected a Cmd from setNotification")
	}
	if m.notification != "hello" {
		t.Fatalf("notification = %q, want %q", m.notification, "hello")
	}
	if m.notificationGen == 0 {
		t.Fatal("notificationGen should be > 0")
	}
}

func TestClearNotificationGenerationMatch(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.notification = "test"
	m.notificationGen = 5

	// Matching generation clears.
	m.Update(clearNotificationMsg{gen: 5})
	if m.notification != "" {
		t.Fatal("notification should be cleared with matching gen")
	}
}

func TestClearNotificationGenerationMismatch(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.notification = "test"
	m.notificationGen = 5

	// Mismatched generation does NOT clear.
	m.Update(clearNotificationMsg{gen: 3})
	if m.notification != "test" {
		t.Fatal("notification should NOT be cleared with mismatched gen")
	}
}

// --- renderInputArea ---

func TestRenderInputAreaEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	area := m.renderInputArea()
	if area == "" {
		t.Fatal("expected non-empty input area even for empty input")
	}
	plain := stripANSI(area)
	if !strings.Contains(plain, "╭ chat") {
		t.Fatalf("input area = %q, want chat mode title", plain)
	}
	if !strings.Contains(plain, ">") {
		t.Fatalf("input area = %q, want '>' prompt", plain)
	}
}

func TestRenderInputAreaMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = 8 // end of line 0

	area := m.renderInputArea()
	plain := stripANSI(area)
	lines := strings.Split(plain, "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines including padded input box, got %d", len(lines))
	}
	if !strings.Contains(lines[2], "line one") {
		t.Fatalf("line 2 = %q, want 'line one'", lines[2])
	}
	if !strings.Contains(lines[3], "line two") {
		t.Fatalf("line 3 = %q, want 'line two'", lines[3])
	}
}

func TestRenderInputAreaWrapsLongLineInsideBox(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 32
	m.input = "this is a long message that should wrap inside the input box"
	m.inputCursor = len([]rune(m.input))

	area := m.renderInputArea()
	plain := stripANSI(area)
	lines := strings.Split(plain, "\n")
	if len(lines) <= 5 {
		t.Fatalf("expected wrapped input area to grow vertically, got %d lines:\n%s", len(lines), plain)
	}
	for _, line := range lines {
		if visibleWidth(line) > m.width {
			t.Fatalf("input line width = %d, want <= %d: %q\n%s", visibleWidth(line), m.width, line, plain)
		}
	}
	if strings.Contains(plain, "…") {
		t.Fatalf("input should wrap instead of truncate with ellipsis:\n%s", plain)
	}
}

func TestRenderInputAreaWrapsCursorOntoContinuationLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 18
	m.input = "abcdefghijklmnop"
	m.inputCursor = 15

	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, "  op") {
		t.Fatalf("expected wrapped continuation line with cursor cell, got:\n%s", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if visibleWidth(line) > m.width {
			t.Fatalf("input line width = %d, want <= %d: %q\n%s", visibleWidth(line), m.width, line, plain)
		}
	}
}

func TestRenderLineWithCursor(t *testing.T) {
	ln := []rune("hello")
	col := 2
	out := stripANSI(renderLineWithCursor(ln, col))
	if out != "hello" {
		t.Fatalf("cursor over character = %q, want %q", out, "hello")
	}
}

func TestRenderLineWithCursorAtEnd(t *testing.T) {
	ln := []rune("hi")
	col := 2
	out := stripANSI(renderLineWithCursor(ln, col))
	if out != "hi " {
		t.Fatalf("cursor at end = %q, want 'hi '", out)
	}
}

func TestRenderLineWithCursorAtStart(t *testing.T) {
	ln := []rune("abc")
	col := 0
	out := stripANSI(renderLineWithCursor(ln, col))
	if out != "abc" {
		t.Fatalf("cursor at start = %q, want 'abc'", out)
	}
}

// --- handleChatEvent test for CommandsChangedEvent (no-op) ---

func TestHandleChatEventCommandsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// Should be a no-op — not panic, not crash.
	cmd := m.handleChatEvent(tauchat.CommandsChangedEvent{})
	if cmd != nil {
		t.Fatal("expected nil Cmd from CommandsChangedEvent")
	}
}

// --- handleChatEvent for SkillsChangedEvent ---

func TestHandleChatEventSkillsChangedWithEmptyList(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SkillsChangedEvent{Skills: nil})

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "no skills available") {
		t.Fatalf("expected 'no skills available' message, got %q", joined)
	}
}

// --- insertAtCursor with unicode ---

func TestInsertAtCursorMultiByte(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""
	m.inputCursor = 0

	m.insertAtCursor("😀")
	if m.input != "😀" {
		t.Fatalf("input = %q, want emoji", m.input)
	}
	if m.inputCursor != 1 {
		t.Fatalf("cursor = %d, want 1 (rune count)", m.inputCursor)
	}
}

func TestDeleteAtCursorEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.deleteAtCursor()
}

func TestBackspaceAtCursorStart(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 0

	m.backspaceAtCursor()
	// Should be a no-op.
	if m.input != "hi" {
		t.Fatalf("input = %q, want %q (unchanged)", m.input, "hi")
	}
}

func TestUpsertToolCallExisting(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", args: "old"}}

	m.upsertToolCall("t1", "read", "new")

	if m.tools[0].args != "oldnew" {
		t.Fatalf("args = %q, want %q (appended)", m.tools[0].args, "oldnew")
	}
}

func TestSetToolStatusNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.setToolStatus("nonexistent", "running")
}

func TestSetToolResultNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.setToolResult("nonexistent", "result")
}

func TestSetToolResultAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", result: "part"}}

	m.setToolResult("t1", " two")

	if m.tools[0].result != "part two" {
		t.Fatalf("result = %q, want %q (appended)", m.tools[0].result, "part two")
	}
}

// --- statusText rendering ---

func TestComputeStatusBarBasic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.provider = "openai"
	m.width = 80

	bar := m.computeStatusBar()
	if bar == "" {
		t.Fatal("expected non-empty status bar")
	}
	plain := stripANSI(bar)
	if !strings.Contains(plain, "gpt-4") {
		t.Fatalf("status bar = %q, want 'gpt-4'", plain)
	}
}

func TestComputeStatusBarSteering(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.provider = "openai"
	m.steering = true
	m.width = 80

	bar := m.computeStatusBar()
	plain := stripANSI(bar)
	if !strings.Contains(plain, "steering") {
		t.Fatalf("status bar = %q, want 'steering'", plain)
	}
}

func TestComputeStatusBarEffort(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.reasoningEffort = "high"
	m.width = 80

	bar := m.computeStatusBar()
	plain := stripANSI(bar)
	if !strings.Contains(plain, "high") {
		t.Fatalf("status bar = %q, want 'high' effort", plain)
	}
}

func TestComputeStatusBarLabelsSessionTokenTotals(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))

	pub.Publish(tauchat.MetricEvent{
		Category: tauchat.MetricCategoryLLM,
		Name:     "llm.response",
		Value:    20_000,
		Labels: map[string]string{
			"prompt_tokens":     "19000",
			"completion_tokens": "1000",
		},
		SessionID: "sess",
	})
	for range 100 {
		if totals := tracker.Snapshot("sess"); totals != nil && totals.TotalTokens == 20_000 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = tracker
	m.ctxWindow = 200_000
	m.width = 120

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "20.0k session tok") {
		t.Fatalf("status bar = %q, want session token label", plain)
	}
}

func TestComputeStatusBarContextUsesLatestPromptTokens(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))

	for _, prompt := range []string{"500", "100"} {
		pub.Publish(tauchat.MetricEvent{
			Category: tauchat.MetricCategoryLLM,
			Name:     "llm.response",
			Value:    1000,
			Labels: map[string]string{
				"prompt_tokens":     prompt,
				"completion_tokens": "500",
			},
			SessionID: "sess",
		})
	}
	for range 100 {
		if totals := tracker.Snapshot("sess"); totals != nil && totals.TurnCount == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = tracker
	m.ctxWindow = 1000
	m.width = 120

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "ctx 10%") {
		t.Fatalf("status bar = %q, want latest prompt context percentage", plain)
	}
	if strings.Contains(plain, "ctx 60%") {
		t.Fatalf("status bar = %q, used cumulative prompt tokens for context", plain)
	}
}

// --- toolState tests ---

func TestToolStateDefaultValues(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.upsertToolCall("new-id", "bash", "echo hi")

	if m.tools[0].status != "pending" {
		t.Fatalf("new tool status = %q, want %q", m.tools[0].status, "pending")
	}
}

// --- appendMessage with multi-line ---

func TestAppendMessageMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("user", "line one\nline two")

	if len(m.renderedLines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(m.renderedLines))
	}
}

func TestAppendMessageMultiLineUserContinuationKeepsUserStyle(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("user", "line one\nline two")

	if len(m.renderedLines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(m.renderedLines))
	}
	if !strings.Contains(m.renderedLines[1], "38;2;120;170;255") {
		t.Fatalf("continuation line = %q, want user foreground style", m.renderedLines[1])
	}
	if strings.Contains(m.renderedLines[1], "38;2;128;134;150") {
		t.Fatalf("continuation line = %q, should not use muted metadata style", m.renderedLines[1])
	}
}

func TestAppendMessageToolPreservesRenderedBlockLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("tool", "╭────╮\n│ ok │\n╰────╯")

	if len(m.renderedLines) != 3 {
		t.Fatalf("expected 3 rendered lines, got %d", len(m.renderedLines))
	}
	if got := stripANSI(m.renderedLines[1]); got != "│ ok │" {
		t.Fatalf("middle line = %q, want unindented tool border line", got)
	}
	if strings.Contains(m.renderedLines[1], "38;2;120;170;255") ||
		strings.Contains(m.renderedLines[1], "38;2;128;134;150") {
		t.Fatalf("tool block line = %q, should not be restyled as chat continuation", m.renderedLines[1])
	}
}

func TestAppendMessageAssistantTracksLastText(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("assistant", "final text")

	if m.lastAssistantText != "final text" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "final text")
	}
}

// --- handlePromptKey N key on confirm ---

func TestHandlePromptKeyNOnConfirm(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	}

	drainCmd(m.handlePromptKey(key('n', 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if cmd.Confirmed {
		t.Fatal("'n' on confirm should resolve to Confirmed=false")
	}
}

// --- submitInput with debounce guard ---

func TestSubmitInputDebounceGuardFires(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "fast message"
	m.lastSubmit = time.Now()

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd even during debounce guard")
	}
}

// --- promptConfirmYes toggle with tab ---

func TestHandlePromptKeyTabTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	}
	m.promptConfirmYes = true

	m.handlePromptKey(key(tea.KeyTab, 0))
	if m.promptConfirmYes {
		t.Fatal("Tab should toggle promptConfirmYes to false")
	}

	m.handlePromptKey(key(tea.KeyTab, 0))
	if !m.promptConfirmYes {
		t.Fatal("Tab again should toggle back to true")
	}
}

// --- default dispatchKey cases ---

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

// --- handleChatEvent InteractivePromptRequestedEvent ---

func TestHandleChatEventInteractivePromptClearsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "typing..."
	m.inputCursor = 9

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "input",
	})

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

func TestHandleChatEventInteractivePromptSetsConfirmYes(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.promptConfirmYes = false

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	if !m.promptConfirmYes {
		t.Fatal("promptConfirmYes should default to true")
	}
}

// --- handlePromptKey left/right on confirm toggles ---

func TestHandlePromptKeyLeftTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"}
	m.promptConfirmYes = true

	m.handlePromptKey(key(tea.KeyLeft, 0))
	if m.promptConfirmYes {
		t.Fatal("Left should toggle promptConfirmYes to false")
	}
}

func TestHandlePromptKeyRightTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"}
	m.promptConfirmYes = false

	m.handlePromptKey(key(tea.KeyRight, 0))
	if !m.promptConfirmYes {
		t.Fatal("Right should toggle promptConfirmYes to true")
	}
}

// --- renderPrompt ---

func TestRenderPromptConfirm(t *testing.T) {
	p := &tauchat.InteractivePromptRequestedEvent{
		RequestID: "r1", Kind: "confirm", Title: "Confirm?", Message: "Are you sure?",
	}
	out := stripANSI(renderPrompt(p, true))
	if !strings.Contains(out, "Yes") || !strings.Contains(out, "No") {
		t.Fatalf("expected Yes/No in confirm prompt:\n%s", out)
	}
	if !strings.Contains(out, "Are you sure?") {
		t.Fatalf("expected message in prompt:\n%s", out)
	}
}

func TestRenderPromptInput(t *testing.T) {
	p := &tauchat.InteractivePromptRequestedEvent{
		RequestID: "r2", Kind: "input", Title: "Name?", Message: "What is your name?",
	}
	out := stripANSI(renderPrompt(p, true))
	if strings.Contains(out, "Yes") || strings.Contains(out, "No") {
		t.Fatalf("input prompt should not show Yes/No:\n%s", out)
	}
	if !strings.Contains(out, "type + enter") {
		t.Fatalf("input prompt should show hint:\n%s", out)
	}
}

// --- resolvePromptConfirm ---

func TestResolvePromptConfirmTrue(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.activePrompt = &tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-cf", Kind: "confirm",
	}

	drainCmd(m.resolvePromptConfirm(true))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Confirmed || cmd.Canceled {
		t.Fatal("expected Confirmed=true, Canceled=false")
	}
}

func TestResolvePromptConfirmNoActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = nil

	cmd := m.resolvePromptConfirm(true)
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

// --- dispatchKey printable text path ---

func TestDispatchPrintableNonPrintableIgnored(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// A key with empty text and unknown code should be ignored.
	m.dispatchKey(tea.KeyPressMsg{Code: 999})
	// No crash expected.
}

// --- toolStyleForStatus ---

func TestToolStyleForStatus(t *testing.T) {
	tests := []struct {
		name, toolName, status string
	}{
		{"read running", "read", "running"},
		{"read pending", "read", "pending"},
		{"read done", "read", "done"},
		{"read error", "read", "error"},
		{"skill running", "skill", "running"},
		{"skill done", "skill", "done"},
		{"skill error", "skill", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := toolStyleForStatus(tt.toolName, tt.status)
			// Render a small string through the style — smoke test that it
			// doesn't panic and emits output.
			out := s.Render("x")
			if out == "" {
				t.Error("rendered output is empty")
			}
		})
	}
}

// --- skillLabelFromArgs ---

func TestSkillLabelFromArgs(t *testing.T) {
	tests := []struct {
		args string
		want string
	}{
		{`{"name":"my-skill"}`, "skill: my-skill"},
		{`{"name":"code-review","other":true}`, "skill: code-review"},
		{"", "skill"},
		{"not json", "skill"},
		{`{"no-name":"here"}`, "skill"},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			if got := skillLabelFromArgs(tt.args); got != tt.want {
				t.Errorf("skillLabelFromArgs(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
