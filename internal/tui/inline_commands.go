package tui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// slashCommand is one entry in the inline chat's command table. The table is the
// single source of truth for dispatch (run), completion (name + complete), and
// the /help listing - they previously drifted as three separate lists.
type slashCommand struct {
	name        string
	aliases     []string
	usage       string // argument hint shown in /help, e.g. "<id>" or "[on|off]"
	description string
	run         func(c *inlineChat, args string)
	complete    completeFunc // nil → command takes no completable arguments
	// isAgent marks a command generated from a built-in agent definition
	// (internal/agent/spec), so the completion dropdown can list it under its
	// own "Agents" group instead of lumping it in with core commands.
	isAgent bool
	// mode is the input-mode indicator (name + color) shown on the
	// surrounding dividers and the leading "/" while this command is being
	// typed - see currentInputMode in inline_chat.go. Only isAgent commands
	// get one (populated in agentSlashCommands); nil for plain commands,
	// which are one-shot actions rather than an operating-mode change.
	mode *taui.InputMode
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
			name: "effort", usage: "[level]", description: "set reasoning effort",
			run: (*inlineChat).handleEffortCommand, complete: argEffort,
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
			name: "cost", aliases: []string{"usage"},
			description: "show token usage and cost for this session",
			run:         (*inlineChat).handleCostCommand,
		},
		{
			name: "copy", description: "copy the assistant's last response to the clipboard (Ctrl+Shift+G)",
			run: (*inlineChat).handleCopyCommand,
		},
		{
			name: "provider", usage: "[<name>|login <name>|logout <name>]",
			description: "toggle a provider on/off, or manage its OAuth login",
			run:         (*inlineChat).handleProviderCommand, complete: argProvider,
		},
		{
			name: "reload", description: "reload extensions",
			run: func(c *inlineChat, _ string) {
				c.send(tauchat.ReloadExtensionsCommand{RequestedAt: time.Now().UTC()})
			},
		},
		{
			name: "clear", aliases: []string{"new", "reset"}, description: "Start a new session",
			run: (*inlineChat).cmdClear,
		},
		{
			name: "help", aliases: []string{"?"}, description: "show available commands",
			run: func(c *inlineChat, _ string) { c.printHelp() },
		},
		{
			name: "skills", usage: "list", description: "list available skills",
			run: (*inlineChat).handleSkillsCommand,
		},
		{
			name: "exit", aliases: []string{"quit", "q"}, description: "quit",
			// Stop in a separate goroutine so Terminal.Stop() → wg.Wait()
			// is not called from the stdin goroutine (tracked by the same
			// WaitGroup). See renderer.go:dispatchKey for the Ctrl+C caller.
			run: func(c *inlineChat, _ string) { go c.engine.Stop() },
		},
	}
	slashCommands = append(slashCommands, agentSlashCommands()...)

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
		// Resolve a sub-action: for a command group (one with subcommands), the
		// first token of the args is the sub-action, and the host receives the
		// full space-joined path as the command name, e.g. "mcp list".
		cmdName := ext.Name
		args := rest
		if len(ext.Subcommands) > 0 && rest != "" {
			sub, remainder, _ := strings.Cut(rest, " ")
			cmdName = ext.Name + " " + sub
			args = strings.TrimSpace(remainder)
		}
		c.send(tauchat.RunExtensionCommandCommand{Name: cmdName, Args: args, RequestedAt: time.Now().UTC()})
		return
	}

	// /skills-reload - hot-reload skills from disk
	if name == "skills-reload" {
		c.send(tauchat.ReloadSkillsCommand{RequestedAt: time.Now().UTC()})
		return
	}

	// /skills - list available skills
	if name == "skills" {
		c.send(tauchat.ListSkillsCommand{RequestedAt: time.Now().UTC()})
		return
	}

	// /skill:<name> - user-invoked skill activation. The coordinator activates
	// the skill, injects its instructions, and emits the lilac "loaded" box.
	if after, ok0 := strings.CutPrefix(name, "skill:"); ok0 {
		skillName := strings.TrimSpace(after)
		if skillName == "" {
			c.engine.PrintAbove("%s %s", c.grey("✗"), "missing skill name")
			return
		}
		c.send(tauchat.RunSkillCommand{SessionID: c.sid(), SkillName: skillName, Args: rest, RequestedAt: time.Now().UTC()})
		return
	}

	c.engine.PrintAbove("%s %s", c.grey("✗"), "unknown command: /"+name)
}

func (c *inlineChat) cmdClear(_ string) {
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
	fmt.Fprintf(&b, "\n  %-38s %s", "Ctrl+Shift+G", "copy the assistant's last response (same as /copy)")
	c.engine.PrintAbove("%s", c.grey(b.String()))
}

// agentSlashCommands builds slash-command table entries for tau's built-in
// agent definitions (internal/agent/spec), skipping any marked
// user-invocable: false. Each dispatches through runAgentCommand so the
// definitions in spec/templates/*.agent.md are the single source of truth
// for name, description, and argument hint - this table just wires them up.
func agentSlashCommands() []slashCommand {
	defs, err := agentspec.Builtins()
	if err != nil {
		defs = nil
	}
	builtinNames := make(map[string]bool, len(defs))
	cmds := make([]slashCommand, 0, len(defs))
	for _, def := range defs {
		builtinNames[def.Name] = true
		if !def.UserInvocable {
			continue
		}
		cmds = append(cmds, buildAgentSlashCommand(def.Name, def))
	}

	// Filesystem-discovered agent definitions (.agents/agents/*.agent.md),
	// listed under a user:/project: prefix exactly like internal/registry
	// does - a name colliding with a built-in is skipped so a discovered
	// definition can never shadow one.
	cwd, _ := os.Getwd()
	discovered, _ := agentspec.DiscoverFromDisk(agentspec.DefaultSources(cwd))
	for _, def := range discovered {
		if builtinNames[def.Name] || !def.UserInvocable {
			continue
		}
		cmds = append(cmds, buildAgentSlashCommand(def.CommandPrefix()+def.Name, def))
	}
	return cmds
}

// buildAgentSlashCommand builds the slash-command table entry for a single
// agent definition, dispatching under invokeName (the bare name for a
// built-in, or a user:/project:-prefixed name for a filesystem-discovered
// one).
func buildAgentSlashCommand(invokeName string, def *agentspec.Definition) slashCommand {
	usage := def.ArgumentHint
	if usage == "" {
		usage = "[prompt]"
	}
	return slashCommand{
		name:        invokeName,
		usage:       usage,
		description: def.Description,
		run: func(c *inlineChat, args string) {
			c.runAgentCommand(invokeName, args)
		},
		isAgent: true,
		mode:    agentInputMode(def),
	}
}

// agentInputMode builds the input-mode indicator for an agent command -
// every agent command gets one, defaulting to the shared agent accent
// (theme.CommandFG) so all of them read as "you're about to enter a mode"
// consistently, even before a template author bothers to pick a specific
// color via the optional "color" frontmatter field (an xterm-256 palette
// index, e.g. "134" for /plan's purple).
func agentInputMode(def *agentspec.Definition) *taui.InputMode {
	mode := &taui.InputMode{Label: def.DisplayName, Color: theme.CommandFG}
	if def.Color == "" {
		return mode
	}
	idx, err := strconv.ParseUint(def.Color, 10, 8)
	if err != nil {
		return mode
	}
	mode.Color = termkit.Xterm256(uint8(idx))
	return mode
}

// runAgentCommand activates a built-in agent (e.g. /plan) by name and, when
// args are non-empty, submits them as the turn's prompt via the same
// queueing path as a normal submit - so it interleaves correctly with an
// already-running turn instead of racing the coordinator's own turn loop.
func (c *inlineChat) runAgentCommand(name, args string) {
	c.send(tauchat.RunAgentCommand{
		SessionID:   c.sid(),
		Name:        name,
		RequestedAt: time.Now().UTC(),
	})

	args = strings.TrimSpace(args)
	if args == "" {
		return
	}
	c.startOrQueueTurn(args)
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

// effortLabel returns a display label for a reasoning effort level.
// Generic - no hardcoded map, no stale token estimates.
func effortLabel(level string) string {
	return "effort: " + level
}

// currentModelRef returns the selected model's ref from the available list, or
// a zero ref when nothing is selected or it isn't in the list.
func (c *inlineChat) currentModelRef() tauchat.ChatModelRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.availableModels {
		if m.ID == c.modelName {
			return m
		}
	}
	return tauchat.ChatModelRef{}
}

// effortLevels returns the cycle of reasoning-effort levels for a model:
// the model's advertised reasoning_options followed by "auto" (provider
// default). When the model advertises none, only "auto" is offered.
func effortLevels(model tauchat.ChatModelRef) []string {
	if len(model.Config.ReasoningEfforts) > 0 {
		out := make([]string, 0, len(model.Config.ReasoningEfforts)+1)
		out = append(out, model.Config.ReasoningEfforts...)
		return append(out, "auto")
	}
	return []string{"auto"}
}

func nextEffort(current string, levels []string) string {
	for i, l := range levels {
		if l == current {
			return levels[(i+1)%len(levels)]
		}
	}
	if len(levels) > 1 {
		return levels[0] // first real level ("auto" is always at the back)
	}
	return levels[0]
}

// defaultEffortForModel returns the default effort level for a model.
// "medium" for models that support effort selection, "" otherwise.
func defaultEffortForModel(model tauchat.ChatModelRef) string {
	if len(model.Config.ReasoningEfforts) > 0 {
		if slices.Contains(model.Config.ReasoningEfforts, "medium") {
			return "medium"
		}
		return model.Config.ReasoningEfforts[0]
	}
	return ""
}

func (c *inlineChat) handleEffortCommand(args string) {
	model := c.currentModelRef()
	cfg := model.Config

	// Gate on capability: when we have real metadata for the model and it is not
	// a reasoning model, there is no effort to set. Models we have no metadata
	// for (e.g. live Ollama models) stay permissive.
	hasMetadata := cfg.ContextWindow > 0 || cfg.Reasoning || len(cfg.ReasoningEfforts) > 0
	isReasoning := cfg.Reasoning || len(cfg.ReasoningEfforts) > 0
	if model.ID != "" && hasMetadata && !isReasoning {
		c.pushNotice(model.ID + " doesn't support reasoning")
		return
	}

	levels := effortLevels(model)

	arg := strings.ToLower(strings.TrimSpace(args))
	if arg == "med" {
		arg = "medium"
	}

	c.mu.Lock()
	var effort string
	switch {
	case arg == "":
		effort = nextEffort(c.reasoningEffort, levels)
	case slices.Contains(levels, arg):
		effort = arg
	default:
		c.mu.Unlock()
		c.pushNotice(fmt.Sprintf("%s supports: %s", model.ID, strings.Join(levels, ", ")))
		return
	}
	c.reasoningEffort = effort
	c.mu.Unlock()

	c.pushNotice(effortLabel(effort))
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
			c.pushNotice("no models available - try /refresh")
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
	effort := defaultEffortForModel(model)
	patch := tauchat.ChatSessionPatch{Model: &model, ReasoningEffort: &effort}
	// When the model carries a provider tag (aggregated cross-provider list),
	// switch the session's provider too so the dynamic streamer routes to it.
	if model.Provider != "" {
		provider := model.Provider
		patch.Provider = &provider
	}
	c.send(tauchat.UpdateChatSessionCommand{
		SessionID: c.sid(),
		Patch:     patch,
	})
	c.mu.Lock()
	c.modelName = model.ID
	c.reasoningEffort = effort
	if model.Provider != "" {
		c.provider = model.Provider
	}
	c.mu.Unlock()
	notice := "model: " + model.ID
	if model.Provider != "" {
		notice += " (" + model.Provider + ")"
	}
	c.pushNotice(notice)
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

// handleSkillsCommand requests and displays the list of available skills.
func (c *inlineChat) handleSkillsCommand(args string) {
	args = strings.TrimSpace(args)
	if args == "" || args == "list" {
		c.send(tauchat.ListSkillsCommand{RequestedAt: time.Now().UTC()})
		return
	}
	c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /skills list")
}

// handleCostCommand prints a tidy breakdown of token usage and cost for the
// current session, mirroring printHelp's grey, aligned layout.
func (c *inlineChat) handleCostCommand(_ string) {
	if c.usage == nil {
		c.pushNotice("no usage recorded yet")
		return
	}
	totals := c.usage.Snapshot(c.sid())
	if totals == nil || totals.TotalTokens == 0 {
		c.pushNotice("no usage recorded yet")
		return
	}

	ctxWindow := c.currentModelRef().Config.ContextWindow

	var b strings.Builder
	b.WriteString("Usage:")
	fmt.Fprintf(&b, "\n  %-12s %s", "tokens",
		fmt.Sprintf("↑%d in · ↓%d out · %d total", totals.PromptTokens, totals.CompletionTokens, totals.TotalTokens))
	fmt.Fprintf(&b, "\n  %-12s %d", "turns", totals.TurnCount)
	fmt.Fprintf(&b, "\n  %-12s %s", "cost", formatCost(totals.Cost))
	fmt.Fprintf(&b, "\n  %-12s %s", "tools",
		fmt.Sprintf("%d calls · %d errors", totals.ToolCalls, totals.ToolErrors))
	if pct := formatContextPct(totals.LastPromptTokens, ctxWindow); pct != "" {
		fmt.Fprintf(&b, "\n  %-12s %s", "context",
			fmt.Sprintf("%s (%d / %d)", pct, totals.LastPromptTokens, ctxWindow))
	}
	if totals.LastModel != "" {
		model := totals.LastModel
		if totals.LastProvider != "" {
			model += " (" + totals.LastProvider + ")"
		}
		fmt.Fprintf(&b, "\n  %-12s %s", "model", model)
	}
	c.engine.PrintAbove("%s", c.grey(b.String()))
}

// handleCopyCommand copies the assistant's last completed response to the
// system clipboard via OSC 52 (termkit.OSC52Copy) rather than shelling out to
// a clipboard utility, so it works over SSH and inside tmux where no local
// clipboard binary is reachable. Also bound to Ctrl+Shift+G - see
// inline_chat.go's inlineCtrl.HandleInput.
func (c *inlineChat) handleCopyCommand(_ string) {
	c.mu.Lock()
	text := c.lastResponseText
	c.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		c.pushNotice("nothing to copy yet")
		return
	}
	seq, ok := termkit.OSC52Copy(text)
	if !ok {
		c.pushNotice(fmt.Sprintf("response too large to copy (over %d chars)", termkit.OSC52MaxBytes))
		return
	}
	c.engine.Terminal.Write(seq)
	c.pushNotice("copied last response to clipboard")
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
		// Prefill "/resume " so the completion dropdown shows saved sessions
		// for arrow-key selection, matching the /model UX.
		c.mu.Lock()
		n := len(c.sessionSummaries)
		c.mu.Unlock()
		if n == 0 {
			c.pushNotice("no saved sessions - try /session list first")
			return
		}
		c.input.SetValueAndCursor("/resume ", len([]rune("/resume ")))
		c.engine.RequestRender()
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
