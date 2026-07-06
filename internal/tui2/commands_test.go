package tui2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

var errIntentional = errors.New("intentional error for testing")

// --- cmdModel ---------------------------------------------------------------

func TestCmdModelFoundSendsUpdate(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "gpt-4", Provider: "openai"},
		{ID: "claude-3", Provider: "anthropic"},
	}

	drainCmd(m.cmdModel("gpt-4"))

	if m.modelName != "gpt-4" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-4")
	}
	if m.provider != "openai" {
		t.Fatalf("provider = %q, want %q", m.provider, "openai")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 UpdateChatSessionCommand, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.sent[0])
	}
	if cmd.Patch.Model == nil || cmd.Patch.Model.ID != "gpt-4" {
		t.Fatalf("patch.Model.ID = %v, want 'gpt-4'", cmd.Patch.Model)
	}
	if cmd.Patch.Provider == nil || *cmd.Patch.Provider != "openai" {
		t.Fatalf("patch.Provider = %v, want 'openai'", cmd.Patch.Provider)
	}
}

func TestCmdModelNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}

	drainCmd(m.cmdModel("nonexistent"))

	if m.modelName == "nonexistent" {
		t.Error("modelName should not change when model not found")
	}
	if m.notification == "" {
		t.Error("expected a notification when model not found")
	}
}

func TestCmdModelEmptyAvailable(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// No available models set.

	drainCmd(m.cmdModel(""))

	if m.notification == "" {
		t.Error("expected notification when no models available")
	}
}

func TestCmdModelEmptyPrefillsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}

	cmd := m.cmdModel("")
	if cmd != nil {
		t.Error("expected nil Cmd when pre-filling input for picker")
	}
	if m.input != "/model " {
		t.Fatalf("input = %q, want %q", m.input, "/model ")
	}
}

func TestCmdModelSetsDefaultEffort(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "deepseek", Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		}},
	}
	drainCmd(m.cmdModel("deepseek"))

	if m.reasoningEffort != "medium" {
		t.Fatalf("reasoningEffort = %q, want %q (first default)", m.reasoningEffort, "medium")
	}
}

// --- cmdSystem --------------------------------------------------------------

func TestCmdSystemEmptyPrompt(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdSystem(""))

	if m.notification == "" {
		t.Error("expected notification for empty system prompt")
	}
}

func TestCmdSystemSendsUpdate(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSystem("you are helpful"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.sent[0])
	}
	if cmd.Patch.SystemPrompt == nil || *cmd.Patch.SystemPrompt != "you are helpful" {
		t.Fatalf("SystemPrompt = %v, want 'you are helpful'", cmd.Patch.SystemPrompt)
	}
}

// --- cmdReasoning -----------------------------------------------------------

func TestCmdReasoningToggles(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.showReasoning = false

	drainCmd(m.cmdReasoning(""))
	if !m.showReasoning {
		t.Error("expected reasoning to toggle on")
	}

	drainCmd(m.cmdReasoning(""))
	if m.showReasoning {
		t.Error("expected reasoning to toggle off")
	}
}

func TestCmdReasoningOn(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdReasoning("on"))
	if !m.showReasoning {
		t.Error("expected showReasoning=true with 'on'")
	}
}

func TestCmdReasoningOff(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.showReasoning = true

	drainCmd(m.cmdReasoning("off"))
	if m.showReasoning {
		t.Error("expected showReasoning=false with 'off'")
	}
}

func TestCmdReasoningAuto(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.showReasoning = true

	drainCmd(m.cmdReasoning("auto"))
	if m.showReasoning {
		t.Error("expected showReasoning=false with 'auto'")
	}
}

func TestCmdReasoningInvalidArgToggles(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.showReasoning = false

	drainCmd(m.cmdReasoning("banana"))
	if !m.showReasoning {
		t.Error("expected invalid arg to toggle reasoning on")
	}
}

// --- cmdEffort --------------------------------------------------------------

func TestCmdEffortCycles(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.reasoningEffort = "auto"
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "claude-3", Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		}},
	}
	m.modelName = "claude-3"

	drainCmd(m.cmdEffort(""))

	// Should cycle from "auto" to the first available: "low"
	if m.reasoningEffort != "low" {
		t.Fatalf("after cycle: reasoningEffort = %q, want %q", m.reasoningEffort, "low")
	}
	if len(rt.sent) == 0 {
		t.Fatal("expected update command to be sent")
	}
}

func TestCmdEffortSpecificLevel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "deepseek", Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		}},
	}
	m.modelName = "deepseek"

	drainCmd(m.cmdEffort("high"))

	if m.reasoningEffort != "high" {
		t.Fatalf("reasoningEffort = %q, want %q", m.reasoningEffort, "high")
	}
}

func TestCmdEffortMedAlias(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "deepseek", Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		}},
	}
	m.modelName = "deepseek"

	drainCmd(m.cmdEffort("med"))

	if m.reasoningEffort != "medium" {
		t.Fatalf("reasoningEffort = %q, want %q after 'med' alias", m.reasoningEffort, "medium")
	}
}

func TestCmdEffortInvalidLevel(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "deepseek", Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		}},
	}
	m.modelName = "deepseek"
	m.reasoningEffort = "medium"

	drainCmd(m.cmdEffort("extreme"))

	if m.reasoningEffort != "medium" {
		t.Fatal("effort should not change when level is invalid")
	}
	if m.notification == "" {
		t.Error("expected notification for invalid effort level")
	}
}

func TestCmdEffortModelWithoutCustomLevels(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.modelName = "gpt-4"
	m.reasoningEffort = "auto"

	drainCmd(m.cmdEffort(""))

	// Model without custom efforts has only ["auto"]. Cycling from "auto"
	// with a 1-element list returns "auto" again.
	if m.reasoningEffort != "auto" {
		t.Fatalf("reasoningEffort = %q, want %q", m.reasoningEffort, "auto")
	}
}

// --- cmdRefresh -------------------------------------------------------------

func TestCmdRefreshNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.refresh = nil

	drainCmd(m.cmdRefresh(""))

	if m.notification == "" {
		t.Error("expected notification when refresh is nil")
	}
}

func TestCmdRefreshError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	callCount := 0
	m.refresh = func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		callCount++
		return nil, errIntentional
	}

	cmd := m.cmdRefresh("")
	msgs := drainCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	_, ok := msgs[0].(refreshResultMsg)
	if !ok {
		t.Fatalf("expected refreshResultMsg, got %T", msgs[0])
	}

	// Apply the result.
	m.Update(msgs[0])
	if m.notification == "" {
		t.Error("expected error notification after refresh failure")
	}
	if callCount != 1 {
		t.Fatalf("refresh called %d times, want 1", callCount)
	}
}

func TestCmdRefreshSuccess(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.refresh = func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		return []tauchat.ChatModelRef{{ID: "gpt-5"}, {ID: "claude-4"}}, nil
	}

	cmd := m.cmdRefresh("")
	msgs := drainCmd(cmd)
	m.Update(msgs[0])

	if len(m.availableModels) != 2 {
		t.Fatalf("availableModels = %d, want 2", len(m.availableModels))
	}
	if m.notification == "" {
		t.Error("expected success notification after refresh")
	}
}

// --- cmdCost ----------------------------------------------------------------

func TestCmdCostNoUsage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = nil

	drainCmd(m.cmdCost(""))

	if m.notification == "" {
		t.Error("expected notification when no usage tracker")
	}
}

// --- cmdClear ---------------------------------------------------------------

func TestCmdClearSendsReset(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdClear(""))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ResetChatSessionCommand); !ok {
		t.Fatalf("expected ResetChatSessionCommand, got %T", rt.sent[0])
	}
}

// --- cmdHelp ----------------------------------------------------------------

func TestCmdHelpAppendsMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.renderedLines = nil

	drainCmd(m.cmdHelp(""))

	// Should append a system message with the help text.
	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "Commands:") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected help text to be appended to renderedLines")
	}
}

func TestCmdHelpShowsCtrlS(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdHelp(""))

	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "Ctrl+S") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Ctrl+S steer hint in help output")
	}
}

// --- cmdSession -------------------------------------------------------------

func TestCmdSessionListSendsCommand(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession(""))
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ListSessionsCommand); !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
}

func TestCmdSessionListExplicit(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("list"))
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ListSessionsCommand); !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
}

func TestCmdSessionInfoRequiresID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdSession("info"))
	if !strings.Contains(m.notification, "usage") {
		t.Errorf("notification = %q, want a usage hint", m.notification)
	}
}

func TestCmdSessionInfoNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdSession("info sess-missing"))
	if !strings.Contains(m.notification, "sess-missing") || !strings.Contains(m.notification, "not found") {
		t.Errorf("notification = %q, want a 'not found' hint mentioning the id", m.notification)
	}
}

func TestCmdSessionInfoFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionSummaries = []tauchat.SessionSummary{
		{ID: "sess-abc", ModelID: "gpt-4", Provider: "openai", MessageCount: 3},
	}

	cmd := m.cmdSession("info sess-abc")
	if cmd != nil {
		t.Error("expected nil Cmd — session detail is appended directly, not via notification")
	}
	joined := strings.Join(m.renderedLines, "\n")
	for _, want := range []string{"sess-abc", "gpt-4", "openai", "3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rendered session info missing %q, got %q", want, joined)
		}
	}
}

func TestSessionInfoTextIncludesOptionalFields(t *testing.T) {
	now := time.Now()
	s := tauchat.SessionSummary{
		ID: "sess-1", ModelID: "gpt-4", Provider: "openai", MessageCount: 5,
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150, Cost: 0.02,
		DurationMs: 2500, ToolCalls: 3, ToolErrors: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	out := sessionInfoText(s)
	for _, want := range []string{
		"Session sess-1", "Model: gpt-4", "Provider: openai", "Messages: 5",
		"↑100", "↓50", "total 150", "$0.0200", "2s", "Tool calls: 3", "(1 errors)",
		"Created:", "Updated:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sessionInfoText missing %q, got %q", want, out)
		}
	}
}

func TestCmdSessionExportDefaultID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.sessionID = "sess-123"

	drainCmd(m.cmdSession("export"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.ExportSessionCommand)
	if !ok {
		t.Fatalf("expected ExportSessionCommand, got %T", rt.sent[0])
	}
	if cmd.SessionID != "sess-123" {
		t.Fatalf("SessionID = %q, want %q", cmd.SessionID, "sess-123")
	}
}

func TestCmdSessionExportCustomID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("export other-sess"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.ExportSessionCommand)
	if cmd.SessionID != "other-sess" {
		t.Fatalf("SessionID = %q, want %q", cmd.SessionID, "other-sess")
	}
}

func TestCmdSessionDeleteEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdSession("delete"))
	if m.notification == "" {
		t.Error("expected notification when delete missing ID")
	}
}

func TestCmdSessionDeleteWithID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("delete sess-xyz"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.DeleteSessionCommand)
	if cmd.SessionID != "sess-xyz" {
		t.Fatalf("SessionID = %q, want %q", cmd.SessionID, "sess-xyz")
	}
}

func TestCmdSessionDefaultLoad(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("sess-abc"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.LoadSessionCommand)
	if cmd.SessionID != "sess-abc" {
		t.Fatalf("SessionID = %q, want %q", cmd.SessionID, "sess-abc")
	}
}

// --- cmdResume --------------------------------------------------------------

func TestCmdResumeEmptyListsSessions(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdResume(""))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ListSessionsCommand); !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
}

func TestCmdResumeWithID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdResume("sess-abc"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.LoadSessionCommand)
	if cmd.SessionID != "sess-abc" {
		t.Fatalf("SessionID = %q, want %q", cmd.SessionID, "sess-abc")
	}
}

// --- handleSlashCommand -----------------------------------------------------

func TestHandleSlashCommandKnown(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	cmd := m.handleSlashCommand("/clear")
	if cmd == nil {
		t.Fatal("expected a Cmd for known command /clear")
	}
	drainCmd(cmd)

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
}

func TestHandleSlashCommandUnknown(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleSlashCommand("/nonexistent")
	if m.notification == "" {
		t.Error("expected notification for unknown command")
	}
}

func TestHandleSlashCommandExtension(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.extensionCommands = map[string]tauchat.ExtensionCommand{
		"mcp": {Name: "mcp", Description: "MCP commands", Subcommands: []tauchat.ExtensionCommand{
			{Name: "list", Description: "list servers"},
		}},
	}

	drainCmd(m.handleSlashCommand("/mcp list"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.RunExtensionCommandCommand)
	if !ok {
		t.Fatalf("expected RunExtensionCommandCommand, got %T", rt.sent[0])
	}
	if cmd.Name != "mcp list" {
		t.Fatalf("Name = %q, want %q", cmd.Name, "mcp list")
	}
}

func TestHandleSlashCommandExtensionNoSubcommand(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.extensionCommands = map[string]tauchat.ExtensionCommand{
		"greet": {Name: "greet", Description: "say hello"},
	}

	drainCmd(m.handleSlashCommand("/greet world"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RunExtensionCommandCommand)
	if cmd.Name != "greet" {
		t.Fatalf("Name = %q, want %q", cmd.Name, "greet")
	}
	if cmd.Args != "world" {
		t.Fatalf("Args = %q, want %q", cmd.Args, "world")
	}
}

func TestHandleSlashCommandSkillsReload(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleSlashCommand("/skills-reload"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ReloadSkillsCommand); !ok {
		t.Fatalf("expected ReloadSkillsCommand, got %T", rt.sent[0])
	}
}

func TestHandleSlashCommandSkillEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleSlashCommand("/skill:")
	if m.notification == "" {
		t.Error("expected notification for empty skill name")
	}
}

func TestHandleSlashCommandSkillByName(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleSlashCommand("/skill:my-skill arg1 arg2"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.RunSkillCommand)
	if !ok {
		t.Fatalf("expected RunSkillCommand, got %T", rt.sent[0])
	}
	if cmd.SkillName != "my-skill" {
		t.Fatalf("SkillName = %q, want %q", cmd.SkillName, "my-skill")
	}
	if cmd.Args != "arg1 arg2" {
		t.Fatalf("Args = %q, want %q", cmd.Args, "arg1 arg2")
	}
}

// --- currentModelRef --------------------------------------------------------

func TestCurrentModelRefFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "gpt-4"},
		{ID: "claude-3"},
	}
	m.modelName = "claude-3"

	ref := m.currentModelRef()
	if ref.ID != "claude-3" {
		t.Fatalf("currentModelRef = %q, want %q", ref.ID, "claude-3")
	}
}

func TestCurrentModelRefNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.modelName = "nonexistent"

	ref := m.currentModelRef()
	if ref.ID != "" {
		t.Fatalf("expected empty ref for unknown model, got %q", ref.ID)
	}
}

// --- effortLevels -----------------------------------------------------------

func TestEffortLevelsCustom(t *testing.T) {
	model := tauchat.ChatModelRef{
		ID: "deepseek",
		Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		},
	}
	levels := effortLevels(model)
	if len(levels) != 4 {
		t.Fatalf("expected 4 levels (3 custom + auto), got %d: %v", len(levels), levels)
	}
	if levels[len(levels)-1] != "auto" {
		t.Fatalf("last level should be 'auto', got %q", levels[len(levels)-1])
	}
}

func TestEffortLevelsNoCustom(t *testing.T) {
	model := tauchat.ChatModelRef{ID: "gpt-4"}
	levels := effortLevels(model)
	if len(levels) != 1 || levels[0] != "auto" {
		t.Fatalf("expected [auto], got %v", levels)
	}
}

// --- nextEffort -------------------------------------------------------------

func TestNextEffortCycles(t *testing.T) {
	levels := []string{"low", "medium", "high", "auto"}
	tests := []struct {
		current string
		want    string
	}{
		{"low", "medium"},
		{"medium", "high"},
		{"high", "auto"},
		{"auto", "low"},
	}
	for _, tc := range tests {
		got := nextEffort(tc.current, levels)
		if got != tc.want {
			t.Errorf("nextEffort(%q, %v) = %q, want %q", tc.current, levels, got, tc.want)
		}
	}
}

func TestNextEffortNotFound(t *testing.T) {
	levels := []string{"auto"}
	got := nextEffort("unknown", levels)
	if got != "auto" {
		t.Fatalf("nextEffort('unknown', [auto]) = %q, want %q", got, "auto")
	}
}

// --- defaultEffortForModel --------------------------------------------------

func TestDefaultEffortForModelMediumPreferred(t *testing.T) {
	model := tauchat.ChatModelRef{
		Config: config.ModelConfig{
			ReasoningEfforts: []string{"low", "medium", "high"},
		},
	}
	effort := defaultEffortForModel(model)
	if effort != "medium" {
		t.Fatalf("defaultEffortForModel = %q, want %q", effort, "medium")
	}
}

func TestDefaultEffortForModelFirst(t *testing.T) {
	model := tauchat.ChatModelRef{
		Config: config.ModelConfig{
			ReasoningEfforts: []string{"high", "low"},
		},
	}
	effort := defaultEffortForModel(model)
	if effort != "high" {
		t.Fatalf("defaultEffortForModel = %q, want %q (first available)", effort, "high")
	}
}

func TestDefaultEffortForModelEmpty(t *testing.T) {
	model := tauchat.ChatModelRef{}
	effort := defaultEffortForModel(model)
	if effort != "" {
		t.Fatalf("defaultEffortForModel = %q, want empty", effort)
	}
}

// --- runAgentCommand --------------------------------------------------------

func TestRunAgentCommandNoArgs(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.runAgentCommand("plan", ""))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	_, ok := rt.sent[0].(tauchat.RunAgentCommand)
	if !ok {
		t.Fatalf("expected RunAgentCommand, got %T", rt.sent[0])
	}
}

func TestRunAgentCommandWithArgs(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = false

	drainCmd(m.runAgentCommand("plan", "implement auth"))

	// Should send RunAgentCommand AND start a turn with the args.
	if len(rt.sent) < 1 {
		t.Fatalf("expected at least 1 command, got %d", len(rt.sent))
	}
	foundAgent := false
	foundPrompt := false
	for _, cmd := range rt.sent {
		switch cmd.(type) {
		case tauchat.RunAgentCommand:
			foundAgent = true
		case tauchat.SubmitChatPromptCommand:
			foundPrompt = true
		}
	}
	if !foundAgent {
		t.Error("expected RunAgentCommand in sent commands")
	}
	if !foundPrompt {
		t.Error("expected SubmitChatPromptCommand with args")
	}
}

// --- cmdSkills / cmdProvider ------------------------------------------------

func TestCmdSkillsList(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSkills("list"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ListSkillsCommand); !ok {
		t.Fatalf("expected ListSkillsCommand, got %T", rt.sent[0])
	}
}

func TestCmdSkillsNoArgs(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSkills(""))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.ListSkillsCommand); !ok {
		t.Fatalf("expected ListSkillsCommand, got %T", rt.sent[0])
	}
}

func TestCmdSkillsInvalidArgs(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	drainCmd(m.cmdSkills("invalid"))
	if m.notification == "" {
		t.Error("expected notification for invalid skills args")
	}
}

func TestCmdProviderEmptyShowsMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	cmd := m.cmdProvider("")
	if cmd != nil {
		t.Error("expected nil Cmd — the menu is appended directly, not via notification")
	}
	if len(m.renderedLines) == 0 {
		t.Error("expected the provider menu to be appended as a system message")
	}
}

func TestCmdProviderUnknownProviderToggle(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("not-a-real-provider"))
	if m.notification == "" {
		t.Error("expected notification for an unknown provider")
	}
}

func TestCmdProviderLoginEmptyUsage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("login"))
	if m.notification == "" {
		t.Error("expected usage notification for /provider login with no name")
	}
}

func TestCmdProviderLoginUnknownProvider(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("login not-a-real-provider"))
	if m.notification == "" {
		t.Error("expected notification for an unknown provider")
	}
}

func TestCmdProviderLoginNonOAuthProvider(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("login openai"))
	if !strings.Contains(m.notification, "OAuth") {
		t.Errorf("expected a non-OAuth hint mentioning OAuth, got %q", m.notification)
	}
}

func TestCmdProviderLogoutEmptyUsage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("logout"))
	if m.notification == "" {
		t.Error("expected usage notification for /provider logout with no name")
	}
}

func TestCmdProviderLogoutUnknownProvider(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	drainCmd(m.cmdProvider("logout not-a-real-provider"))
	if m.notification == "" {
		t.Error("expected notification for an unknown provider")
	}
}

func TestProviderCompletionsListsCatalogAndSubVerbs(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	groups := m.providerCompletions([]string{"/provider"}, 0)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	var haveOpenAI, haveLogin, haveLogout bool
	for _, mc := range groups[0].Matches {
		switch mc.Word {
		case "openai":
			haveOpenAI = true
		case "login":
			haveLogin = true
		case "logout":
			haveLogout = true
		}
	}
	if !haveOpenAI || !haveLogin || !haveLogout {
		t.Fatalf("expected openai/login/logout in %+v", groups[0].Matches)
	}
}

func TestProviderCompletionsSecondArgAfterLogin(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	groups := m.providerCompletions([]string{"/provider", "login"}, 1)
	if len(groups) != 1 || len(groups[0].Matches) == 0 {
		t.Fatalf("expected provider names after 'login', got %+v", groups)
	}
	for _, mc := range groups[0].Matches {
		if mc.Word == "login" || mc.Word == "logout" {
			t.Fatalf("sub-verbs should not reappear as an argument, got %+v", groups[0].Matches)
		}
	}
}

func TestProviderCompletionsSecondArgAfterPlainName(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	if groups := m.providerCompletions([]string{"/provider", "openai"}, 1); groups != nil {
		t.Fatalf("expected nil for a second argument after a bare provider name, got %v", groups)
	}
}

// --- refreshAfterProviderChange / providerToggleResultMsg ------------------

func TestRefreshAfterProviderChangeNoRefresher(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.refresh = nil

	cmd := m.refreshAfterProviderChange("OpenRouter", true, "")
	if cmd != nil {
		t.Fatal("expected nil Cmd when no refresher is configured")
	}
	if !strings.Contains(strings.Join(m.renderedLines, "\n"), "OpenRouter enabled") {
		t.Fatalf("expected an 'OpenRouter enabled' line, got %v", m.renderedLines)
	}
}

func TestProviderToggleResultMsgCombinesOutcomeAndCount(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.Update(providerToggleResultMsg{
		displayName: "OpenRouter",
		action:      "enabled",
		models:      []tauchat.ChatModelRef{{ID: "a"}, {ID: "b"}},
	})

	if len(m.availableModels) != 2 {
		t.Fatalf("availableModels = %d, want 2", len(m.availableModels))
	}
	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "OpenRouter enabled, models available: 2") {
		t.Fatalf("expected combined outcome+count line, got %q", joined)
	}
}

func TestProviderToggleResultMsgWithWarning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.Update(providerToggleResultMsg{
		displayName: "OpenRouter",
		action:      "enabled",
		warning:     "no API key found — set $OPENROUTER_API_KEY",
		models:      nil,
	})

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "no API key found") {
		t.Fatalf("expected the warning in the rendered line, got %q", joined)
	}
}

func TestProviderToggleResultMsgRefreshFailed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.Update(providerToggleResultMsg{
		displayName: "OpenRouter",
		action:      "disabled",
		err:         errIntentional,
	})

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "OpenRouter disabled") || !strings.Contains(joined, "model refresh failed") {
		t.Fatalf("expected a disabled+refresh-failed line, got %q", joined)
	}
}

// --- edge: empty slash table dispatch ---------------------------------------

func TestHandleSlashCommandEmptyText(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.handleSlashCommand("")
	if cmd != nil {
		t.Fatal("expected nil Cmd for empty text")
	}
}
