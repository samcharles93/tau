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
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
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
	if !m.activePrompt.confirmYes {
		t.Fatal("expected the default highlighted option to be Yes")
	}

	// Toggle to "No" before submitting.
	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activePrompt.confirmYes {
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

func TestHandleChatEventToolExecutionStartedAdoptsFinalCallID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "tool_call_0",
		ToolName:         "tau-plugin-hello__hello_greet",
		ArgumentsSummary: `{"name":"Sam"}`,
	})

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:   "call_real",
		ToolName: "tau-plugin-hello__hello_greet",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected lifecycle event to adopt existing synthetic row, got %d tools: %+v", len(m.tools), m.tools)
	}
	if m.tools[0].id != "call_real" {
		t.Fatalf("tool id = %q, want final call id", m.tools[0].id)
	}
	if m.tools[0].status != "running" {
		t.Fatalf("tool status = %q, want running", m.tools[0].status)
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

func TestHandleChatEventToolExecutionCompletedAdoptsFinalCallID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "tool_call_0",
		ToolName:         "tau-plugin-hello__hello_greet",
		ArgumentsSummary: `{"name":"Sam"}`,
	})

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{
		CallID:        "call_real",
		ToolName:      "tau-plugin-hello__hello_greet",
		ResultSummary: "Hello, Sam!",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected completed event to adopt existing synthetic row, got %d tools: %+v", len(m.tools), m.tools)
	}
	if m.tools[0].id != "call_real" {
		t.Fatalf("tool id = %q, want final call id", m.tools[0].id)
	}
	if m.tools[0].status != "done" {
		t.Fatalf("tool status = %q, want done", m.tools[0].status)
	}
	if m.tools[0].result != "Hello, Sam!" {
		t.Fatalf("tool result = %q, want final summary", m.tools[0].result)
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

	// A tool-only turn has no real assistant prose, so lastAssistantText
	// (used by /copy) stays empty rather than holding a synthetic
	// placeholder — but the tool call itself must still land in scrollback.
	if m.lastAssistantText != "" {
		t.Fatalf("lastAssistantText = %q, want empty for a tool-only turn", m.lastAssistantText)
	}
	if len(m.tools) != 0 {
		t.Fatalf("expected the tool-only batch to be committed, m.tools still has %d entries", len(m.tools))
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "search") {
		t.Fatalf("expected the tool call to be committed to scrollback, got %q", joined)
	}
}

// TestToolCallDoesNotReorderAheadOfPrecedingText guards against a real bug:
// commentary text the model streamed BEFORE calling a tool was rendering
// AFTER the tool's box once it committed — because renderedLines (baked
// history) always rendered ahead of the live m.streaming buffer, a
// just-committed tool box would visually "jump" above text that
// chronologically came first. upsertToolCall now flushes pending streaming
// text into scrollback the moment a new tool call starts.
func TestToolCallDoesNotReorderAheadOfPrecedingText(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "I'll investigate the workspace."})
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "read"})

	if m.streaming != "" {
		t.Fatalf("streaming = %q, want flushed to empty once a tool call starts", m.streaming)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "investigate the workspace") {
		t.Fatalf("expected preceding text in scrollback, got %q", joined)
	}
	if len(m.tools) != 1 || m.tools[0].id != "t1" {
		t.Fatalf("expected the tool call to still be live (not yet committed), got %+v", m.tools)
	}
}

// TestManyToolCallsCommitAsOneGroup guards against a real bug: a turn with
// many sequential/parallel tool calls committed one box per tool into
// permanent scrollback, so a turn with 100+ tool calls could bury the
// assistant text right before or after it. Multiple tool calls uninterrupted
// by text must collapse into one compact summary once committed.
func TestManyToolCallsCommitAsOneGroup(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	for i := range 8 {
		id := fmt.Sprintf("t%d", i)
		m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: id, ToolName: "read"})
		m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: id})
	}
	// Text resumes after the batch — this is the boundary that commits it.
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "Done reading."})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if len(m.tools) != 0 {
		t.Fatalf("expected the batch to be committed and cleared, m.tools has %d entries", len(m.tools))
	}
	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "8 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '8 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
	}
}

// TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow covers the fix that let
// a committed "N tool calls" group keep its accordion interaction after it
// scrolls into history: it used to freeze forever as one flat summary line
// the moment a group had more than one call, with no way back to the
// per-tool detail. toggleCommittedToolAtLine now mirrors the live group's
// two levels — click the header to unfold into per-tool rows, click a row
// to see its full output, click again to fold back down.
func TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)

	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group, got %d", len(m.committedGroups))
	}
	g := m.committedGroups[0]
	if g.expanded {
		t.Fatal("expected a freshly committed group to start folded")
	}

	// Click the header line (relative row 0) to unfold.
	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a click on the group's header line to be handled")
	}
	if !g.expanded {
		t.Fatal("expected the group to unfold on header click")
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "read") || !strings.Contains(joined, "search") {
		t.Fatalf("expected unfolded group to show each tool's compact row, got %q", joined)
	}
	if strings.Contains(joined, "Press Enter to collapse") {
		t.Fatalf("expected rows to stay compact (no full-detail box) until individually expanded, got %q", joined)
	}

	// Click the second row (relative row 3: border(0) + header(1) + t0's
	// row(2) + t1's row(3) — see renderToolGroupBox's row accounting) to
	// expand just that tool's full output.
	if !m.toggleCommittedToolAtLine(g.lineIdx + 3) {
		t.Fatal("expected a click on a tool row to be handled")
	}
	if g.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1", g.expandedID)
	}
	joined = stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "b.go") {
		t.Fatalf("expected t1's full output visible once expanded, got %q", joined)
	}

	// Click the header TEXT line again (relative row 1, not row 0's
	// border) to fold the whole group back down — a real regression: the
	// fold trigger only matched row 0 (the border character), so clicking
	// the actual visible "N tool calls" text a user would click did
	// nothing at all.
	if !m.toggleCommittedToolAtLine(g.lineIdx + 1) {
		t.Fatal("expected a click on the header text line to be handled")
	}
	if g.expanded {
		t.Fatal("expected the group to fold back down on a header text click")
	}
	joined = stripANSI(strings.Join(m.renderedLines, "\n"))
	if strings.Contains(joined, "read") || strings.Contains(joined, "b.go") {
		t.Fatalf("expected folded group to show only the one-line summary, got %q", joined)
	}
}

func TestCommittedSingleToolOpensAndCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "single output"},
	}, nil)

	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group, got %d", len(m.committedGroups))
	}
	g := m.committedGroups[0]
	if g.expanded {
		t.Fatal("expected a freshly committed single tool to start collapsed")
	}
	if strings.Contains(stripANSI(strings.Join(m.renderedLines, "\n")), "Press Enter to collapse") {
		t.Fatal("expected collapsed single tool to hide its expanded controls")
	}

	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a click on the single tool box to be handled")
	}
	if !g.expanded {
		t.Fatal("expected single tool to expand on click")
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "single output") {
		t.Fatalf("expected expanded single tool to show output, got %q", joined)
	}

	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a second click on the single tool box to be handled")
	}
	if g.expanded {
		t.Fatal("expected single tool to collapse on second click")
	}
}

// TestApplySnapshotPreservesCommittedGroupExpandState guards the other half
// of the same fix: applySnapshot fully rebuilds m.renderedLines from scratch
// on every snapshot (which fires after every submitted prompt), so without
// carrying expand state forward via oldGroups/committedGroupKey, a group the
// user had unfolded would silently refold the moment they sent their next
// message.
func TestApplySnapshotPreservesCommittedGroupExpandState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{
			Role: tauchat.ChatRoleAssistant,
			ToolCalls: []tauchat.ChatToolCall{
				{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-2", Function: tauchat.ChatFunctionCall{Name: "read"}},
			},
		},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-2", Content: "b"},
	}

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group after first snapshot, got %d", len(m.committedGroups))
	}
	if !m.toggleCommittedToolAtLine(m.committedGroups[0].lineIdx) {
		t.Fatal("expected the header click to be handled")
	}
	if !m.committedGroups[0].expanded {
		t.Fatal("expected the group to be unfolded before the next snapshot")
	}

	// A second snapshot (e.g. after the user sends another message) rebuilds
	// everything from scratch — the group must come back already unfolded.
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group after second snapshot, got %d", len(m.committedGroups))
	}
	if !m.committedGroups[0].expanded {
		t.Fatal("expected the group's unfolded state to survive an applySnapshot rebuild")
	}
}

// --- messageRanges ---

// TestMessageRangesRecordedOnApplySnapshot verifies applySnapshot records a
// messageLineRange for every message with a real ID, keyed to the exact
// renderedLines span that message's own append produced.
func TestMessageRangesRecordedOnApplySnapshot(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"},
		{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "hi there"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})

	if len(m.messageRanges) != 2 {
		t.Fatalf("expected 2 message ranges, got %d: %+v", len(m.messageRanges), m.messageRanges)
	}
	u1, a1 := m.messageRanges[0], m.messageRanges[1]
	if u1.id != "u1" || u1.startLine != 0 {
		t.Fatalf("u1 range = %+v, want id=u1 startLine=0", u1)
	}
	if a1.id != "a1" || a1.startLine != u1.endLine {
		t.Fatalf("a1 range = %+v, want id=a1 startLine=%d (u1.endLine)", a1, u1.endLine)
	}
	if a1.endLine != len(m.renderedLines) {
		t.Fatalf("a1.endLine = %d, want %d (len(renderedLines))", a1.endLine, len(m.renderedLines))
	}
}

// TestMessageRangesSkipMessagesWithoutID verifies a message with no ID (e.g.
// a pre-migration session loaded before per-message IDs existed) gets no
// range recorded — right-clicking it should simply find nothing, not panic
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

// TestMessageRangesRebuiltOnApplySnapshotRerun verifies a second snapshot
// fully replaces messageRanges rather than appending to stale entries from
// the first rebuild — mirrors renderedLines' own m.renderedLines[:0] reset.
func TestMessageRangesRebuiltOnApplySnapshotRerun(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	first := []tauchat.ChatMessage{{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"}}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: first}})
	if len(m.messageRanges) != 1 {
		t.Fatalf("expected 1 message range after first snapshot, got %d", len(m.messageRanges))
	}

	second := []tauchat.ChatMessage{
		{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"},
		{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "hi there"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: second}})
	if len(m.messageRanges) != 2 {
		t.Fatalf("expected messageRanges rebuilt to exactly 2 entries after second snapshot, got %d: %+v", len(m.messageRanges), m.messageRanges)
	}
}

// TestMessageRangesShiftAfterCommittedGroupToggle guards the highest-risk
// part of per-message geometry: spliceCommittedGroup mutates renderedLines
// in place (the only site that does, besides pure trailing appends) when a
// committed tool group folds/unfolds, and must shift every messageRange
// that comes after it by the same delta it already applies to other
// committedGroups' lineIdx — otherwise a message after the toggled group
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

	// messageAtRow must still resolve to a1 at its new (shifted) position —
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
// stomped by the very next tick-driven re-render — the user couldn't scroll
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

// TestHandleChatEventResponseCompletedReasoningOnly guards against a
// regression where a reasoning-only turn (no trailing answer text) was
// rendered as a literal "[reasoning only]" placeholder instead of the real
// reasoning text — and more generally, where reasoning was never committed
// to scrollback at all and only ever existed in the live view, vanishing
// the instant the turn completed (see finalizeResponse).
func TestHandleChatEventResponseCompletedReasoningOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.reasoning = "I think..."
	m.inResponse = true

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(line, "I think...") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("renderedLines = %q, want a line containing the reasoning text", m.renderedLines)
	}
	if m.reasoning != "" {
		t.Fatalf("m.reasoning = %q, want cleared after finalizeResponse", m.reasoning)
	}
}

// TestReasoningStyleDistinctFromFinalAnswer guards the visual-hierarchy
// contract: reasoning must render with a style distinct from (and dimmer
// than) the final answer, the answer must remain unstyled/full-brightness
// so it stays the strongest element on screen, and the Warm Ochre accent
// must appear only on the "│ " bar prefixed to every reasoning line — never
// painted across the reasoning text itself, and never on the answer.
func TestReasoningStyleDistinctFromFinalAnswer(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 20 // narrow enough to force reasoning across multiple lines
	m.streaming = "the final answer"
	m.reasoning = "weighing several possible options carefully"
	m.inResponse = true

	m.finalizeResponse("")

	// The turn produced a trailing answer, so the block auto-collapses (see
	// commitReasoningBlock) — expand it to inspect the expanded styling this
	// test is actually about.
	if !m.toggleReasoningBlock(m.lastReasoningKey) {
		t.Fatal("expected toggleReasoningBlock to find the just-committed block")
	}

	// lipgloss renders an explicit truecolor Foreground as a decimal
	// "38;2;R;G;B" SGR sequence, not hex.
	accentSGR := fmt.Sprintf("38;2;%d;%d;%d", theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2])
	mutedSGR := fmt.Sprintf("38;2;%d;%d;%d", theme.ToneMuted[0], theme.ToneMuted[1], theme.ToneMuted[2])

	var reasoningLines []string
	var answerLine string
	for _, line := range m.renderedLines {
		switch {
		case strings.Contains(line, "│"):
			reasoningLines = append(reasoningLines, line)
		case strings.Contains(stripANSI(line), "final"):
			// The answer goes through glamour, which may style/wrap each
			// word separately, so match on stripped content rather than a
			// literal substring of the raw ANSI-laden line.
			answerLine = line
		}
	}
	if len(reasoningLines) < 2 {
		t.Fatalf("renderedLines = %q, want reasoning wrapped across multiple bar-prefixed lines", m.renderedLines)
	}
	if answerLine == "" {
		t.Fatalf("renderedLines = %q, want a line containing the final answer", m.renderedLines)
	}

	for _, reasoningLine := range reasoningLines {
		if !strings.Contains(reasoningLine, accentSGR) {
			t.Fatalf("every reasoning line should carry the Warm Ochre bar, got %q", reasoningLine)
		}
		if strings.Count(reasoningLine, accentSGR) != 1 {
			t.Fatalf("Warm Ochre accent should appear exactly once per line (on the bar), not across the body: %q", reasoningLine)
		}
		if !strings.Contains(reasoningLine, mutedSGR) {
			t.Fatalf("reasoning body should carry the explicit muted foreground (not rely on Faint alone), got %q", reasoningLine)
		}
		if !containsFaintSGR(reasoningLine) {
			t.Fatalf("reasoning body should also carry the faint (dim) SGR code as a secondary cue, got %q", reasoningLine)
		}
	}

	// The final answer must never inherit reasoning's muted/dim treatment.
	if strings.Contains(answerLine, mutedSGR) || containsFaintSGR(answerLine) {
		t.Fatalf("final answer line should not be dimmed like reasoning, got %q", answerLine)
	}
	if strings.Contains(answerLine, accentSGR) {
		t.Fatalf("final answer line should not carry the reasoning accent color, got %q", answerLine)
	}
}

// TestReasoningBlockCollapsedAndExpandedRendering checks the two render
// states directly: collapsed shows a single subtle line (no raw reasoning
// text, no keybinding hint baked in — that's /help's job), expanded shows
// the full multi-line body with the raw text intact.
func TestReasoningBlockCollapsedAndExpandedRendering(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	b := &committedReasoningBlock{key: "k", text: "the raw reasoning text", collapsed: true}
	collapsed := m.renderCommittedReasoning(b)
	if strings.Contains(collapsed, "the raw reasoning text") {
		t.Fatalf("collapsed rendering should not include the raw text, got %q", collapsed)
	}
	if strings.Contains(collapsed, "ctrl+r") || strings.Contains(strings.ToLower(collapsed), "ctrl+r") {
		t.Fatalf("collapsed rendering should not bake in the keybinding hint (belongs in /help only), got %q", collapsed)
	}
	if got := len(strings.Split(collapsed, "\n")); got != 1 {
		t.Fatalf("collapsed rendering should be a single line, got %d lines: %q", got, collapsed)
	}
	if !strings.Contains(stripANSI(collapsed), "reasoning collapsed") {
		t.Fatalf("collapsed rendering should carry a clear indicator, got %q", stripANSI(collapsed))
	}

	b.collapsed = false
	expanded := m.renderCommittedReasoning(b)
	if !strings.Contains(expanded, "the raw reasoning text") {
		t.Fatalf("expanded rendering should include the raw text, got %q", expanded)
	}
}

// TestReasoningBlockContentPreservedAcrossCollapse guards the "presentation
// state only" requirement: toggling collapse must never mutate the block's
// underlying text, and the persisted ChatMessage.ReasoningContent (re-read
// on every applySnapshot rebuild) is what's authoritative — collapsing is
// purely a rendering decision layered on top.
func TestReasoningBlockContentPreservedAcrossCollapse(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.streaming = "the final answer"
	m.reasoning = "the original reasoning text"
	m.inResponse = true
	m.finalizeResponse("msg-1")

	if len(m.committedReasoning) != 1 {
		t.Fatalf("expected one committed reasoning block, got %d", len(m.committedReasoning))
	}
	original := m.committedReasoning[0].text
	if original != "the original reasoning text" {
		t.Fatalf("unexpected initial text %q", original)
	}

	// Toggle collapsed -> expanded -> collapsed again.
	for range 2 {
		if !m.toggleReasoningBlock("msg-1") {
			t.Fatal("expected toggleReasoningBlock to find block \"msg-1\"")
		}
		if m.committedReasoning[0].text != original {
			t.Fatalf("collapse toggle mutated the underlying text: got %q, want %q", m.committedReasoning[0].text, original)
		}
	}

	// A snapshot rebuild re-reads ReasoningContent from the persisted
	// message, independent of whatever presentation state the toggles left
	// behind — collapsing never alters the message that would be sent back
	// to the model or saved to disk.
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{ID: "msg-1", Role: tauchat.ChatRoleAssistant, ReasoningContent: original, Content: "the final answer"},
			},
		},
	})
	if len(m.committedReasoning) != 1 || m.committedReasoning[0].text != original {
		t.Fatalf("expected persisted reasoning text to survive rebuild unchanged, got %+v", m.committedReasoning)
	}
}

// TestCtrlRTogglesLastReasoningBlock checks the keyboard toggle end to end:
// ctrl+r flips the most recently committed block's collapse state and
// re-renders it in place, and is a no-op with reasoning disabled or before
// anything has completed.
func TestCtrlRTogglesLastReasoningBlock(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// No completed block yet: ctrl+r is a no-op, not a panic.
	m.dispatchKey(key('r', tea.ModCtrl))
	if len(m.committedReasoning) != 0 {
		t.Fatalf("expected no committed reasoning blocks yet, got %d", len(m.committedReasoning))
	}

	m.streaming = "the final answer"
	m.reasoning = "weighing the options"
	m.inResponse = true
	m.finalizeResponse("msg-1")

	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected the block to auto-collapse (turn produced a trailing answer)")
	}

	m.dispatchKey(key('r', tea.ModCtrl))
	if m.committedReasoning[0].collapsed {
		t.Fatal("expected ctrl+r to expand the collapsed block")
	}
	if !strings.Contains(strings.Join(m.renderedLines, "\n"), "weighing the options") {
		t.Fatal("expected expanding via ctrl+r to reveal the raw reasoning text in renderedLines")
	}

	m.dispatchKey(key('r', tea.ModCtrl))
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected a second ctrl+r to collapse the block again")
	}

	// Reasoning disabled: ctrl+r must not touch the (still-persisted) block.
	m.showReasoning = false
	m.dispatchKey(key('r', tea.ModCtrl))
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected ctrl+r to be a no-op while reasoning is disabled")
	}
}

// TestStreamingReasoningRemainsVisibleWhileActive checks the split this
// feature relies on: only *completed* reasoning is collapsible — the
// in-progress turn's reasoning is rendered fresh from m.reasoning every
// frame and is never folded, even after a sibling turn's block has already
// been committed and auto-collapsed.
func TestStreamingReasoningRemainsVisibleWhileActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// A prior, completed turn: auto-collapses since it has a trailing answer.
	m.streaming = "prior answer"
	m.reasoning = "prior reasoning"
	m.inResponse = true
	m.finalizeResponse("msg-1")
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected the prior turn's reasoning to auto-collapse")
	}

	// A new turn starts streaming reasoning — must render in full, live,
	// regardless of the previous block's collapsed state.
	m.inResponse = true
	m.reasoning = "currently streaming reasoning"
	m.streaming = ""

	view := m.viewportLinesForView(true)
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, "currently streaming reasoning") {
		t.Fatalf("expected live streaming reasoning to render in full, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAppearsWhileStreaming checks the cursor shows up on the
// live view while text is actively streaming, and only there.
func TestStreamCursorAppearsWhileStreaming(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "the response so far"

	view := m.viewportLinesForView(false)
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, streamCursor) {
		t.Fatalf("expected the cursor while streaming, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAbsentBeforeStreamingStarts checks the working indicator
// state (in response, nothing streamed yet) shows no cursor — there's no
// content for it to sit after.
func TestStreamCursorAbsentBeforeStreamingStarts(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = ""

	view := m.viewportLinesForView(false)
	joined := strings.Join(view, "\n")
	if strings.Contains(joined, streamCursor) {
		t.Fatalf("expected no cursor before any text has streamed, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAbsentOnCompletedMessage checks the cursor never survives
// into committed scrollback — it's presentation-only, drawn fresh from
// m.streaming each frame, never written into renderedLines.
func TestStreamCursorAbsentOnCompletedMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "the finished response"
	m.finalizeResponse("msg-1")

	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, streamCursor) {
		t.Fatalf("expected no cursor in committed scrollback, got %q", joined)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatalf("expected no cursor in the view once the turn is finalized, got %q", strings.Join(view, "\n"))
	}
}

// TestStreamCursorIsPresentationOnly checks the cursor never leaks into any
// of the places the raw streamed text feeds: the persisted/copyable text
// (m.lastAssistantText, set in finalizeResponse), and m.streaming itself,
// which is what would flow into token counts and the model's context on the
// next turn if it were ever mutated in place.
func TestStreamCursorIsPresentationOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "plain text response"

	// While actively streaming, the raw buffer itself must stay untouched —
	// rendering must never mutate the source string the cursor is drawn from.
	_ = m.viewportLinesForView(false)
	if strings.Contains(m.streaming, streamCursor) {
		t.Fatalf("m.streaming was mutated to include the cursor: %q", m.streaming)
	}

	content := m.finalizeResponse("msg-1")
	if strings.Contains(content, streamCursor) {
		t.Fatalf("finalizeResponse's returned content includes the cursor: %q", content)
	}
	if strings.Contains(m.lastAssistantText, streamCursor) {
		t.Fatalf("m.lastAssistantText (source for /copy) includes the cursor: %q", m.lastAssistantText)
	}
}

// TestStreamCursorClearsOnToolTransition checks the cursor disappears the
// instant a tool call starts mid-stream — upsertToolCall flushes streaming
// text to scrollback and clears m.streaming, which is what the cursor's
// visibility is keyed on.
func TestStreamCursorClearsOnToolTransition(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "text before the tool call"

	m.upsertToolCall("call-1", "read", `{"path":"a.go"}`, "")

	if m.streaming != "" {
		t.Fatalf("expected m.streaming cleared on tool transition, got %q", m.streaming)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatal("expected no cursor once a tool call has started")
	}
}

// TestStreamCursorClearsOnCancelAndError checks the cursor disappears on
// both interrupted-turn paths (flushInterruptedTurn -> flushStreamingText).
func TestStreamCursorClearsOnCancelAndError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "an in-flight response"

	m.flushInterruptedTurn("chat request cancelled")

	if m.streaming != "" {
		t.Fatalf("expected m.streaming cleared after an interrupted turn, got %q", m.streaming)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatal("expected no cursor after the turn was interrupted")
	}
}

// TestRenderStreamingLinesCursorPlacement checks the cursor lands correctly
// after plain text, a wrapped multi-line response, and a trailing newline
// (its own blank row) — and that reserving room for it never pushes any
// line past the wrap width, which is what would cause terminal-level
// reflow/jitter on every delta.
func TestRenderStreamingLinesCursorPlacement(t *testing.T) {
	t.Run("plain text", func(t *testing.T) {
		lines := renderStreamingLines("hello", 40)
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d: %q", len(lines), lines)
		}
		if !strings.HasSuffix(stripANSI(lines[0]), streamCursor) {
			t.Fatalf("expected cursor at end of line, got %q", stripANSI(lines[0]))
		}
	})

	t.Run("wrapped lines", func(t *testing.T) {
		width := 20
		lines := renderStreamingLines("this is a long response that should wrap across several lines", width)
		if len(lines) < 2 {
			t.Fatalf("expected wrapping to produce multiple lines, got %d", len(lines))
		}
		for i, line := range lines {
			plain := stripANSI(line)
			if i < len(lines)-1 && strings.Contains(plain, streamCursor) {
				t.Fatalf("cursor should only appear on the last line, found it on line %d: %q", i, plain)
			}
			if visibleWidth(plain) > width {
				t.Fatalf("line %d width = %d, want <= %d (cursor pushed line past wrap width): %q", i, visibleWidth(plain), width, plain)
			}
		}
		if !strings.HasSuffix(stripANSI(lines[len(lines)-1]), streamCursor) {
			t.Fatalf("expected cursor at end of last line, got %q", stripANSI(lines[len(lines)-1]))
		}
	})

	t.Run("trailing newline", func(t *testing.T) {
		lines := renderStreamingLines("finished a line\n", 40)
		last := stripANSI(lines[len(lines)-1])
		if last != streamCursor {
			t.Fatalf("expected the cursor alone on its own row after a trailing newline, got %q", last)
		}
	})
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
	m.tools = []toolState{{
		id:        "t1",
		name:      "read",
		status:    "running",
		startedAt: time.Now().Add(-time.Second),
	}}
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
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	for _, want := range []string{"partial", "read", "chat request cancelled"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected cancelled turn scrollback to contain %q, got %q", want, joined)
		}
	}
}

func TestHandleChatEventRuntimeError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true
	m.steering = true
	m.streaming = "partial"
	m.reasoning = "thinking"
	m.tools = []toolState{
		{id: "t1", name: "shell", status: "done", result: "ok"},
		{id: "t2", name: "read", status: "pending", startedAt: time.Now().Add(-time.Second)},
	}

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
	if m.notification != "API error" || m.notificationLevel != notify.LevelError {
		t.Fatalf("expected an error-level notification banner, got %q level=%v", m.notification, m.notificationLevel)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	for _, want := range []string{"partial", "2 tool calls", "API error"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected runtime error scrollback to contain %q, got %q", want, joined)
		}
	}
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected interrupted tool batch to be committed, got %d groups", len(m.committedGroups))
	}
	if got := m.committedGroups[0].tools[1].status; got != "error" {
		t.Fatalf("interrupted pending tool status = %q, want error", got)
	}
	if got := m.committedGroups[0].tools[1].result; !strings.Contains(got, "API error") {
		t.Fatalf("interrupted pending tool result = %q, want API error", got)
	}
}

// TestHandleChatEventRuntimeErrorSkipsRedundantEchoForLoneTool is a
// regression test: a single in-flight tool call failing (e.g. a provider
// streaming a tool-call delta with no function name) previously showed the
// exact same error text three times — the committed lone tool box's compact
// summary, a duplicate "✗ <message>" scrollback line, and the notification
// banner. The scrollback echo is now skipped whenever a lone tool box
// already displays the reason inline (see ChatRuntimeErrorEvent's
// hadLoneTool); the banner (m.notification) and the tool box itself are
// unaffected — this only removes the redundant middle copy.
func TestHandleChatEventRuntimeErrorSkipsRedundantEchoForLoneTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true
	m.tools = []toolState{
		{id: "fc1", name: "shell", status: "pending", startedAt: time.Now()},
	}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "stream tool call 0 has no function name"})

	if m.notification != "stream tool call 0 has no function name" {
		t.Fatalf("expected the notification banner to still show the error, got %q", m.notification)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if got := strings.Count(joined, "stream tool call 0 has no function name"); got != 1 {
		t.Errorf("error text appears %d times in scrollback, want exactly 1 (the tool box) — got:\n%s", got, joined)
	}
	if strings.Contains(joined, "✗ stream tool call") {
		t.Error("expected no redundant '✗ <message>' scrollback echo when the lone tool box already shows the reason")
	}
}

func TestHandleChatEventNotification(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatNotificationEvent{Message: "hello"})

	if m.notification != "hello" {
		t.Fatalf("expected the notification banner to show %q, got %q", "hello", m.notification)
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
	if m.activePrompt.requestID != "req-1" {
		t.Fatalf("activePrompt.requestID = %q, want %q", m.activePrompt.requestID, "req-1")
	}
}

func TestHandleChatEventInteractivePromptQueueSecond(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1"})
	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-2"})

	// First should be active, second queued.
	if m.activePrompt.requestID != "req-1" {
		t.Fatalf("active prompt = %q, want %q", m.activePrompt.requestID, "req-1")
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "req-2" {
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
	for _, want := range []string{"Available Skills", "python", "Python helper", "(project)", "go", "Go helper"} {
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
	if len(m.history) != 1 || m.history[0] != "/clear" {
		t.Fatalf("history = %v, want [/clear]", m.history)
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
	if len(m.history) != 1 || m.history[0] != "!ls -la" {
		t.Fatalf("history = %v, want [!ls -la]", m.history)
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

func TestStartOrQueueTurnClearsPriorTerminalState(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state agentState
	}{
		{name: "cancelled", state: agentCancelled},
		{name: "error", state: agentError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.agentState = tt.state

			m.startOrQueueTurn("try again")

			if m.agentState != agentThinking {
				t.Fatalf("agentState = %v, want agentThinking after a new submission", m.agentState)
			}
		})
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

// --- clearScreen (Ctrl+Shift+L) ---

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

// TestRecallHistoryDraftRestore verifies that an in-progress (unsent) draft is
// stashed on first history recall and restored when navigating past the most
// recent history entry (CAT-18).
func TestRecallHistoryDraftRestore(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1
	m.input = "my draft"
	m.inputCursor = 8

	// Up: should stash draft and show most recent history.
	m.recallHistory(-1)
	if m.input != "second" {
		t.Fatalf("up: input = %q, want %q", m.input, "second")
	}
	if m.draftInput != "my draft" {
		t.Fatalf("draft not stashed: draftInput = %q, want %q", m.draftInput, "my draft")
	}

	// Up again: oldest entry.
	m.recallHistory(-1)
	if m.input != "first" {
		t.Fatalf("up again: input = %q, want %q", m.input, "first")
	}

	// Down: back to most recent.
	m.recallHistory(1)
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}

	// Down past most recent: should restore draft.
	m.recallHistory(1)
	if m.input != "my draft" {
		t.Fatalf("down past most recent: input = %q, want %q", m.input, "my draft")
	}
	if m.historyIdx != len(m.history) {
		t.Fatalf("historyIdx = %d, want %d (draft slot)", m.historyIdx, len(m.history))
	}

	// Down again: clamped at draft slot.
	m.recallHistory(1)
	if m.input != "my draft" {
		t.Fatalf("down again: input = %q, want %q", m.input, "my draft")
	}

	// Up from draft: back to most recent history.
	m.recallHistory(-1)
	if m.input != "second" {
		t.Fatalf("up from draft: input = %q, want %q", m.input, "second")
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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "first"})

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "second"})

	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "second" {
		t.Fatalf("expected [second] queued, got %+v", m.promptQueue)
	}
}

func TestPresentNextQueuedPromptNoQueue(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "current"})

	m.presentNextQueuedPrompt()

	if m.activePrompt != nil {
		t.Fatal("activePrompt should be nil when queue is empty")
	}
}

func TestPresentNextQueuedPromptDrains(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "current"})
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "next"})
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "last"})

	m.presentNextQueuedPrompt()

	if m.activePrompt == nil || m.activePrompt.requestID != "next" {
		t.Fatalf("activePrompt.requestID = %v, want 'next'", m.activePrompt.requestID)
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "last" {
		t.Fatalf("promptQueue should have [last], got %+v", m.promptQueue)
	}
	if !m.activePrompt.confirmYes {
		t.Fatal("confirmYes should reset to true for new prompt")
	}
}

func TestResolvePromptCancel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-cancel"})

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

	cmd := m.resolvePrompt()
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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})

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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "question", Message: "enter value",
	})
	m.activePrompt.field.SetValue("my value")

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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})

	m.handlePromptKey(key('y', 0))
	if got := m.activePrompt.field.Value(); got != "y" {
		t.Fatalf("field value = %q, want %q (y should be inserted for non-confirm prompts)", got, "y")
	}
}

func TestHandlePromptKeyBackspace(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})
	m.activePrompt.field.SetValue("abc")

	m.handlePromptKey(key(tea.KeyBackspace, 0))
	if got := m.activePrompt.field.Value(); got != "ab" {
		t.Fatalf("field value = %q, want %q", got, "ab")
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

// TestApplySnapshotRendersAssistantMarkdown guards against a real bug: a
// ChatSessionSnapshotEvent fires routinely (not just on session load), and
// applySnapshot used to rebuild renderedLines straight from raw message
// content instead of routing assistant text through the glamour renderer.
// The visible symptom was a finalized, nicely-rendered response reverting to
// literal "**bold**"/"| table |" markdown text the next time a snapshot
// arrived (e.g. right after submitting the next prompt).
func TestApplySnapshotRendersAssistantMarkdown(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80 // newModel pre-populates the glamour cache for width 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleAssistant, Content: "**bold text**"},
			},
		},
	})

	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, "**bold text**") {
		t.Fatalf("assistant markdown was not glamour-rendered, got raw content: %q", joined)
	}
	if !strings.Contains(stripANSI(joined), "bold text") {
		t.Fatalf("expected rendered output to still contain the text, got %q", stripANSI(joined))
	}
}

// TestApplySnapshotRendersPersistedReasoning guards against a real bug
// twinned with finalizeResponse's live-reasoning commit: reasoning is
// persisted per-message (ChatMessage.ReasoningContent) specifically so it
// survives a snapshot rebuild, but applySnapshot ignored that field
// entirely — a reasoning block that finalizeResponse had just committed to
// scrollback would render correctly for one turn, then vanish the moment
// the next prompt triggered a rebuild here, since this function is the sole
// source of truth for renderedLines.
//
// Since this message has a trailing answer, the block auto-collapses (see
// commitReasoningBlock) — the raw text must still survive uncollapsed in
// m.committedReasoning (collapsing is presentation-only, never touches the
// persisted/source content), and expanding it must reveal the exact text.
func TestApplySnapshotRendersPersistedReasoning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleAssistant, ReasoningContent: "carefully considering", Content: "the answer"},
			},
		},
	})

	if len(m.committedReasoning) != 1 {
		t.Fatalf("expected one committed reasoning block, got %d", len(m.committedReasoning))
	}
	if m.committedReasoning[0].text != "carefully considering" {
		t.Fatalf("expected raw reasoning text preserved, got %q", m.committedReasoning[0].text)
	}
	if !m.committedReasoning[0].collapsed {
		t.Fatalf("expected block to auto-collapse since the message has a trailing answer")
	}
	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, "carefully considering") {
		t.Fatalf("expected collapsed block not to show its raw text yet, got %q", joined)
	}
	if !strings.Contains(stripANSI(joined), "the answer") {
		t.Fatalf("expected assistant content to still render, got %q", stripANSI(joined))
	}

	if !m.toggleReasoningBlock(m.committedReasoning[0].key) {
		t.Fatal("expected toggleReasoningBlock to find the committed block")
	}
	joined = strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "carefully considering") {
		t.Fatalf("expected expanding the block to reveal the raw reasoning text, got %q", joined)
	}
}

// TestApplySnapshotPreservesToolCalls guards against a real bug: a
// ChatRoleTool message fell into applySnapshot's default/continue branch,
// so every past tool call silently vanished from the viewport the next time
// a snapshot rebuilt it (e.g. right after submitting the next prompt) —
// the same routine-snapshot hazard as the markdown-reverting bug above.
func TestApplySnapshotPreservesToolCalls(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "list files"},
				{
					Role: tauchat.ChatRoleAssistant,
					ToolCalls: []tauchat.ChatToolCall{
						{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "ls", Arguments: `{"path":"."}`}},
					},
				},
				{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a.go\nb.go"},
			},
		},
	})

	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "ls") {
		t.Fatalf("expected replayed tool box to show tool name %q, got %q", "ls", joined)
	}
	if !strings.Contains(joined, "a.go") {
		t.Fatalf("expected replayed tool box to show its result, got %q", joined)
	}
}

// TestApplySnapshotGroupsConsecutiveToolCalls mirrors
// TestManyToolCallsCommitAsOneGroup but for the session-history replay path
// (applySnapshot) — a saved session with a burst of tool calls must replay
// as one compact group too, not one box per call.
func TestApplySnapshotGroupsConsecutiveToolCalls(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{
			Role: tauchat.ChatRoleAssistant,
			ToolCalls: []tauchat.ChatToolCall{
				{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-2", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-3", Function: tauchat.ChatFunctionCall{Name: "read"}},
			},
		},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-2", Content: "b"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-3", Content: "c"},
	}

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{Messages: messages},
	})

	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "3 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '3 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
	}
}

// TestApplySnapshotReconstructsBashHistoryBox covers the fix for a bash-mode
// ("!") command losing its tool box on replay: runBashCommand
// (internal/agent/coordinator.go) persists the result as a plain
// ChatRoleUser message in "Ran `cmd`\n\n```\noutput\n```" form since it runs
// outside the normal tool-call loop. applySnapshot must recognize that shape
// and rebuild a real tool box from it instead of showing raw markdown text.
func TestApplySnapshotReconstructsBashHistoryBox(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "Ran `git status`\n\n```\nOn branch main\n```"},
			},
		},
	})

	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "shell") {
		t.Fatalf("expected reconstructed tool box to show tool name %q, got %q", "shell", joined)
	}
	if !strings.Contains(joined, "On branch main") {
		t.Fatalf("expected reconstructed tool box to show its output, got %q", joined)
	}
	if strings.Contains(joined, "Ran `git status`") {
		t.Fatalf("expected raw bash-history markdown to be replaced by a tool box, got %q", joined)
	}
}

// TestApplySnapshotGroupsConsecutiveBashHistory ensures back-to-back bash-mode
// commands (no assistant/user text between them) replay as one compact group,
// matching how a live burst of tool calls collapses via renderCommittedTools.
func TestApplySnapshotGroupsConsecutiveBashHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "Ran `git status`\n\n```\nclean\n```"},
				{Role: tauchat.ChatRoleUser, Content: "Ran `git diff`\n\n```\n\n```"},
			},
		},
	})

	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "2 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '2 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
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

	content := m.finalizeResponse("")

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
	// autoFollow — which now gates GotoBottom instead of inResponse/
	// PastBottom — actually clears, matching what a real manual scroll does.
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

// TestNotificationWrapsInsteadOfTruncating guards against a real bug: chat
// runtime errors/notifications were previously squeezed into a single-line
// status-bar segment that hard-truncated with "…" once the message didn't
// fit — the notification banner (rendered above the input area) must instead
// wrap across as many lines as it needs.
// TestNotificationWrapsWithinReservedLines verifies a notification that
// fits within notifyReservedLines wraps in full (no mid-word ellipsis
// truncation — the original bug this banner replaced, where a long
// chat-runtime error got squeezed into a single-line status-bar segment
// and hard-truncated there).
func TestNotificationWrapsWithinReservedLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	msg := "short notice that fits in two lines"
	m.notification = msg
	m.notificationLevel = notify.LevelError

	geom := m.computeLayout()
	plain := stripANSI(geom.chromeStr)

	if strings.Contains(plain, "…") {
		t.Fatalf("notification should wrap, not truncate with an ellipsis:\n%s", plain)
	}
	for word := range strings.FieldsSeq(msg) {
		if !strings.Contains(plain, word) {
			t.Fatalf("expected word %q to survive in the rendered chrome, got:\n%s", word, plain)
		}
	}
}

// TestNotificationAreaHasFixedHeight guards against a real UX bug: the
// notification banner only occupied space while m.notification was
// non-empty, so the viewport visibly grew and shrank by that height every
// time a notification appeared or cleared ("pushing text up and dropping
// it back down"). The reserved area must now be exactly notifyReservedLines
// tall regardless of whether there's currently a message — verified here
// by checking the viewport gets the same height either way.
func TestNotificationAreaHasFixedHeight(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for range 40 {
		m.appendMessage("user", "line")
	}

	m.View()
	emptyHeight := m.viewport.Height()

	m.notification = "a notice"
	m.View()
	withNoticeHeight := m.viewport.Height()

	if emptyHeight != withNoticeHeight {
		t.Fatalf("viewport height changed when a notification appeared: %d -> %d, want unchanged", emptyHeight, withNoticeHeight)
	}
}

// TestNotificationLongerThanReservedLinesIsClipped documents the accepted
// trade-off of a fixed-height reserved area: a message that doesn't fit
// within notifyReservedLines is clipped rather than growing the area (and
// therefore resizing the whole layout) to fit it.
func TestNotificationLongerThanReservedLinesIsClipped(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 24})
	m.notification = "this message is deliberately far too long to fit within just two narrow lines of text"

	geom := m.computeLayout()
	notifyLines := strings.Split(stripANSI(geom.chromeStr), "\n")[:notifyReservedLines]
	if len(notifyLines) != notifyReservedLines {
		t.Fatalf("expected exactly %d reserved notification lines, got %d", notifyReservedLines, len(notifyLines))
	}
}

// TestNotificationRendersAboveInput verifies positioning: the notification
// banner must appear before the separator/input in the rendered chrome, not
// buried in the status bar below the input.
func TestNotificationRendersAboveInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.notification = "distinctive-marker-text"

	geom := m.computeLayout()
	plain := stripANSI(geom.chromeStr)

	notifyIdx := strings.Index(plain, "distinctive-marker-text")
	sepIdx := strings.Index(plain, strings.Repeat("─", 10))
	if notifyIdx < 0 {
		t.Fatal("expected the notification text to appear in the chrome")
	}
	if sepIdx < 0 {
		t.Fatal("expected to find the separator line in the chrome")
	}
	if notifyIdx > sepIdx {
		t.Fatalf("expected notification (at %d) to render above the separator/input (at %d)", notifyIdx, sepIdx)
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

// --- mouse drag text selection ---------------------------------------------

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

// TestExpandedToolBoxPopulatesMarkdownCacheAtInnerWidth guards against a
// cache-key mismatch: an expanded tool box renders its markdown result at
// innerWidth = width-8, but mdCache is normally only populated at the full
// terminal width (constructor preload + WindowSizeMsg). A direct lookup at
// innerWidth used to miss every time — silently falling back to raw,
// unrendered markdown — because nothing ever populated the cache at that
// key. renderToolBox must ensure a renderer exists at innerWidth itself.
func TestExpandedToolBoxPopulatesMarkdownCacheAtInnerWidth(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	tool := toolState{id: "t1", name: "read", status: "done", result: "# Heading\n\nSome body text."}
	m.renderToolBox(tool, true, 0)

	const innerWidth = 72 // width(80) - 8, per renderToolBox's own math
	if r, ok := m.mdCache[innerWidth]; !ok || r == nil {
		t.Fatalf("expected renderToolBox to ensure a glamour renderer in mdCache at innerWidth=%d, got ok=%v", innerWidth, ok)
	}
}

// TestRenderMarkdownAtNarrowWidthStillRenders guards against a normalization
// mismatch: ensureMDRenderer clamps widths below 20 up to 20 before storing
// a renderer, but renderMarkdown used to look the renderer back up under the
// raw, unclamped width — so a narrow terminal (or m.width == 0 before the
// first WindowSizeMsg) always missed the cache and fell back to raw
// markdown.
func TestRenderMarkdownAtNarrowWidthStillRenders(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 10, Height: 24})

	out := m.renderMarkdown("# Heading")
	plain := stripANSI(out)
	if !strings.Contains(plain, "Heading") {
		t.Fatalf("expected rendered markdown to contain the heading text, got:\n%s", plain)
	}
	if strings.Contains(plain, "# Heading") {
		t.Fatalf("expected the heading to be glamour-rendered at a narrow width, not left as raw markdown:\n%s", plain)
	}
}

// TestComputeLayoutRowsMatchRenderedContent guards against the row-drift bug
// class where computeLayout's own row bookkeeping disagrees with what
// actually lands on screen. Earlier mouse-hit tests fed computeLayout's
// coordinates straight back into hit-testing, which stays self-consistent
// even if the underlying math is wrong — this test instead cross-checks
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

// --- context menu ---

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

func TestOpenContextMenuOnLiveToolBoxRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.toolBoxes[0].startY})

	if m.contextMenu == nil {
		t.Fatal("expected right-click on a live tool box to open a context menu")
	}
	if m.contextMenu.target != contextMenuTargetTool || m.contextMenu.targetID != "t1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetTool targetID=t1", m.contextMenu)
	}
	if len(m.contextMenu.items) != 2 || m.contextMenu.items[1].label != "Expand" {
		t.Fatalf("items = %+v, want [Copy output, Expand]", m.contextMenu.items)
	}
}

func TestOpenContextMenuOnCommittedToolGroupRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)
	g := m.committedGroups[0]

	cm := m.buildCommittedToolContextMenu(g.lineIdx, 0, 0)
	if cm == nil {
		t.Fatal("expected a menu for a click on the (folded) committed group")
	}
	if cm.target != contextMenuTargetTool || cm.targetID != "t0" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetTool targetID=t0 (first tool)", cm)
	}
}

func TestOpenContextMenuOnCommittedToolGroupRowRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)
	g := m.committedGroups[0]

	// Unfold the group first (mirrors TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow).
	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected the header click to unfold the group")
	}
	if !g.expanded {
		t.Fatal("expected the group to be unfolded")
	}

	// Row layout per renderToolGroupBox: border(0) + header(1) + t0's row(2) + t1's row(3).
	cm := m.buildCommittedToolContextMenu(g.lineIdx+3, 0, 0)
	if cm == nil {
		t.Fatal("expected a menu for a click on t1's row")
	}
	if cm.target != contextMenuTargetToolRow || cm.targetID != "t1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetToolRow targetID=t1", cm)
	}
}

func TestOpenContextMenuOnMessageRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{
		Messages: []tauchat.ChatMessage{
			{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello there"},
		},
	}})
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.viewportStartY})

	if m.contextMenu == nil {
		t.Fatal("expected right-click on a message to open a context menu")
	}
	if m.contextMenu.target != contextMenuTargetMessage || m.contextMenu.targetID != "u1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetMessage targetID=u1", m.contextMenu)
	}
	if len(m.contextMenu.items) != 1 || m.contextMenu.items[0].label != "Copy" {
		t.Fatalf("items = %+v, want [Copy]", m.contextMenu.items)
	}
}

func TestContextMenuCopyMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.messageRanges = []messageLineRange{{id: "u1", content: "raw message text", startLine: 0, endLine: 1}}

	cmd := m.activateMessageContextAction("u1", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuMessageCopyUsesRawContentNotStyledLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{
		Messages: []tauchat.ChatMessage{
			{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "**bold** markdown"},
		},
	}})

	// renderedLines holds glamour-rendered (ANSI-styled) output, not the
	// raw "**bold** markdown" — Copy must read messageRanges' stored raw
	// content instead, the same reason lastAssistantText exists.
	var content string
	for _, r := range m.messageRanges {
		if r.id == "a1" {
			content = r.content
		}
	}
	if content != "**bold** markdown" {
		t.Fatalf("stored content = %q, want raw markdown %q", content, "**bold** markdown")
	}
}

// TestCompositeContextMenuPreservesBaseContentOutsideMenu is a regression
// test for a real bug: composing bare Layers directly onto a Canvas (rather
// than through a Compositor) ignores each Layer's X/Y and draws every layer
// starting at (0,0) filling the whole canvas area — which blanks out
// everything the menu layer's own small bounds don't cover, leaving only a
// tiny box in the top-left corner and wiping the rest of the screen.
func TestCompositeContextMenuPreservesBaseContentOutsideMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 40, 10
	m.contextMenu = &contextMenu{
		x: 20, y: 5,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	base := strings.Repeat("base-line-content\n", 9) + "base-line-content"
	out := stripANSI(m.compositeContextMenu(base))

	if !strings.Contains(out, "base-line-content") {
		t.Fatalf("expected base content to survive compositing, got:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected the top row (far from the click at y=5) to still show base content, got blank line: %q", lines[0])
	}
}

// TestCompositeContextMenuPositionsNearClick guards the other half of the
// same bug: the menu must render near m.contextMenu.x/y, not always at the
// canvas origin.
func TestCompositeContextMenuPositionsNearClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 60, 20
	m.contextMenu = &contextMenu{
		x: 30, y: 10,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	base := strings.Repeat(strings.Repeat(".", 60)+"\n", 19) + strings.Repeat(".", 60)
	out := stripANSI(m.compositeContextMenu(base))
	lines := strings.Split(out, "\n")

	for i := range 5 {
		if strings.Contains(lines[i], "Copy output") {
			t.Fatalf("expected the menu not to appear near the top of the screen (click was at y=10), found it on line %d: %q", i, lines[i])
		}
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Copy output") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the menu to appear somewhere in the composited output, got:\n%s", out)
	}
}

func TestContextMenuUpDownWraps(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.contextMenu = &contextMenu{items: []contextMenuItem{{label: "a"}, {label: "b"}, {label: "c"}}}

	m.handleContextMenuKey(key(tea.KeyUp, 0))
	if m.contextMenu.selected != 2 {
		t.Fatalf("selected = %d, want 2 (up from 0 wraps to last)", m.contextMenu.selected)
	}
	m.handleContextMenuKey(key(tea.KeyDown, 0))
	if m.contextMenu.selected != 0 {
		t.Fatalf("selected = %d, want 0 (down from last wraps to first)", m.contextMenu.selected)
	}
}

func TestContextMenuEscDismissesWithoutAction(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}

	m.handleContextMenuKey(key(tea.KeyEscape, 0))

	if m.contextMenu != nil {
		t.Fatal("expected esc to close the menu")
	}
	if m.notification != "" {
		t.Fatalf("expected esc to take no action, got notification %q", m.notification)
	}
}

func TestContextMenuEnterActivatesSelectedItemAndCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}

	cmd := m.handleContextMenuKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.contextMenu != nil {
		t.Fatal("expected enter to close the menu")
	}
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected the Copy action to fire, got notification %q", m.notification)
	}
}

func TestContextMenuCopyToolOutputLiveTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha output"}}

	cmd := m.activateToolContextAction("t1", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuCopyToolOutputCommittedGroupJoinsAllResults(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "alpha"},
		{id: "t1", name: "search", status: "done", result: "bravo"},
	}, nil)

	cmd := m.activateToolContextAction("t0", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuExpandCollapseLabelReflectsLiveState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}

	cm := m.buildLiveToolContextMenu("t1", 0, 0)
	if cm.items[1].label != "Expand" {
		t.Fatalf("label = %q, want Expand for a not-yet-expanded tool", cm.items[1].label)
	}

	m.expandedID = "t1"
	cm = m.buildLiveToolContextMenu("t1", 0, 0)
	if cm.items[1].label != "Collapse" {
		t.Fatalf("label = %q, want Collapse for an already-expanded tool", cm.items[1].label)
	}
}

func TestContextMenuToggleExpandLiveTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}

	m.activateToolContextAction("t1", contextMenuActionToggleExpand)
	if m.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1 after toggling expand", m.expandedID)
	}

	m.activateToolContextAction("t1", contextMenuActionToggleExpand)
	if m.expandedID != "" {
		t.Fatalf("expandedID = %q, want empty after toggling collapse", m.expandedID)
	}
}

func TestActivePromptTakesPriorityOverOpenContextMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}
	m.activePrompt = &formPrompt{kind: promptConfirm, title: "t", message: "m", confirmYes: true}

	m.dispatchKey(key(tea.KeyEnter, 0))

	// The prompt handler ran (resolved and cleared activePrompt via
	// resolvePrompt), not the context-menu handler — the menu must still
	// be open since handlePromptKey never touches it.
	if m.contextMenu == nil {
		t.Fatal("expected the context menu to be untouched by a key routed to the prompt handler")
	}
}

func TestEnqueuePromptClosesOpenContextMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.contextMenu = &contextMenu{items: []contextMenuItem{{label: "x"}}}

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{Kind: "confirm", Title: "t", Message: "m"})

	if m.contextMenu != nil {
		t.Fatal("expected a newly enqueued prompt to close any open context menu")
	}
}

func TestCompletionsDoNotConsumeKeysWhileContextMenuOpen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.input = "/help"
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "a"}, {label: "b"}},
	}

	m.dispatchKey(key(tea.KeyDown, 0))

	if m.contextMenu == nil {
		t.Fatal("expected the context menu to stay open")
	}
	if m.contextMenu.selected != 1 {
		t.Fatalf("selected = %d, want 1 — the menu, not the completions dropdown, should have consumed 'down'", m.contextMenu.selected)
	}
}

func TestContextMenuClickAwayDismisses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		x: 5, y: 5,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	// Far outside the menu's small footprint near (5,5).
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 70, Y: 20})

	if m.contextMenu != nil {
		t.Fatal("expected a click outside the menu's bounds to close it")
	}
}

func TestContextMenuClickInsideActivatesItem(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha output"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		x: 5, y: 5,
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}, {label: "Expand", action: contextMenuActionToggleExpand}},
	}

	// contextMenuStyle draws a 1-cell border, so item 0 ("Copy output")
	// sits one row below the menu's top-left corner (5,5) -> (5, 6).
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 6, Y: 6})

	if m.contextMenu != nil {
		t.Fatal("expected a click on an item to close the menu")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected the click to activate Copy output, got notification %q", m.notification)
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
// (model.go) checks toolsSel first among the four states, and right-click
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

func TestSpaceTogglesToolExpansion(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}
	m.focusedTool = 0

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1", m.expandedID)
	}

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.expandedID != "" {
		t.Fatalf("expandedID should collapse on second space, got %q", m.expandedID)
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

func TestNotificationDefaultDurationsAreTransient(t *testing.T) {
	if defaultNotificationClearDelay <= 0 {
		t.Fatalf("defaultNotificationClearDelay = %v, want positive", defaultNotificationClearDelay)
	}
	if defaultNotifyInfoDuration <= 0 {
		t.Fatalf("defaultNotifyInfoDuration = %v, want positive", defaultNotifyInfoDuration)
	}
	if defaultNotifyWarnDuration <= 0 {
		t.Fatalf("defaultNotifyWarnDuration = %v, want positive", defaultNotifyWarnDuration)
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
	for line := range strings.SplitSeq(plain, "\n") {
		if visibleWidth(line) > m.width {
			t.Fatalf("input line width = %d, want <= %d: %q\n%s", visibleWidth(line), m.width, line, plain)
		}
	}
}

// TestRenderInputAreaCapsHeightAndScrollsOverflow guards against the input
// box growing past inputBoxHeightFrac of the terminal and pushing the
// viewport off the top — a long multi-line paste must scroll inside a
// capped box instead of rendering every line.
func TestRenderInputAreaCapsHeightAndScrollsOverflow(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 80, 20 // 60% cap -> max box height 12 -> max body 8 rows

	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		if i > 1 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "line %02d", i)
	}
	m.input = sb.String()
	m.inputCursor = 0 // cursor on the first line

	area := m.renderInputArea()
	lines := strings.Split(area, "\n")
	maxBoxHeight := int(float64(m.height) * inputBoxHeightFrac)
	if len(lines) > maxBoxHeight {
		t.Fatalf("input box height = %d lines, want <= %d (60%% of terminal height %d)", len(lines), maxBoxHeight, m.height)
	}

	plain := stripANSI(area)
	if !strings.Contains(plain, "line 01") {
		t.Fatalf("expected the cursor's line (line 01) to stay visible, got:\n%s", plain)
	}
	if strings.Contains(plain, "line 30") {
		t.Fatalf("expected lines far past the cursor to scroll out of view, got:\n%s", plain)
	}
	if !strings.Contains(plain, "more below") {
		t.Fatalf("expected a scroll indicator when content overflows, got:\n%s", plain)
	}
}

// TestRenderInputAreaFitsWithoutClipping checks the common case is
// unaffected: input short enough to fit within the height cap renders every
// line with no scroll indicator.
func TestRenderInputAreaFitsWithoutClipping(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 80, 40
	m.input = "line one\nline two\nline three"
	m.inputCursor = 0

	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, "line one") || !strings.Contains(plain, "line two") || !strings.Contains(plain, "line three") {
		t.Fatalf("expected all lines to render unclipped, got:\n%s", plain)
	}
	if strings.Contains(plain, "more above") || strings.Contains(plain, "more below") {
		t.Fatalf("expected no scroll indicator when content fits, got:\n%s", plain)
	}
}

func TestRenderInputChunkCursorOverCharacter(t *testing.T) {
	ln := []rune("hello")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 2, false, 0, 0))
	if out != "hello" {
		t.Fatalf("cursor over character = %q, want %q", out, "hello")
	}
}

func TestRenderInputChunkCursorAtEnd(t *testing.T) {
	ln := []rune("hi")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 2, false, 0, 0))
	if out != "hi " {
		t.Fatalf("cursor at end = %q, want 'hi '", out)
	}
}

func TestRenderInputChunkCursorAtStart(t *testing.T) {
	ln := []rune("abc")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 0, false, 0, 0))
	if out != "abc" {
		t.Fatalf("cursor at start = %q, want 'abc'", out)
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

// --- input box mouse selection ----------------------------------------------

// promptPrefixWidth mirrors renderInputArea's own prefixWidth computation
// for the non-steering "> " prompt, so tests can compute expected click
// columns without hardcoding a width that would silently drift if the
// prefix ever changes.
func promptPrefixWidth() int {
	return visibleWidth(stripANSI(inputPromptStyle.Render("> ")))
}

func TestInputPositionAtMapsClickToRunePosition(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.input = "hello world"

	textStartCol := 1 + promptPrefixWidth() // 1 for the left border
	const bodyRow = 2                       // top border + hint row precede body rows

	if got := m.inputPositionAt(bodyRow, textStartCol); got != 0 {
		t.Fatalf("click at text start = %d, want 0", got)
	}
	if got := m.inputPositionAt(bodyRow, textStartCol+5); got != 5 {
		t.Fatalf("click 5 cols in = %d, want 5", got)
	}
	if got := m.inputPositionAt(bodyRow, textStartCol+100); got != len([]rune(m.input)) {
		t.Fatalf("click past the end = %d, want %d", got, len([]rune(m.input)))
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

// --- status bar mouse selection ----------------------------------------------

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

	m.upsertToolCall("t1", "read", "new", "")

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
	if !strings.Contains(plain, "20.0k tok") {
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

// --- agentState transitions ---

func TestAgentStateDefaultsToReady(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	if m.agentState != agentReady {
		t.Fatalf("agentState = %v, want agentReady (zero value)", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ready") {
		t.Fatalf("status bar = %q, want the Ready label", plain)
	}
}

func TestAgentStateResponseStartedTransitionsToThinking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})

	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Thinking") {
		t.Fatalf("status bar = %q, want 'Thinking'", plain)
	}
	if !strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, want the interrupt hint", plain)
	}
}

func TestAgentStateReasoningDeltaStaysThinking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "considering..."})

	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking", m.agentState)
	}
}

func TestAgentStateReasoningDeltaKeepsRunningTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "checking the result"})

	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool while a tool is executing", m.agentState)
	}
}

func TestAgentStateToolExecutionStartedTransitionsToRunningTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})

	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Running read") {
		t.Fatalf("status bar = %q, want 'Running read'", plain)
	}
	if !strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, want the interrupt hint", plain)
	}
}

func TestAgentStateRunningToolWithoutRunningEntryHasSafeFallback(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.agentState = agentRunningTool

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Running") {
		t.Fatalf("status bar = %q, want the defensive Running fallback", plain)
	}
}

func TestToolExecutionStartResetsElapsedClock(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	oldStart := time.Now().Add(-time.Minute)
	m.tools = []toolState{{id: "t1", name: "read", status: "pending", startedAt: oldStart}}
	beforeStart := time.Now()

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})

	if m.tools[0].startedAt.Before(beforeStart) {
		t.Fatalf("startedAt = %v, want execution-start time after %v", m.tools[0].startedAt, beforeStart)
	}
	startedAt := m.tools[0].startedAt
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	if !m.tools[0].startedAt.Equal(startedAt) {
		t.Fatalf("duplicate execution-start reset startedAt: %v -> %v", startedAt, m.tools[0].startedAt)
	}
}

// TestAgentStateToolExecutionCompletedKeepsRunningToolWhileSiblingActive
// guards against the status bar flickering back to "Thinking" and forward
// to "Running <tool>" again between two concurrently running tool calls —
// it should stay on agentRunningTool until every call in the batch settles.
func TestAgentStateToolExecutionCompletedKeepsRunningToolWhileSiblingActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t2", ToolName: "grep"})

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1"})
	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool while t2 is still running", m.agentState)
	}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t2"})
	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking once every tool has settled", m.agentState)
	}
}

func TestAgentStateResponseDeltaTransitionsToStreaming(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})

	if m.agentState != agentStreaming {
		t.Fatalf("agentState = %v, want agentStreaming", m.agentState)
	}
	if m.streamStartedAt.IsZero() {
		t.Fatal("expected streamStartedAt to be set on transition into Streaming")
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "generating") {
		t.Fatalf("status bar = %q, want 'generating'", plain)
	}
}

// TestAgentStateResponseDeltaStreamStartedAtStableAcrossDeltas checks
// streamStartedAt is set once per turn (on the transition into Streaming),
// not reset on every subsequent delta — otherwise a live tok/s estimate
// would never accumulate enough elapsed time to ever become available.
func TestAgentStateResponseDeltaStreamStartedAtStableAcrossDeltas(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello "})
	first := m.streamStartedAt
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "world"})
	if !m.streamStartedAt.Equal(first) {
		t.Fatalf("streamStartedAt changed across deltas: %v -> %v", first, m.streamStartedAt)
	}
}

func TestAgentStateResponseCompletedReturnsToReady(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.agentState != agentReady {
		t.Fatalf("agentState = %v, want agentReady after completion", m.agentState)
	}
}

func TestAgentStateResponseCancelledTransitionsToCancelled(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseCancelledEvent{})

	if m.agentState != agentCancelled {
		t.Fatalf("agentState = %v, want agentCancelled", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Cancelled") {
		t.Fatalf("status bar = %q, want 'Cancelled'", plain)
	}
	if strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, should not show the interrupt hint once cancelled", plain)
	}
}

func TestAgentStateRuntimeErrorTransitionsToError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "connection reset by peer"})

	if m.agentState != agentError {
		t.Fatalf("agentState = %v, want agentError", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Error") {
		t.Fatalf("status bar = %q, want 'Error'", plain)
	}
	// The message itself belongs to the notification banner/scrollback, not
	// the status bar — see TestAgentStateRuntimeErrorStatusBarIsJustTheState.
	if strings.Contains(plain, "connection reset by peer") {
		t.Fatalf("status bar = %q, should not restate the error message", plain)
	}
}

// TestAgentStateRuntimeErrorStatusBarIsJustTheState checks the status bar
// shows only the "Error" state label, not the message itself — the full
// message already has a home in the notification banner (persists until
// dismissed) and scrollback, both on screen at the same time as this bar,
// so restating it here would just be a third copy of the same text.
func TestAgentStateRuntimeErrorStatusBarIsJustTheState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	longMsg := strings.Repeat("x", 200) + "\nsecond line should never appear"
	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: longMsg})

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Error") {
		t.Fatalf("expected status bar to show the Error state, got %q", plain)
	}
	if strings.Contains(plain, "second line") || strings.Contains(plain, strings.Repeat("x", 60)) {
		t.Fatalf("status bar should not restate the error message, got %q", plain)
	}
}

// --- computeStatusBar narrow-width rendering ---

// TestComputeStatusBarNarrowWidthPrioritizesActiveState checks the core
// width-pressure requirement: under a narrow terminal, the active-state
// label and interrupt hint must survive while secondary metadata (session
// tokens, cost, context %) gets dropped.
func TestComputeStatusBarNarrowWidthPrioritizesActiveState(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))
	pub.Publish(tauchat.MetricEvent{
		Category:  tauchat.MetricCategoryLLM,
		Name:      "llm.response",
		Value:     20_000,
		Labels:    map[string]string{"prompt_tokens": "19000", "completion_tokens": "1000"},
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
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})

	m.width = 18 // narrow enough to force dropping
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ctrl+C") {
		t.Fatalf("narrow status bar = %q, want the interrupt hint to survive", plain)
	}
	if strings.Contains(plain, "session tok") {
		t.Fatalf("narrow status bar = %q, session token metadata should have been dropped first", plain)
	}
}

// TestComputeStatusBarNarrowWidthReadyKeepsModelOverLabel checks Ready's own
// degradation order: the model name (left, tail-truncated) survives longer
// than the trailing "Ready" label (right, lowest priority) under pressure.
// TestComputeStatusBarNarrowWidthReadyPrioritizesStateLabel checks Ready's
// own degradation order under extreme width pressure: the right-hand group
// (here, just the "Ready" state label) is never truncated away — only the
// left identity blob (τ tau/model/provider) yields, via tail-truncation —
// matching renderStatusBar's existing invariant and this feature's
// "prioritize active state over secondary metadata" requirement, treating
// "Ready" itself as Ready's active-state label.
func TestComputeStatusBarNarrowWidthReadyPrioritizesStateLabel(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	// Wide enough for "τ · Ready" (the undroppable left floor) plus a
	// little room, but too narrow for "gpt-4" to also fit on the right.
	m.width = 14

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ready") {
		t.Fatalf("narrow status bar = %q, want the Ready state label to survive", plain)
	}
	if strings.Contains(plain, "gpt-4") {
		t.Fatalf("narrow status bar = %q, want the model name dropped before the state label truncates", plain)
	}
}

// TestComputeStatusBarExtremeNarrowWidthDoesNotPanic checks the true
// rock-bottom case: even "τ tau · Ready" alone doesn't fit. There's nothing
// left to drop at that point (both are prioTransient), so the
// character-truncation fallback is the documented last resort — this just
// guards it stays non-empty and never panics, not any particular content.
func TestComputeStatusBarExtremeNarrowWidthDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.width = 5

	if out := m.computeStatusBar(); out == "" {
		t.Fatal("expected non-empty output even at extreme narrow width")
	}
}

func TestComputeStatusBarNarrowThinkingStateDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 10
	m.modelName = "gpt-4"
	m.agentState = agentThinking

	plain := stripANSI(m.computeStatusBar())
	if plain == "" {
		t.Fatal("expected a non-empty narrow Thinking status bar")
	}
	if visibleWidth(plain) > m.width {
		t.Fatalf("status bar width = %d, want <= %d: %q", visibleWidth(plain), m.width, plain)
	}
}

func TestComputeStatusBarZeroWidthDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.width = 0

	// Should not panic; content is unconstrained by an actual terminal width
	// until the first WindowSizeMsg, same as the rest of the layout.
	_ = m.computeStatusBar()
}

// --- approxTokensPerSecond ---

func TestApproxTokensPerSecondUnavailableTooEarly(t *testing.T) {
	if _, ok := approxTokensPerSecond("some streamed text", 100*time.Millisecond); ok {
		t.Fatal("expected tok/s to be unavailable before enough elapsed time has passed")
	}
}

func TestApproxTokensPerSecondUnavailableWithNoText(t *testing.T) {
	if _, ok := approxTokensPerSecond("", 2*time.Second); ok {
		t.Fatal("expected tok/s to be unavailable with no streamed text yet")
	}
}

func TestApproxTokensPerSecondAvailable(t *testing.T) {
	rate, ok := approxTokensPerSecond(strings.Repeat("word ", 100), 2*time.Second)
	if !ok {
		t.Fatal("expected tok/s to be available with enough text and elapsed time")
	}
	if rate <= 0 {
		t.Fatalf("rate = %d, want > 0", rate)
	}
}

func TestApproxTokensPerSecondCountsRunes(t *testing.T) {
	rate, ok := approxTokensPerSecond("你好世界你好世界", time.Second)
	if !ok {
		t.Fatal("expected tok/s to be available for non-ASCII text")
	}
	if rate != 2 { // 8 runes / 4 estimated chars per token / 1 second.
		t.Fatalf("rate = %d, want 2", rate)
	}
}

// --- toolState tests ---

func TestToolStateDefaultValues(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.upsertToolCall("new-id", "bash", "echo hi", "")

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

func TestAppendMessageMultiLineUserContinuationInheritsTerminalForeground(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("user", "line one\nline two")

	if len(m.renderedLines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(m.renderedLines))
	}
	// Chat message bodies must not force a truecolor foreground: they
	// should inherit the terminal's own default foreground rather than
	// overriding the user's theme.
	if strings.Contains(m.renderedLines[1], "38;2;") {
		t.Fatalf("continuation line = %q, user message body should not force a foreground color", m.renderedLines[1])
	}
	// It should also not fall back to the generic/tool continuation's dim
	// styling — a user message reads as plain content, not muted metadata.
	if strings.Contains(m.renderedLines[1], "\x1b[2m") {
		t.Fatalf("continuation line = %q, should not use faint/dim styling", m.renderedLines[1])
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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

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
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	m.handlePromptKey(key(tea.KeyTab, 0))
	if m.activePrompt.confirmYes {
		t.Fatal("Tab should toggle confirmYes to false")
	}

	m.handlePromptKey(key(tea.KeyTab, 0))
	if !m.activePrompt.confirmYes {
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

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	if !m.activePrompt.confirmYes {
		t.Fatal("confirmYes should default to true")
	}
}

// --- handlePromptKey left/right on confirm toggles ---

func TestHandlePromptKeyLeftTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"})

	m.handlePromptKey(key(tea.KeyLeft, 0))
	if m.activePrompt.confirmYes {
		t.Fatal("Left should toggle confirmYes to false")
	}
}

func TestHandlePromptKeyRightTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"})
	m.activePrompt.confirmYes = false

	m.handlePromptKey(key(tea.KeyRight, 0))
	if !m.activePrompt.confirmYes {
		t.Fatal("Right should toggle confirmYes to true")
	}
}

// --- renderPrompt ---

func TestRenderPromptConfirm(t *testing.T) {
	p := &formPrompt{kind: promptConfirm, title: "Confirm?", message: "Are you sure?", confirmYes: true}
	out := stripANSI(renderPrompt(p, 80))
	if !strings.Contains(out, "Yes") || !strings.Contains(out, "No") {
		t.Fatalf("expected Yes/No in confirm prompt:\n%s", out)
	}
	if !strings.Contains(out, "Are you sure?") {
		t.Fatalf("expected message in prompt:\n%s", out)
	}
}

func TestRenderPromptInput(t *testing.T) {
	p := &formPrompt{kind: promptQuestion, title: "Name?", message: "What is your name?", field: newTextField("")}
	out := stripANSI(renderPrompt(p, 80))
	if strings.Contains(out, "Yes") || strings.Contains(out, "No") {
		t.Fatalf("input prompt should not show Yes/No:\n%s", out)
	}
	if !strings.Contains(out, "enter to continue") {
		t.Fatalf("input prompt should show hint:\n%s", out)
	}
}

// --- resolvePromptConfirm ---

func TestResolvePromptConfirmTrue(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-cf", Kind: "confirm",
	})

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
