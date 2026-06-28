package tui

import (
	"context"
	"sync"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
)

// nullTerminal is a no-op Terminal so the engine can render without touching a
// real tty during tests.
type nullTerminal struct{}

func (nullTerminal) Start(func(string), func()) {}
func (nullTerminal) SignalStop()                {}
func (nullTerminal) Stop()                      {}
func (nullTerminal) Write(string)               {}
func (nullTerminal) Size() (int, int)           { return 80, 24 }
func (nullTerminal) HideCursor()                {}
func (nullTerminal) ShowCursor()                {}
func (nullTerminal) MoveBy(int)                 {}
func (nullTerminal) ClearLine()                 {}
func (nullTerminal) ClearToEnd()                {}
func (nullTerminal) ClearScreen()               {}
func (nullTerminal) SetTitle(string)            {}

// recordingRuntime captures commands sent by the chat.
type recordingRuntime struct {
	mu       sync.Mutex
	commands []tauchat.ChatCommand
}

func (r *recordingRuntime) Send(cmd tauchat.ChatCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, cmd)
	return nil
}

func (r *recordingRuntime) Close() {}

func (r *recordingRuntime) last() tauchat.ChatCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.commands) == 0 {
		return nil
	}
	return r.commands[len(r.commands)-1]
}

// newTestChat builds an inlineChat wired to a no-op engine and a recording
// runtime, without spawning the background goroutines (which newInlineChat
// would). This isolates command/completion logic.
func newTestChat(t *testing.T) (*inlineChat, *recordingRuntime) {
	t.Helper()
	engine := taui.NewTUI(nullTerminal{})
	rt := &recordingRuntime{}
	c := &inlineChat{
		engine:          engine,
		stage:           &taui.Container{},
		ctx:             context.Background(),
		runtime:         rt,
		notifyQueue:     notify.NewQueue(),
		sessionID:       "sess-1",
		modelName:       "model-a",
		provider:        "prov",
		reasoningEffort: "low",
		availableModels: []tauchat.ChatModelRef{
			{ID: "model-a", Config: config.ModelConfig{ReasoningEfforts: []string{"low", "medium", "high", "max"}}},
			{ID: "model-b", Config: config.ModelConfig{ReasoningEfforts: []string{"low", "medium", "high", "max"}}},
		},
		extensionCommands: map[string]tauchat.ExtensionCommand{},
		bold:              func(s string) string { return s },
		grey:              func(s string) string { return s },
		dim:               func(s string) string { return s },
	}
	c.header = taui.NewText("")
	c.status = taui.NewText("")
	c.input = taui.NewLineInput("› ")
	engine.AddChild(c.header)
	return c, rt
}

func TestHandleSlashCommand_ModelSwitch(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/model model-b")

	cmd, ok := rt.last().(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.last())
	}
	if cmd.Patch.Model == nil || cmd.Patch.Model.ID != "model-b" {
		t.Fatalf("expected model patch model-b, got %+v", cmd.Patch.Model)
	}
	if got := c.modelName; got != "model-b" {
		t.Fatalf("expected modelName model-b, got %q", got)
	}
}

func TestHandleSlashCommand_ModelUnknownDoesNotSend(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/model nope")
	if rt.last() != nil {
		t.Fatalf("expected no command for unknown model, got %T", rt.last())
	}
}

func TestHandleSlashCommand_Effort(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/effort high")

	cmd, ok := rt.last().(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.last())
	}
	if cmd.Patch.ReasoningEffort == nil || *cmd.Patch.ReasoningEffort != "high" {
		t.Fatalf("expected effort high, got %+v", cmd.Patch.ReasoningEffort)
	}
}

func TestHandleEffortCommand_Cycles(t *testing.T) {
	c, _ := newTestChat(t)
	c.reasoningEffort = "low"
	c.handleEffortCommand("") // no arg → cycle
	if c.reasoningEffort != "medium" {
		t.Fatalf("expected cycle low->medium, got %q", c.reasoningEffort)
	}
}

func TestHandleReasoningCommand_Toggles(t *testing.T) {
	c, _ := newTestChat(t)
	c.showReasoning = false
	c.handleReasoningCommand("") // no arg → toggle
	if !c.showReasoning {
		t.Fatal("expected reasoning to toggle on")
	}
	c.handleReasoningCommand("off")
	if c.showReasoning {
		t.Fatal("expected reasoning off")
	}
}

func TestHandleSlashCommand_New(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/new")
	if _, ok := rt.last().(tauchat.ResetChatSessionCommand); !ok {
		t.Fatalf("expected ResetChatSessionCommand, got %T", rt.last())
	}
}

func TestHandleSlashCommand_SessionList(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/session list")
	if _, ok := rt.last().(tauchat.ListSessionsCommand); !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.last())
	}
}

func TestHandleSlashCommand_SessionLoad(t *testing.T) {
	c, rt := newTestChat(t)
	c.handleSlashCommand("/session abc123")
	cmd, ok := rt.last().(tauchat.LoadSessionCommand)
	if !ok {
		t.Fatalf("expected LoadSessionCommand, got %T", rt.last())
	}
	if cmd.SessionID != "abc123" {
		t.Fatalf("expected session abc123, got %q", cmd.SessionID)
	}
}

func TestHandleSlashCommand_Extension(t *testing.T) {
	c, rt := newTestChat(t)
	c.extensionCommands = map[string]tauchat.ExtensionCommand{
		"deploy": {Name: "deploy"},
	}
	c.handleSlashCommand("/deploy now")
	cmd, ok := rt.last().(tauchat.RunExtensionCommandCommand)
	if !ok {
		t.Fatalf("expected RunExtensionCommandCommand, got %T", rt.last())
	}
	if cmd.Name != "deploy" || cmd.Args != "now" {
		t.Fatalf("expected deploy/now, got %q/%q", cmd.Name, cmd.Args)
	}
}

func TestCompletionSet_Commands(t *testing.T) {
	c, _ := newTestChat(t)
	set := c.completionSet(taui.CompletionContext{Text: "/mod", Cursor: 4})
	if set == nil || len(set.Groups) == 0 {
		t.Fatal("expected command completions")
	}
	if !hasWord(set.Groups[0].Matches, "/model") {
		t.Fatalf("expected /model in command completions, got %+v", set.Groups[0].Matches)
	}
}

func TestCompletionSet_ModelArgs(t *testing.T) {
	c, _ := newTestChat(t)
	set := c.completionSet(taui.CompletionContext{Text: "/model ", Cursor: 7})
	if set == nil || len(set.Groups) == 0 {
		t.Fatal("expected model completions")
	}
	if !hasWord(set.Groups[0].Matches, "model-a") || !hasWord(set.Groups[0].Matches, "model-b") {
		t.Fatalf("expected model ids, got %+v", set.Groups[0].Matches)
	}
}

func TestCompletionSet_SessionSubcommands(t *testing.T) {
	c, _ := newTestChat(t)
	set := c.completionSet(taui.CompletionContext{Text: "/session ", Cursor: 9})
	if set == nil || len(set.Groups) == 0 {
		t.Fatal("expected session subcommand completions")
	}
	if !hasWord(set.Groups[0].Matches, "list") {
		t.Fatalf("expected session subcommands, got %+v", set.Groups[0].Matches)
	}
}

func TestCompletionSet_NonSlashReturnsNil(t *testing.T) {
	c, _ := newTestChat(t)
	if set := c.completionSet(taui.CompletionContext{Text: "hello", Cursor: 5}); set != nil {
		t.Fatalf("expected nil for non-slash text, got %+v", set)
	}
}

// TestCompletionSet_CompletedModelArgStops guards the bug where a fully-typed
// model argument followed by a space kept re-opening the dropdown, making the
// line impossible to submit.
func TestCompletionSet_CompletedModelArgStops(t *testing.T) {
	c, _ := newTestChat(t)
	text := "/model model-a "
	if set := c.completionSet(taui.CompletionContext{Text: text, Cursor: len([]rune(text))}); set != nil {
		t.Fatalf("expected nil after a completed model arg, got %+v", set)
	}
}

func TestCompletionSet_ModelArgFiltersInProgress(t *testing.T) {
	c, _ := newTestChat(t)
	text := "/model model-a"
	set := c.completionSet(taui.CompletionContext{Text: text, Cursor: len([]rune(text))})
	if set == nil {
		t.Fatal("expected completions while typing the model arg")
	}
	// ReplaceStart must point at the start of the in-progress token "model-a".
	if set.ReplaceStart != len([]rune("/model ")) {
		t.Fatalf("expected ReplaceStart %d, got %d", len([]rune("/model ")), set.ReplaceStart)
	}
}

func TestCompletionSet_CompletedSessionArgStops(t *testing.T) {
	c, _ := newTestChat(t)
	text := "/session info abc " // second arg complete → nothing more to offer
	if set := c.completionSet(taui.CompletionContext{Text: text, Cursor: len([]rune(text))}); set != nil {
		t.Fatalf("expected nil after a completed session arg, got %+v", set)
	}
}

func hasWord(matches []taui.Match, word string) bool {
	for _, m := range matches {
		if m.Word == word {
			return true
		}
	}
	return false
}
