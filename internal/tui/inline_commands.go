package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/taui"
)

// slashCommand is one entry in the inline chat's command table. The table is the
// single source of truth for dispatch (run), completion (name + complete), and
// the /help listing — they previously drifted as three separate lists.
type slashCommand struct {
	name        string
	aliases     []string
	usage       string // argument hint shown in /help, e.g. "<id>" or "[on|off]"
	description string
	run         func(c *inlineChat, args string)
	complete    completeFunc // nil → command takes no completable arguments
}

// completeFunc returns argument completions for a command. fields are the
// whitespace-split tokens of the input (including the command), and argsBefore
// is the number of fully-typed arguments before the cursor.
type completeFunc func(c *inlineChat, fields []string, argsBefore int) (title string, matches []taui.Match)

// slashCommands and slashByName are populated in init rather than as variable
// initializers: the help command's run closure references printHelp, which
// ranges over slashCommands, and Go rejects that as a package-var init cycle
// even though nothing is evaluated at init time. Assigning inside init sidesteps
// the (spurious) cycle.
var (
	slashCommands []slashCommand
	slashByName   map[string]*slashCommand
)

func init() {
	slashCommands = []slashCommand{
		{
			name: "model", usage: "<id>", description: "switch model",
			run: (*inlineChat).handleModelCommand, complete: argModels,
		},
		{
			name: "system", usage: "<prompt>", description: "set the system prompt",
			run: (*inlineChat).handleSystemCommand,
		},
		{
			name: "reasoning", usage: "[on|off]", description: "toggle reasoning display",
			run: (*inlineChat).handleReasoningCommand, complete: firstArg("Reasoning", reasoningMatches),
		},
		{
			name: "effort", usage: "[level]", description: "set reasoning effort (off/low/medium/high/max)",
			run: (*inlineChat).handleEffortCommand, complete: firstArg("Effort", effortMatches),
		},
		{
			name: "session", usage: "[list|info|export|delete|<id>]", description: "manage saved sessions",
			run: (*inlineChat).handleSessionCommand, complete: argSession,
		},
		{
			name: "resume", usage: "[id]", description: "resume a saved session",
			run: (*inlineChat).handleResumeCommand, complete: argSessions,
		},
		{
			name: "refresh", description: "re-discover available models",
			run: func(c *inlineChat, _ string) { c.refreshModels() },
		},
		{
			name: "reload", description: "reload extensions",
			run: func(c *inlineChat, _ string) {
				c.send(tauchat.ReloadExtensionsCommand{RequestedAt: time.Now().UTC()})
			},
		},
		{
			name: "new", aliases: []string{"clear", "reset"}, description: "start a fresh conversation",
			run: (*inlineChat).cmdNew,
		},
		{
			name: "help", aliases: []string{"?"}, description: "show available commands",
			run: func(c *inlineChat, _ string) { c.printHelp() },
		},
		{
			name: "exit", aliases: []string{"quit", "q"}, description: "quit",
			run: func(c *inlineChat, _ string) { c.engine.Stop() },
		},
	}

	// Index by primary name and alias for O(1) dispatch.
	slashByName = make(map[string]*slashCommand, len(slashCommands)*2)
	for i := range slashCommands {
		cmd := &slashCommands[i]
		slashByName[cmd.name] = cmd
		for _, a := range cmd.aliases {
			slashByName[a] = cmd
		}
	}
}

// handleSlashCommand dispatches a "/command args" line via the table, falling
// back to extension commands, then an unknown-command notice.
func (c *inlineChat) handleSlashCommand(text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	rest := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))

	if cmd, ok := slashByName[name]; ok {
		cmd.run(c, rest)
		return
	}

	c.mu.Lock()
	ext, ok := c.extensionCommands[name]
	c.mu.Unlock()
	if ok {
		c.send(tauchat.RunExtensionCommandCommand{Name: ext.Name, Args: rest, RequestedAt: time.Now().UTC()})
		return
	}
	c.engine.PrintAbove("%s %s", c.grey("✗"), "unknown command: /"+name)
}

func (c *inlineChat) cmdNew(_ string) {
	c.send(tauchat.ResetChatSessionCommand{SessionID: c.sid(), RequestedAt: time.Now().UTC()})
	c.engine.Update(c.clearTurnLocked)
	c.engine.PrintAbove("%s", c.grey("conversation cleared"))
	c.pushNotice("conversation cleared")
}

// printHelp renders the command listing from the table so it never drifts from
// what actually dispatches.
func (c *inlineChat) printHelp() {
	var b strings.Builder
	b.WriteString("Commands:")
	for i := range slashCommands {
		cmd := &slashCommands[i]
		label := "/" + cmd.name
		if cmd.usage != "" {
			label += " " + cmd.usage
		}
		fmt.Fprintf(&b, "\n  %-38s %s", label, cmd.description)
	}
	fmt.Fprintf(&b, "\n  %-38s %s", "Ctrl+S", "steer the agent mid-turn")
	c.engine.PrintAbove("%s", c.grey(b.String()))
}

// ── Command argument handlers ────────────────────────────────────────────────
// These are the table's run targets (referenced as method expressions above).

func (c *inlineChat) handleReasoningCommand(args string) {
	args = strings.TrimSpace(args)
	c.mu.Lock()
	switch strings.ToLower(args) {
	case "":
		c.showReasoning = !c.showReasoning
	case "on", "true", "yes":
		c.showReasoning = true
	case "off", "false", "no", "auto":
		c.showReasoning = false
	default:
		c.showReasoning = !c.showReasoning
	}
	on := c.showReasoning
	c.mu.Unlock()
	if on {
		c.pushNotice("reasoning: on")
	} else {
		c.pushNotice("reasoning: off")
	}
}

// reasoningEffortLevels cycle in order.
var reasoningEffortLevels = []string{"off", "low", "medium", "high", "max"}

// effortDisplay maps effort levels to their display labels.
var effortDisplay = map[string]string{
	"off":    "effort: off",
	"low":    "effort: low (~512 tok)",
	"medium": "effort: medium (~2k tok)",
	"high":   "effort: high (~8k tok)",
	"max":    "effort: max (unlimited)",
}

func (c *inlineChat) handleEffortCommand(args string) {
	c.mu.Lock()
	effort := c.reasoningEffort
	set := false
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "off", "none":
		effort, set = "off", true
	case "low":
		effort, set = "low", true
	case "med", "medium":
		effort, set = "medium", true
	case "high":
		effort, set = "high", true
	case "max":
		effort, set = "max", true
	}
	if !set {
		next := reasoningEffortLevels[1] // default to low if unrecognised
		for i, level := range reasoningEffortLevels {
			if level == effort {
				next = reasoningEffortLevels[(i+1)%len(reasoningEffortLevels)]
				break
			}
		}
		effort = next
	}
	c.reasoningEffort = effort
	c.mu.Unlock()

	if label, ok := effortDisplay[effort]; ok {
		c.pushNotice(label)
	}
	c.send(tauchat.UpdateChatSessionCommand{
		SessionID: c.sid(),
		Patch:     tauchat.ChatSessionPatch{ReasoningEffort: &effort},
	})
}

func (c *inlineChat) handleModelCommand(modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		// Open the model picker: prefill "/model " so the completion dropdown
		// lists the available models for arrow-key selection.
		c.mu.Lock()
		n := len(c.availableModels)
		c.mu.Unlock()
		if n == 0 {
			c.pushNotice("no models available — try /refresh")
			return
		}
		c.input.SetValueAndCursor("/model ", len([]rune("/model ")))
		c.engine.RequestRender()
		return
	}
	c.mu.Lock()
	var model tauchat.ChatModelRef
	found := false
	for _, m := range c.availableModels {
		if m.ID == modelID {
			model, found = m, true
			break
		}
	}
	c.mu.Unlock()
	if !found {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("model %q is not in the available model list", modelID))
		return
	}
	c.send(tauchat.UpdateChatSessionCommand{
		SessionID: c.sid(),
		Patch:     tauchat.ChatSessionPatch{Model: &model},
	})
	c.mu.Lock()
	c.modelName = model.ID
	c.mu.Unlock()
	c.pushNotice("model: " + model.ID)
}

func (c *inlineChat) handleSystemCommand(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /system <prompt>")
		return
	}
	c.send(tauchat.UpdateChatSessionCommand{
		SessionID: c.sid(),
		Patch:     tauchat.ChatSessionPatch{SystemPrompt: &prompt},
	})
	c.pushNotice("system prompt updated")
}

func (c *inlineChat) refreshModels() {
	if c.refresh == nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "model refresh is not available")
		return
	}
	c.pushNotice("refreshing models…")
	go func() {
		models, err := c.refresh(c.ctx)
		if err != nil {
			c.engine.PrintAbove("%s %s", c.grey("✗"), "model refresh failed: "+err.Error())
			return
		}
		c.mu.Lock()
		c.availableModels = slices.Clone(models)
		c.mu.Unlock()
		c.pushNotice(fmt.Sprintf("refreshed models: %d available", len(models)))
		c.engine.RequestRender()
	}()
}

// ── Session management ────────────────────────────────────────────────────────

func (c *inlineChat) handleSessionCommand(rest string) {
	parts := strings.Fields(strings.TrimSpace(rest))
	if len(parts) == 0 {
		c.listSessions()
		return
	}
	switch strings.ToLower(parts[0]) {
	case "list":
		c.listSessions()
	case "info":
		c.handleSessionInfo(parts)
	case "export":
		c.handleSessionExport(parts)
	case "delete":
		c.handleSessionDelete(parts)
	default:
		c.loadSession(parts[0])
	}
}

func (c *inlineChat) listSessions() {
	c.send(tauchat.ListSessionsCommand{Limit: 10})
}

func (c *inlineChat) handleResumeCommand(rest string) {
	id := strings.TrimSpace(rest)
	if id == "" {
		c.listSessions()
		return
	}
	c.loadSession(id)
}

func (c *inlineChat) loadSession(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.send(tauchat.LoadSessionCommand{SessionID: id, RuntimeSessionID: c.sid()})
}

func (c *inlineChat) handleSessionInfo(parts []string) {
	if len(parts) < 2 {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /session info <id>")
		return
	}
	id := parts[1]
	c.mu.Lock()
	summaries := c.sessionSummaries
	c.mu.Unlock()
	for _, s := range summaries {
		if s.ID == id {
			c.printSessionInfo(s)
			return
		}
	}
	c.engine.PrintAbove("%s %s", c.grey("✗"), "session not found: "+id+" (try /session list first)")
}

func (c *inlineChat) handleSessionExport(parts []string) {
	if len(parts) < 2 {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /session export <id> [path]")
		return
	}
	output := ""
	if len(parts) >= 3 {
		output = parts[2]
	}
	c.send(tauchat.ExportSessionCommand{SessionID: parts[1], Format: "jsonl", Output: output})
}

func (c *inlineChat) handleSessionDelete(parts []string) {
	if len(parts) < 2 {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /session delete <id>")
		return
	}
	c.send(tauchat.DeleteSessionCommand{SessionID: parts[1]})
}

// ── Argument completers ──────────────────────────────────────────────────────

// firstArg builds a completer offering a static match list for the first
// argument only (nothing once that argument is complete, so the line submits).
func firstArg(title string, matches []taui.Match) completeFunc {
	return func(_ *inlineChat, _ []string, argsBefore int) (string, []taui.Match) {
		if argsBefore == 0 {
			return title, matches
		}
		return "", nil
	}
}

func argModels(c *inlineChat, _ []string, argsBefore int) (string, []taui.Match) {
	if argsBefore == 0 {
		return "Models", c.modelMatches()
	}
	return "", nil
}

func argSessions(c *inlineChat, _ []string, argsBefore int) (string, []taui.Match) {
	if argsBefore == 0 {
		return "Sessions", c.sessionMatches()
	}
	return "", nil
}

func argSession(c *inlineChat, fields []string, argsBefore int) (string, []taui.Match) {
	return c.sessionArgs(fields, argsBefore)
}
