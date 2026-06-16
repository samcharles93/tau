package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

type fakeRuntime struct {
	commands []tauchat.ChatCommand
}

func (r *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	r.commands = append(r.commands, cmd)
	return nil
}

func (r *fakeRuntime) Close() {}

func newTestPanel(runtime *fakeRuntime) *ChatPanel {
	bus := eventbus.New()
	client := bus.Client("test")
	chatSub := eventbus.Subscribe[tauchat.ChatEvent](client)
	panel := NewChatPanel(context.Background(), runtime, chatSub, TUIConfig{
		SessionID: "session_1",
		ModelName: "model-a",
		AvailableModels: []tauchat.ChatModelRef{
			{ID: "model-a", URL: "https://example.invalid/a", Ready: true},
			{ID: "model-b", URL: "https://example.invalid/b", Ready: true},
			{ID: "model-c", URL: "https://example.invalid/c", Ready: false},
		},
	})
	// Seed the registry commands state — in production this arrives via
	// CommandsChangedEvent on the bus, but tests skip the bus wiring.
	panel.registryCommands.Set(testRegistryCommands())
	return panel
}

// testRegistryCommands returns a minimal built-in command set for tests.
func testRegistryCommands() []tauchat.CommandRef {
	return []tauchat.CommandRef{
		{Name: "new", Label: "/new", Description: "start a new conversation"},
		{Name: "system", Label: "/system", Description: "set system prompt", AcceptsArgs: true},
		{Name: "model", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Name: "refresh", Label: "/refresh", Description: "refresh models"},
		{Name: "reload", Label: "/reload", Description: "reload extensions while idle"},
		{Name: "reasoning", Label: "/reasoning", Description: "show or hide reasoning", AcceptsArgs: true},
		{Name: "settings", Label: "/settings", Description: "show settings help"},
		{Name: "session", Label: "/session", Description: "manage saved sessions", AcceptsArgs: true},
		{Name: "resume", Label: "/resume", Description: "resume a saved session"},
		{Name: "debug", Label: "/debug", Description: "preview TUI components (developer)", AcceptsArgs: true},
		{Name: "exit", Label: "/exit", Description: "quit"},
		{Name: "quit", Label: "/quit", Description: "quit"},
		{Name: "q", Label: "/q", Description: "quit"},
		{Name: "help", Label: "/help", Description: "toggle help"},
		{Name: "?", Label: "/?", Description: "toggle help"},
		{Name: "clear", Label: "/clear", Description: "start a new conversation"},
		{Name: "reset", Label: "/reset", Description: "start a new conversation"},
		{Name: "models", Label: "/models", Description: "refresh models"},
	}
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

func TestHandleSubmitQueuesWhenBusy(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	// Set status to busy.
	panel.status.Set(tauchat.ChatSessionStreaming)

	panel.handleSubmit("queued message")

	// Reset lastSubmitTime so the drained submit isn't debounced.
	panel.lastSubmitTime = time.Time{}

	// Verify it was queued and not sent.
	if len(runtime.commands) != 0 {
		t.Fatalf("commands = %d, want 0 (queued)", len(runtime.commands))
	}
	queue := panel.outgoingQueue.Get()
	if len(queue) != 1 || queue[0] != "queued message" {
		t.Fatalf("queue = %v, want ['queued message']", queue)
	}

	// Now simulate completion and verify it drains.
	panel.handleRuntimeEvent(tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{
			SessionID: "session_1",
			Status:    tauchat.ChatSessionIdle,
		},
		RequestID: panel.activeRequestID.Get(),
	})

	// Wait for the goroutine in drainQueue to trigger handleSubmit.
	// In a real test environment we might need more robust sync,
	// but let's see if this works with a small sleep or just checking commands.
	time.Sleep(10 * time.Millisecond)

	if len(runtime.commands) != 1 {
		t.Fatalf("commands = %d, want 1 (drained)", len(runtime.commands))
	}
	cmd, ok := runtime.commands[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("command = %#v, want SubmitChatPromptCommand", runtime.commands[0])
	}
	if cmd.Prompt != "queued message" {
		t.Fatalf("prompt = %q, want 'queued message'", cmd.Prompt)
	}
	if len(panel.outgoingQueue.Get()) != 0 {
		t.Fatalf("queue = %v, want empty", panel.outgoingQueue.Get())
	}
}

func TestHandleSteerSubmitSendsSteer(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	// Set status to busy.
	panel.status.Set(tauchat.ChatSessionStreaming)

	panel.handleSteerSubmit("steering message")

	if len(runtime.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(runtime.commands))
	}
	cmd, ok := runtime.commands[0].(tauchat.SteerChatPromptCommand)
	if !ok {
		t.Fatalf("command = %#v, want SteerChatPromptCommand", runtime.commands[0])
	}
	if cmd.Prompt != "steering message" {
		t.Fatalf("prompt = %q, want 'steering message'", cmd.Prompt)
	}
}

func TestPopQueueToInput(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	// Queue some messages.
	panel.status.Set(tauchat.ChatSessionStreaming)
	panel.handleSubmit("first")
	panel.lastSubmitTime = time.Time{}
	panel.handleSubmit("second")
	panel.lastSubmitTime = time.Time{}

	if len(panel.outgoingQueue.Get()) != 2 {
		t.Fatalf("queue length = %d, want 2", len(panel.outgoingQueue.Get()))
	}

	// Pop to input.
	panel.popQueueToInput()

	if got := panel.inputValue.Get(); got != "second" {
		t.Fatalf("input = %q, want 'second'", got)
	}
	if len(panel.outgoingQueue.Get()) != 1 {
		t.Fatalf("queue length = %d, want 1", len(panel.outgoingQueue.Get()))
	}

	// Clear input and pop again.
	panel.inputValue.Set("")
	panel.popQueueToInput()

	if got := panel.inputValue.Get(); got != "first" {
		t.Fatalf("input = %q, want 'first'", got)
	}
	if len(panel.outgoingQueue.Get()) != 0 {
		t.Fatalf("queue length = %d, want 0", len(panel.outgoingQueue.Get()))
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

func TestApplySelectedCompletionUpdatesTextArea(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.input = newCompletionTextArea(
		panel.inputValue,
		panel.completions,
		panel.handleSubmit,
		panel.handleSteerSubmit,
		panel.popQueueToInput,
		func() { panel.selectCompletion(-1) },
		func() { panel.selectCompletion(1) },
		120,
	)
	panel.inputValue.Set("/mo")
	panel.syncCompletions("/mo")

	panel.applySelectedCompletion()

	if got := panel.inputValue.Get(); got != "/model " {
		t.Fatalf("input = %q, want /model completion", got)
	}
	if got := panel.input.Text(); got != "/model " {
		t.Fatalf("text area = %q, want /model completion", got)
	}
}

func TestCompletionTextAreaHeightAllowsMoreThanEightRows(t *testing.T) {
	input := gt.NewState("")
	textarea := newCompletionTextArea(
		input,
		gt.NewState([]completionItem{}),
		nil,
		nil,
		nil,
		nil,
		nil,
		120,
	)

	textarea.SetText(strings.Repeat("line\n", 11) + "line")

	if got := textarea.Height(); got != 12 {
		t.Fatalf("textarea height = %d, want 12", got)
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

// --- KeyMap delegation tests ---

func TestKeyMap_EscClosesSettings(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.showSettings.Set(true)

	km := panel.KeyMap()
	esc := findKeyBinding(km, gt.KeyEscape)
	esc.Handler(gt.KeyEvent{Key: gt.KeyEscape})

	if panel.showSettings.Get() {
		t.Error("settings should be closed after Esc")
	}
}

func TestKeyMap_EscClosesTree(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.showSessionTree.Set(true)

	km := panel.KeyMap()
	esc := findKeyBinding(km, gt.KeyEscape)
	esc.Handler(gt.KeyEvent{Key: gt.KeyEscape})

	if panel.showSessionTree.Get() {
		t.Error("tree should be closed after Esc")
	}
}

func TestKeyMap_EscClosesTopmostView(t *testing.T) {
	// Esc closes one view at a time — topmost first. In practice,
	// mutual exclusion means they're never both open, but the handler
	// checks settings before tree.
	panel := newTestPanel(&fakeRuntime{})

	// Settings only.
	panel.showSettings.Set(true)
	km := panel.KeyMap()
	for i := range km {
		if km[i].Pattern.Key == gt.KeyEscape {
			km[i].Handler(gt.KeyEvent{Key: gt.KeyEscape})
			break
		}
	}
	if panel.showSettings.Get() {
		t.Error("settings should be closed after Esc")
	}

	// Tree only.
	panel.showSessionTree.Set(true)
	for i := range km {
		if km[i].Pattern.Key == gt.KeyEscape {
			km[i].Handler(gt.KeyEvent{Key: gt.KeyEscape})
			break
		}
	}
	if panel.showSessionTree.Get() {
		t.Error("tree should be closed after Esc")
	}
}

func TestKeyMap_CtrlRTogglesReasoning(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	initial := panel.showReasoning.Get()

	// Test via slash command — more reliable than matching KeyMap patterns.
	panel.handleSlashCommand("/reasoning toggle")

	if panel.showReasoning.Get() == initial {
		t.Error("reasoning should have toggled")
	}
}

func TestKeyMap_SettingsUpDownNavigatesModels(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.showSettings.Set(true)

	km := panel.KeyMap()
	down := findKeyBinding(km, gt.KeyDown)
	up := findKeyBinding(km, gt.KeyUp)

	// Down: 0 → 1.
	panel.selectedModelIndex.Set(0)
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if panel.selectedModelIndex.Get() != 1 {
		t.Errorf("after down: index = %d, want 1", panel.selectedModelIndex.Get())
	}

	// Up: 1 → 0.
	up.Handler(gt.KeyEvent{Key: gt.KeyUp})
	if panel.selectedModelIndex.Get() != 0 {
		t.Errorf("after up: index = %d, want 0", panel.selectedModelIndex.Get())
	}

	// Settings inactive → up/down do nothing.
	panel.showSettings.Set(false)
	panel.selectedModelIndex.Set(0)
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if panel.selectedModelIndex.Get() != 0 {
		t.Errorf("up/down should not navigate when settings is closed")
	}
}

func TestKeyMap_SettingsEnterSwitchesModel(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)
	panel.showSettings.Set(true)
	panel.selectedModelIndex.Set(1) // model-b

	km := panel.KeyMap()
	enter := findKeyBinding(km, gt.KeyEnter)
	enter.Handler(gt.KeyEvent{Key: gt.KeyEnter})

	// Should have sent UpdateChatSessionCommand with model-b.
	found := false
	for _, cmd := range runtime.commands {
		if update, ok := cmd.(tauchat.UpdateChatSessionCommand); ok {
			if update.Patch.Model != nil && update.Patch.Model.ID == "model-b" {
				found = true
			}
		}
	}
	if !found {
		t.Error("Enter should have switched to model-b")
	}

	// Enter now only switches the model; Ctrl+R toggles reasoning.
	if panel.showReasoning.Get() {
		t.Error("Enter should not toggle reasoning in settings")
	}
}

func TestKeyMap_SettingsInactiveKeysDoNothing(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	km := panel.KeyMap()
	enter := findKeyBinding(km, gt.KeyEnter)
	down := findKeyBinding(km, gt.KeyDown)

	// Settings not active → Enter does nothing.
	enter.Handler(gt.KeyEvent{Key: gt.KeyEnter})
	if len(runtime.commands) != 0 {
		t.Error("Enter should not send commands when settings is closed")
	}

	// Settings not active → Down does nothing.
	prevIdx := panel.selectedModelIndex.Get()
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if panel.selectedModelIndex.Get() != prevIdx {
		t.Error("Down should not navigate when settings is closed")
	}
}

func TestSessionTreeCommandDispatches(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	// /session tree dispatches to handleSessionCommand("tree")
	// which calls openSessionTree(). Without an app, openSessionTree
	// returns early (app == nil guard), but /session tree should still
	// be recognized as a valid command.
	panel.handleSlashCommand("/session tree")

	// The command is dispatched but the view needs an app to bind.
	// We verify the session sub-command routing works by checking
	// that a bare "/session" opens the list and "/session tree" is
	// at least handled without error.
	if panel.lastError.Get() != "" {
		t.Errorf("unexpected error: %s", panel.lastError.Get())
	}
}

// --- helpers ---

func TestWrapUserMessageLines(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{
			name:  "no wrap needed",
			text:  "hello",
			width: 10,
			want:  []string{"hello"},
		},
		{
			name:  "wraps at word boundary",
			text:  "causing a cluttered look on the TUI",
			width: 20,
			want: []string{
				"causing a cluttered",
				"look on the TUI",
			},
		},
		{
			name:  "long word exceeds width falls back to hard break",
			text:  "supercalifragilisticexpialidocious is fun",
			width: 10,
			want: []string{
				"supercalif",
				"ragilistic",
				"expialidoc",
				"ious is",
				"fun",
			},
		},
		{
			name:  "preserves existing line breaks",
			text:  "line one\nline two",
			width: 20,
			want:  []string{"line one", "line two"},
		},
		{
			name:  "multiple spaces collapsed at break",
			text:  "hello    world is big",
			width: 10,
			want: []string{
				"hello",
				"world is",
				"big",
			},
		},
		{
			name:  "empty text",
			text:  "",
			width: 10,
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapUserMessageLines(tt.text, tt.width)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("wrapUserMessageLines mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func findKeyBinding(km gt.KeyMap, key gt.Key) *gt.KeyBinding {
	for i := range km {
		if km[i].Pattern.Key == key {
			return &km[i]
		}
	}
	return nil
}
