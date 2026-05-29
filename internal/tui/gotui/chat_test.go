package gotui

import (
	"context"
	"strings"
	"testing"

	gotui "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/theme"
)

type fakeRuntime struct {
	commands []tauchat.ChatCommand
}

func (r *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	r.commands = append(r.commands, cmd)
	return nil
}

func (r *fakeRuntime) SubscribeEvents(int) (*pubsub.Subscription[tauchat.ChatEvent], error) {
	return nil, nil
}

func (r *fakeRuntime) Close() {}

func newTestPanel(runtime *fakeRuntime) *ChatPanel {
	return NewChatPanel(context.Background(), runtime, nil, Config{
		SessionID: "session_1",
		ModelName: "model-a",
		AvailableModels: []tauchat.ChatModelRef{
			{ID: "model-a", URL: "https://example.invalid/a", Ready: true},
			{ID: "model-b", URL: "https://example.invalid/b", Ready: true},
			{ID: "model-c", URL: "https://example.invalid/c", Ready: false},
		},
	})
}

func TestHandleSubmitSendsPrompt(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	panel.inputValue.Set("hello")
	panel.handleSubmit("hello")

	if got := panel.inputValue.Get(); got != "" {
		t.Fatalf("input = %q, want empty", got)
	}
	if len(runtime.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(runtime.commands))
	}
	cmd, ok := runtime.commands[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("command = %#v, want SubmitChatPromptCommand", runtime.commands[0])
	}
	if cmd.SessionID != "session_1" || cmd.Prompt != "hello" || cmd.RequestID == "" {
		t.Fatalf("command = %#v, want populated prompt for session_1", cmd)
	}
}

func TestSlashModelSendsUpdate(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	panel.handleSlashCommand("/model model-b")

	if len(runtime.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(runtime.commands))
	}
	cmd, ok := runtime.commands[0].(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("command = %#v, want UpdateChatSessionCommand", runtime.commands[0])
	}
	if cmd.Patch.Model == nil || cmd.Patch.Model.ID != "model-b" {
		t.Fatalf("model patch = %#v, want model-b", cmd.Patch.Model)
	}
}

func TestSlashModelRejectsNotReady(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	panel.handleSlashCommand("/model model-c")

	if len(runtime.commands) != 0 {
		t.Fatalf("commands = %d, want 0", len(runtime.commands))
	}
	if panel.lastError.Get() == "" {
		t.Fatal("lastError is empty, want not-ready error")
	}
}

func TestRuntimeEventsUpdateStreamingState(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})

	panel.handleRuntimeEvent(tauchat.ChatResponseStartedEvent{
		SessionID: "session_1",
		RequestID: "request_1",
	})
	panel.handleRuntimeEvent(tauchat.ChatResponseDeltaEvent{
		SessionID: "session_1",
		RequestID: "request_1",
		Delta:     "hello",
		Snapshot:  "hello",
	})

	if got := panel.status.Get(); got != tauchat.ChatSessionStreaming {
		t.Fatalf("status = %q, want streaming", got)
	}
	if got := panel.streamingContent.Get(); got != "hello" {
		t.Fatalf("streamingContent = %q, want hello", got)
	}
}

func TestCompletedEventSyncsMessages(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	state := tauchat.ChatSessionState{
		SessionID: "session_1",
		Status:    tauchat.ChatSessionIdle,
		Messages:  []tauchat.ChatMessage{{Role: tauchat.ChatRoleAssistant, Content: "done"}},
	}

	panel.handleRuntimeEvent(tauchat.ChatResponseCompletedEvent{State: state, RequestID: "request_1"})

	messages := panel.messages.Get()
	if len(messages) != 1 || messages[0].Content != "done" {
		t.Fatalf("messages = %#v, want completed assistant message", messages)
	}
	if got := panel.streamingContent.Get(); got != "" {
		t.Fatalf("streamingContent = %q, want empty", got)
	}
}

func TestUserMessageHasNoRedundantUserLabel(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})

	message := panel.renderMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleUser, Content: "hello"})
	body := message.Children()[1]
	children := body.Children()

	if len(children) != 1 {
		t.Fatalf("user body children = %d, want only content", len(children))
	}
	if got := children[0].Text(); got != "hello" {
		t.Fatalf("user content = %q, want hello", got)
	}
	if got := message.Children()[0].Text(); got == "u" {
		t.Fatalf("user rail = %q, should not show u", got)
	}
}

func TestReasoningRendersBeforeAssistantContent(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.showReasoning.Set(true)

	message := panel.renderMessage(tauchat.ChatMessage{
		Role:             tauchat.ChatRoleAssistant,
		ReasoningContent: "thinking first",
		Content:          "answer second",
	})
	body := message.Children()[1]
	children := body.Children()

	if len(children) != 3 {
		t.Fatalf("assistant body children = %d, want label, reasoning, content", len(children))
	}
	if got := children[1].Text(); got != "Reasoning\nthinking first" {
		t.Fatalf("reasoning child = %q, want reasoning before content", got)
	}
	if got := children[2].Text(); got != "answer second" {
		t.Fatalf("content child = %q, want content after reasoning", got)
	}
}

func TestSlashCommandCompletionsApplyBeforeSubmit(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	panel.inputValue.Set("/mo")
	panel.syncCompletions("/mo")
	panel.handleSubmit("/mo")

	if got := panel.inputValue.Get(); got != "/model " {
		t.Fatalf("input = %q, want /model completion", got)
	}
	if len(runtime.commands) != 0 {
		t.Fatalf("commands = %d, want 0 before completed slash command submit", len(runtime.commands))
	}
}

func TestModelArgumentCompletions(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})

	items := panel.completionItems("/model model-b")

	if len(items) != 1 {
		t.Fatalf("completion count = %d, want 1", len(items))
	}
	if got := items[0].Value; got != "/model model-b" {
		t.Fatalf("completion value = %q, want /model model-b", got)
	}
}

func TestApplySelectedCompletionMovesCursorToEnd(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.input = newChatInput(
		panel.inputValue,
		120,
		"",
		theme.BodyStyle(),
		theme.DimStyle(),
		theme.ColorPurple,
		panel.handleSubmit,
		panel.syncCompletions,
	)
	panel.inputValue.Set("/mo")
	panel.syncCompletions("/mo")

	panel.applySelectedCompletion()

	if got := panel.inputValue.Get(); got != "/model " {
		t.Fatalf("input = %q, want /model completion", got)
	}
	if got, want := panel.input.cursorPos.Get(), len([]rune("/model ")); got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
}

func TestSettingsCommandOpensDialog(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})

	panel.handleSlashCommand("/settings")

	if !panel.showSettings.Get() {
		t.Fatal("settings dialog is closed, want open")
	}
}

func TestStatusBarShowsSelectedModel(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.modelName.Set("model-b")

	statusBar := panel.renderStatusBar()

	found := false
	for _, child := range statusBar.Children() {
		if strings.Contains(child.Text(), "model-b") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("status bar children did not include selected model: %#v", statusBar.Children())
	}
}

func TestInputCtrlWordMovement(t *testing.T) {
	value := gotui.NewState("hello brave world")
	input := newChatInput(value, 120, "", theme.BodyStyle(), theme.DimStyle(), theme.ColorPurple, nil, nil)

	input.SetText("hello brave world")
	input.moveWordLeft()
	if got := input.cursorPos.Get(); got != len([]rune("hello brave ")) {
		t.Fatalf("cursor after word-left = %d, want %d", got, len([]rune("hello brave ")))
	}
	input.moveWordRight()
	if got := input.cursorPos.Get(); got != len([]rune("hello brave world")) {
		t.Fatalf("cursor after word-right = %d, want end", got)
	}
}
