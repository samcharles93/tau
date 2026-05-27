package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
)

type fakeRuntime struct {
	commands []tauchat.ChatCommand
}

func (r *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	r.commands = append(r.commands, cmd)
	return nil
}

func (r *fakeRuntime) SubscribeEvents(buffer int) (*pubsub.Subscription[tauchat.ChatEvent], error) {
	return nil, nil
}

func (r *fakeRuntime) Close() {}

func TestEmptyEscapeExits(t *testing.T) {
	model := newTestModel(t, nil)

	_, cmd := model.Update(keyPress(tea.KeyEscape, 0))
	if !cmdReturnsQuit(cmd) {
		t.Fatalf("Escape with empty input returned %#v, want tea.Quit", cmd)
	}
}

func TestCtrlCIdleEmptyExits(t *testing.T) {
	model := newTestModel(t, nil)

	_, cmd := model.Update(keyPress('c', tea.ModCtrl))
	if !cmdReturnsQuit(cmd) {
		t.Fatalf("Ctrl+C with idle empty input returned %#v, want tea.Quit", cmd)
	}
}

func TestCtrlCStreamingCancels(t *testing.T) {
	runtime := &fakeRuntime{}
	model := newTestModel(t, runtime)
	model.status = tauchat.ChatSessionStreaming

	_, cmd := model.Update(keyPress('c', tea.ModCtrl))
	if cmd == nil {
		t.Fatal("Ctrl+C while streaming returned nil command, want cancel command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("Ctrl+C while streaming command message = %#v, want non-empty BatchMsg", msg)
	}
	batch[0]()

	if len(runtime.commands) != 1 {
		t.Fatalf("runtime commands = %d, want 1", len(runtime.commands))
	}
	if _, ok := runtime.commands[0].(tauchat.CancelChatRequestCommand); !ok {
		t.Fatalf("runtime command = %#v, want CancelChatRequestCommand", runtime.commands[0])
	}
	notification := model.notifications.Current()
	if notification == nil || notification.Message != "Cancelling… Ctrl+C to quit" {
		t.Fatalf("notification = %#v, want cancellation hint", notification)
	}
	if model.status != tauchat.ChatSessionCancelling {
		t.Fatalf("status = %q, want cancelling", model.status)
	}

	_, cmd = model.Update(keyPress('c', tea.ModCtrl))
	if !cmdReturnsQuit(cmd) {
		t.Fatalf("second Ctrl+C while cancelling returned %#v, want tea.Quit", cmd)
	}
}

func TestCtrlCWithOverlayDoesNotQuitOrCancel(t *testing.T) {
	runtime := &fakeRuntime{}
	model := newTestModel(t, runtime)
	model.status = tauchat.ChatSessionStreaming
	model.openCommandPalette()

	_, cmd := model.Update(keyPress('c', tea.ModCtrl))
	if cmd != nil {
		t.Fatalf("Ctrl+C with overlay returned command %#v, want nil", cmd)
	}
	if len(runtime.commands) != 0 {
		t.Fatalf("runtime commands = %d, want 0", len(runtime.commands))
	}
	if model.status != tauchat.ChatSessionStreaming {
		t.Fatalf("status = %q, want streaming", model.status)
	}
	if !model.overlay.HasDialogs() {
		t.Fatal("overlay closed unexpectedly")
	}
}

func TestCtrlPAndCtrlKMatchWithExtraModifiers(t *testing.T) {
	model := newTestModel(t, nil)
	_, cmd := model.Update(keyPress('p', tea.ModCtrl|tea.ModAlt))
	if cmd != nil {
		t.Fatalf("Ctrl+Alt+P command = %#v, want nil", cmd)
	}
	if !model.overlay.HasDialogs() {
		t.Fatal("Ctrl+Alt+P did not open command palette")
	}

	model = newTestModel(t, nil)
	_, cmd = model.Update(keyPress('k', tea.ModCtrl|tea.ModAlt))
	if cmd != nil {
		t.Fatalf("Ctrl+Alt+K command = %#v, want nil", cmd)
	}
	if !model.overlay.HasDialogs() {
		t.Fatal("Ctrl+Alt+K did not open model picker")
	}
}

func TestToolEventsRenderVisibleActivity(t *testing.T) {
	model := newTestModel(t, nil)
	model.ready = true
	model.width = 100
	model.height = 30
	model.recalculateLayout()

	model.handleRuntimeEvent(tauchat.ChatResponseStartedEvent{SessionID: "s1", RequestID: "r1"})
	model.handleRuntimeEvent(tauchat.ChatToolExecutionStartedEvent{
		SessionID:        "s1",
		RequestID:        "r1",
		CallID:           "call_1",
		ToolName:         "read",
		ArgumentsSummary: `{"path":"README.md"}`,
	})
	model.handleRuntimeEvent(tauchat.ChatToolExecutionCompletedEvent{
		SessionID:     "s1",
		RequestID:     "r1",
		CallID:        "call_1",
		ToolName:      "read",
		Status:        "success",
		ResultSummary: "file contents",
	})
	model.handleRuntimeEvent(tauchat.ChatResponseCompletedEvent{
		State:     tauchat.ChatSessionState{SessionID: "s1"},
		RequestID: "r1",
	})
	model.refreshViewport()

	got := model.viewport.View()
	if !strings.Contains(got, "Tools") || !strings.Contains(got, "read success") {
		t.Fatalf("viewport = %q, want tool activity section", got)
	}
}

func TestToolDeltaRendersSummarizedArguments(t *testing.T) {
	model := newTestModel(t, nil)
	model.ready = true
	model.width = 100
	model.height = 30
	model.recalculateLayout()
	rawLongArg := strings.Repeat("secret", 200)

	model.handleRuntimeEvent(tauchat.ChatResponseStartedEvent{SessionID: "s1", RequestID: "r1"})
	model.handleRuntimeEvent(tauchat.ChatToolCallDeltaEvent{
		SessionID:        "s1",
		RequestID:        "r1",
		CallID:           "call_1",
		ToolName:         "read",
		ArgumentsSummary: "secretsecret…",
		Truncated:        true,
	})
	model.refreshViewport()

	got := model.viewport.View()
	if !strings.Contains(got, "secretsecret…") || !strings.Contains(got, "[truncated]") {
		t.Fatalf("viewport = %q, want summarized truncated arguments", got)
	}
	if strings.Contains(got, rawLongArg) {
		t.Fatalf("viewport contains raw long argument")
	}
}

func TestReasoningStoredSeparatelyAndRenderedOnlyWhenEnabled(t *testing.T) {
	model := newTestModel(t, nil)
	model.ready = true
	model.width = 100
	model.height = 30
	model.recalculateLayout()

	model.handleRuntimeEvent(tauchat.ChatResponseStartedEvent{SessionID: "s1", RequestID: "r1"})
	model.handleRuntimeEvent(tauchat.ChatReasoningDeltaEvent{
		SessionID: "s1",
		RequestID: "r1",
		Delta:     "hidden thought",
		Snapshot:  "hidden thought",
	})
	model.handleRuntimeEvent(tauchat.ChatResponseDeltaEvent{
		SessionID: "s1",
		RequestID: "r1",
		Delta:     "visible answer",
		Snapshot:  "visible answer",
	})
	model.handleRuntimeEvent(tauchat.ChatResponseCompletedEvent{
		State:     tauchat.ChatSessionState{SessionID: "s1"},
		RequestID: "r1",
	})

	model.refreshViewport()
	if strings.Contains(model.viewport.View(), "hidden thought") {
		t.Fatalf("viewport = %q, reasoning rendered while disabled", model.viewport.View())
	}
	if model.turns[0].content != "visible answer" {
		t.Fatalf("assistant content = %q, want answer only", model.turns[0].content)
	}

	model.toggleReasoning()
	if got := model.viewport.View(); !strings.Contains(got, "Reasoning") || !strings.Contains(got, "hidden thought") {
		t.Fatalf("viewport = %q, want reasoning section after toggle", got)
	}
}

func TestCompletedEventReconcilesReasoningForLaterToggle(t *testing.T) {
	model := newTestModel(t, nil)
	model.ready = true
	model.width = 100
	model.height = 30
	model.recalculateLayout()

	model.handleRuntimeEvent(tauchat.ChatResponseStartedEvent{SessionID: "s1", RequestID: "r1"})
	model.handleRuntimeEvent(tauchat.ChatResponseDeltaEvent{
		SessionID: "s1",
		RequestID: "r1",
		Delta:     "visible answer",
		Snapshot:  "visible answer",
	})
	model.handleRuntimeEvent(tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{
			SessionID: "s1",
			Messages: []tauchat.ChatMessage{{
				Role:             tauchat.ChatRoleAssistant,
				Content:          "visible answer",
				ReasoningContent: "completed reasoning",
			}},
		},
		RequestID: "r1",
	})
	model.refreshViewport()
	if strings.Contains(model.viewport.View(), "completed reasoning") {
		t.Fatalf("viewport = %q, reasoning rendered while disabled", model.viewport.View())
	}

	model.toggleReasoning()
	if got := model.viewport.View(); !strings.Contains(got, "completed reasoning") {
		t.Fatalf("viewport = %q, want completed reasoning after toggle", got)
	}
}

func TestMouseWheelScrollUpPreventsAutoScrollOnIncomingEvent(t *testing.T) {
	model := newTestModel(t, nil)
	model.ready = true
	model.width = 80
	model.height = 12
	model.recalculateLayout()
	model.turns = append(model.turns, turnBlock{
		role:     tauchat.ChatRoleAssistant,
		content:  strings.Repeat("line\n", 80),
		finished: true,
	})
	model.refreshViewport()
	if !model.viewport.AtBottom() {
		t.Fatal("test setup did not start at bottom")
	}

	_, _ = model.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if !model.userScrolled {
		t.Fatal("mouse wheel up did not mark userScrolled")
	}
	if model.viewport.AtBottom() {
		t.Fatal("mouse wheel up did not move viewport away from bottom")
	}

	model.handleRuntimeEvent(tauchat.ChatResponseDeltaEvent{
		SessionID: "s1",
		RequestID: "r1",
		Delta:     "new",
		Snapshot:  "new",
	})
	model.refreshViewport()
	if model.viewport.AtBottom() {
		t.Fatal("incoming event forced viewport back to bottom after mouse wheel scroll")
	}
}

func newTestModel(t *testing.T, runtime *fakeRuntime) *Model {
	t.Helper()
	if runtime == nil {
		runtime = &fakeRuntime{}
	}
	return New(context.Background(), runtime, nil, Config{
		SessionID: "s1",
		ModelName: "test-model",
		Provider:  "test",
		AvailableModels: []tauchat.ChatModelRef{{
			ID:    "test-model",
			URL:   "https://model.example/v1",
			Ready: true,
		}},
	})
}

func keyPress(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}

func cmdReturnsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
