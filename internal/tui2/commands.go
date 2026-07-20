package tui2

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
)

// --- command table ---------------------------------------------------------

// slashEntry is one row in the tui2 command table. It mirrors the legacy
// slashCommand type but handlers return tea.Cmd instead of mutating directly.
type slashEntry struct {
	name        string
	aliases     []string
	usage       string
	displayName string
	description string
	isAgent     bool
	modeSwitch  bool
	run         func(m *model, args string) tea.Cmd
}

// buildAgentSlashEntry builds the slash-command table entry for a single
// agent definition, dispatching under invokeName (the bare name for a
// built-in, or a user:/project:-prefixed name for a filesystem-discovered
// one).
func buildAgentSlashEntry(invokeName string, def *agentspec.Definition) slashEntry {
	usage := def.ArgumentHint
	if usage == "" {
		usage = "[prompt]"
	}
	return slashEntry{
		name:        invokeName,
		usage:       usage,
		displayName: def.DisplayName,
		description: def.Description,
		isAgent:     true,
		modeSwitch:  def.ModeSwitcher,
		run: func(m *model, args string) tea.Cmd {
			return m.runAgentCommand(invokeName, args)
		},
	}
}

func (e slashEntry) modeLabel() string {
	if e.displayName != "" {
		return e.displayName
	}
	if e.name == "" {
		return "agent"
	}
	return strings.ToUpper(e.name[:1]) + e.name[1:]
}

type inputMode struct {
	command string
}

func inputModes() []inputMode {
	modes := []inputMode{{}}
	for i := range slashTable {
		entry := slashTable[i]
		if !entry.isAgent || !entry.modeSwitch {
			continue
		}
		modes = append(modes, inputMode{
			command: entry.name,
		})
	}
	return modes
}

func slashNameAndArgs(text string) (name, args string) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if text == "" {
		return "", ""
	}
	name, args, _ = strings.Cut(text, " ")
	return strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(args)
}

// slashCommandsB is the tui2 command table, populated in init().
var slashTable []slashEntry

// slashIndex maps primary name and aliases to their entry.
var slashIndex map[string]*slashEntry

func init() {
	slashTable = []slashEntry{
		{
			name: "model", usage: "<id>", description: "switch model",
			run: (*model).cmdModel,
		},
		{
			name: "system", usage: "<prompt>", description: "set the system prompt",
			run: (*model).cmdSystem,
		},
		{
			name: "reasoning", usage: "[on|off]", description: "toggle reasoning display",
			run: (*model).cmdReasoning,
		},
		{
			name: "effort", usage: "[level]", description: "set reasoning effort",
			run: (*model).cmdEffort,
		},
		{
			name: "session", usage: "[list|info|export|delete|<id>]", description: "manage saved sessions",
			run: (*model).cmdSession,
		},
		{
			name: "resume", usage: "[id]", description: "resume a saved session",
			run: (*model).cmdResume,
		},
		{
			name: "refresh", description: "re-discover available models",
			run: (*model).cmdRefresh,
		},
		{
			name: "cost", aliases: []string{"usage"},
			description: "show token usage and cost for this session",
			run:         (*model).cmdCost,
		},
		{
			name: "copy", usage: "[session|all|<N>]", description: "copy last response or transcript to clipboard",
			run: (*model).cmdCopy,
		},
		{
			name: "provider", usage: "[<name>|login <name>|logout <name>]",
			description: "toggle a provider on/off, or manage its OAuth login",
			run:         (*model).cmdProvider,
		},
		{
			name: "reload", description: "reload extensions",
			run: func(m *model, _ string) tea.Cmd {
				return sendCommand(m.runtime, tauchat.ReloadExtensionsCommand{
					RequestedAt: time.Now().UTC(),
				})
			},
		},
		{
			name: "clear", aliases: []string{"new", "reset"}, description: "start a new session",
			run: (*model).cmdClear,
		},
		{
			name: "help", aliases: []string{"?"}, description: "show available commands",
			run: (*model).cmdHelp,
		},
		{
			name: "skills", usage: "list", description: "list available skills",
			run: (*model).cmdSkills,
		},
		{
			name: "exit", aliases: []string{"quit", "q"}, description: "quit",
			run: func(m *model, _ string) tea.Cmd {
				return tea.Quit
			},
		},
	}

	// Append agent commands from spec (dynamic discovery).
	defs, err := agentspec.Builtins()
	if err != nil {
		defs = nil
	}
	builtinAgentNames := make(map[string]bool, len(defs))
	for _, def := range defs {
		builtinAgentNames[def.Name] = true
		if !def.UserInvocable {
			continue
		}
		slashTable = append(slashTable, buildAgentSlashEntry(def.Name, def))
	}

	// Filesystem-discovered agent definitions (.agents/agents/*.agent.md),
	// listed under a user:/project: prefix exactly like internal/registry
	// does - a name colliding with a built-in is skipped so a discovered
	// definition can never shadow one.
	cwd, _ := os.Getwd()
	discovered, _ := agentspec.DiscoverFromDisk(agentspec.DefaultSources(cwd))
	for _, def := range discovered {
		if builtinAgentNames[def.Name] || !def.UserInvocable {
			continue
		}
		slashTable = append(slashTable, buildAgentSlashEntry(def.CommandPrefix()+def.Name, def))
	}

	slashIndex = make(map[string]*slashEntry, len(slashTable)*2)
	for i := range slashTable {
		e := &slashTable[i]
		slashIndex[e.name] = e
		for _, a := range e.aliases {
			slashIndex[a] = e
		}
	}
}

// --- dispatch --------------------------------------------------------------

// handleSlashCommand dispatches a "/command args" line. Returns a Cmd to
// send to the coordinator (handlers must never call runtime.Send directly).
func (m *model) handleSlashCommand(text string) tea.Cmd {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	rest := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))

	if entry, ok := slashIndex[name]; ok {
		return entry.run(m, rest)
	}

	// Extension commands.
	if ext, ok := m.extensionCommands[name]; ok {
		cmdName := ext.Name
		args := rest
		if len(ext.Subcommands) > 0 && rest != "" {
			sub, remainder, _ := strings.Cut(rest, " ")
			cmdName = ext.Name + " " + sub
			args = strings.TrimSpace(remainder)
		}
		return sendCommand(m.runtime, tauchat.RunExtensionCommandCommand{
			Name:        cmdName,
			Args:        args,
			RequestedAt: time.Now().UTC(),
		})
	}

	// /skills-reload - hot-reload skills from disk.
	if name == "skills-reload" {
		return sendCommand(m.runtime, tauchat.ReloadSkillsCommand{
			RequestedAt: time.Now().UTC(),
		})
	}

	// /skill:<name> - user-invoked skill activation.
	if after, ok := strings.CutPrefix(name, "skill:"); ok {
		skillName := strings.TrimSpace(after)
		if skillName == "" {
			return m.setNotification("missing skill name")
		}
		return sendCommand(m.runtime, tauchat.RunSkillCommand{
			SessionID:   m.sessionID,
			SkillName:   skillName,
			Args:        rest,
			RequestedAt: time.Now().UTC(),
		})
	}

	return m.setNotification("unknown command: /" + name)
}

// --- command handlers ------------------------------------------------------

func (m *model) cmdClear(_ string) tea.Cmd {
	return tea.Batch(
		sendCommand(m.runtime, tauchat.ResetChatSessionCommand{
			SessionID:   m.sessionID,
			RequestedAt: time.Now().UTC(),
		}),
		m.setNotification("conversation cleared"),
	)
}

// cmdHelp opens the keybinding reference as a floating overlay (see
// help.go), centered on top of the current view rather than appended to
// scrollback - a reference card isn't conversation history, and baking it
// into renderedLines just left a permanent, ever-growing block behind every
// time it was opened. It closes on any keypress or a click outside it (see
// handleHelpOverlayKey/handleHelpOverlayClick), and clicking a row inside it
// expands that row's description in place, same as before. Slash commands
// live in the "/" completions dropdown and are deliberately not repeated
// here. Opening it also closes any other open exclusive overlay, matching
// openDiffViewer's same "opening a modal always wins" convention.
func (m *model) cmdHelp(_ string) tea.Cmd {
	m.closeOtherExclusiveOverlays(overlayHelp)
	m.helpOverlay = &helpOverlayState{expanded: map[helpRowKey]bool{}}
	return nil
}

func (m *model) cmdReasoning(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "":
		m.showReasoning = !m.showReasoning
	case "on", "true", "yes":
		m.showReasoning = true
	case "off", "false", "no", "auto":
		m.showReasoning = false
	default:
		m.showReasoning = !m.showReasoning
	}
	if m.showReasoning {
		return m.setNotification("reasoning: on")
	}
	return m.setNotification("reasoning: off")
}

func (m *model) cmdEffort(args string) tea.Cmd {
	model := m.currentModelRef()
	levels := effortLevels(model)

	arg := strings.ToLower(strings.TrimSpace(args))
	if arg == "med" {
		arg = "medium"
	}

	var effort string
	switch {
	case arg == "":
		effort = nextEffort(m.reasoningEffort, levels)
	case slices.Contains(levels, arg):
		effort = arg
	default:
		return m.setNotification(fmt.Sprintf("%s supports: %s", model.ID, strings.Join(levels, ", ")))
	}
	m.reasoningEffort = effort

	return tea.Batch(
		m.setNotification("effort: "+effort),
		sendCommand(m.runtime, tauchat.UpdateChatSessionCommand{
			SessionID: m.sessionID,
			Patch:     tauchat.ChatSessionPatch{ReasoningEffort: &effort},
		}),
	)
}

func (m *model) cmdModel(modelID string) tea.Cmd {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return m.openModelPalette()
	}

	var ref tauchat.ChatModelRef
	found := false
	for _, r := range m.availableModels {
		if r.ID == modelID {
			ref, found = r, true
			break
		}
	}
	if !found {
		return m.setNotification(fmt.Sprintf("model %q not found - try /refresh", modelID))
	}

	effort := defaultEffortForModel(ref)
	m.modelName = ref.ID
	m.reasoningEffort = effort
	patch := tauchat.ChatSessionPatch{Model: &ref, ReasoningEffort: &effort}
	if ref.Provider != "" {
		m.provider = ref.Provider
		patch.Provider = &m.provider
	}
	notice := "model: " + ref.ID
	if ref.Provider != "" {
		notice += " (" + ref.Provider + ")"
	}

	return tea.Batch(
		m.setNotification(notice),
		sendCommand(m.runtime, tauchat.UpdateChatSessionCommand{
			SessionID: m.sessionID,
			Patch:     patch,
		}),
	)
}

func (m *model) cmdSystem(prompt string) tea.Cmd {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return m.setNotification("usage: /system <prompt>")
	}
	return tea.Batch(
		m.setNotification("system prompt updated"),
		sendCommand(m.runtime, tauchat.UpdateChatSessionCommand{
			SessionID: m.sessionID,
			Patch:     tauchat.ChatSessionPatch{SystemPrompt: &prompt},
		}),
	)
}

func (m *model) cmdRefresh(_ string) tea.Cmd {
	if m.refresh == nil {
		return m.setNotification("model refresh not available")
	}
	return func() tea.Msg {
		models, err := m.refresh(m.ctx)
		if err != nil {
			return refreshResultMsg{err: err}
		}
		return refreshResultMsg{models: models}
	}
}

// cmdCost prints a full usage breakdown (tokens, turns, cost, tools, context
// %, model) to the scrollback. Mirrors
// internal/tui/inline_commands.go's handleCostCommand.
func (m *model) cmdCost(_ string) tea.Cmd {
	if m.usage == nil {
		return m.setNotification("no usage recorded yet")
	}
	totals := m.usage.Snapshot(m.sessionID)
	if totals == nil || totals.TotalTokens == 0 {
		return m.setNotification("no usage recorded yet")
	}

	ctxWindow := m.currentModelRef().Config.ContextWindow

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
	m.appendMessage("system", b.String())
	return nil
}

func (m *model) cmdCopy(args string) tea.Cmd {
	args = strings.TrimSpace(args)

	switch {
	case args == "":
		if m.lastAssistantText == "" {
			return m.setNotification("nothing to copy")
		}
		return tea.Batch(tea.SetClipboard(m.lastAssistantText), m.setNotification("copied to clipboard"))

	case args == "session" || args == "all":
		msgs := m.canonicalMessages
		if len(msgs) == 0 {
			return m.setNotification("nothing to copy")
		}
		text := formatCopyTranscript(msgs)
		return tea.Batch(tea.SetClipboard(text), m.setNotification("session copied to clipboard"))

	default:
		n, err := parseCopyCount(args)
		if err != nil {
			return m.setNotification(err.Error())
		}
		msgs := m.canonicalMessages
		if len(msgs) == 0 {
			return m.setNotification("nothing to copy")
		}
		if n > len(msgs) {
			n = len(msgs)
		}
		if n < 1 {
			n = 1
		}
		text := formatCopyTranscript(msgs[len(msgs)-n:])
		return tea.Batch(tea.SetClipboard(text), m.setNotification(fmt.Sprintf("%d messages copied to clipboard", n)))
	}
}

// parseCopyCount parses the /copy <N> argument as a positive integer.
func parseCopyCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid message count: %q - use a positive number", s)
	}
	return n, nil
}

// formatCopyTranscript formats a slice of canonical (user/assistant) messages
// as a plain-text transcript with role labels, preserving Markdown and code
// fences. No ANSI styling, system prompts, tool protocol, or internal events
// are included.
func formatCopyTranscript(msgs []tauchat.ChatMessage) string {
	var b strings.Builder
	for i, msg := range msgs {
		label := "User"
		if msg.Role == tauchat.ChatRoleAssistant {
			label = "Assistant"
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%s:\n%s", label, msg.Content)
	}
	return b.String()
}

// cmdProvider dispatches /provider's three forms: bare (show the menu), a
// provider name (toggle it on/off), or a "login"/"logout" sub-verb followed
// by a name. There is no catalog provider literally named "login" or
// "logout", so the sub-verb check is unambiguous.
func (m *model) cmdProvider(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" {
		return m.openProviderPalette("")
	}

	fields := strings.Fields(args)
	rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	switch strings.ToLower(fields[0]) {
	case "login":
		if rest == "" {
			return m.openProviderPalette("login")
		}
		return m.providerLogin(rest)
	case "logout":
		if rest == "" {
			return m.openProviderPalette("logout")
		}
		return m.providerLogout(rest)
	default:
		return m.providerToggle(fields[0])
	}
}

// providerToggle enables an API-key/no-auth provider if it's currently off,
// or disables it if it's currently on - the effective on/off state as shown
// in the /provider menu, not just the raw explicit-enable list, so toggling
// an auto-detected (env-key-present) provider off actually takes effect.
// OAuth providers aren't toggleable this way; they need /provider login.
func (m *model) providerToggle(name string) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	entry, ok := providers.Lookup(name)
	if !ok {
		return m.setNotification(fmt.Sprintf("unknown provider %q - see /provider for the list", name))
	}
	if entry.Auth == providers.AuthOAuth {
		return m.setNotification(fmt.Sprintf("%s uses OAuth - use /provider login %s", entry.DisplayName, entry.ID))
	}

	enabled, warning, err := providers.NewManage(nil).Toggle(entry.ID)
	if err != nil {
		return m.setNotification(err.Error())
	}
	return m.refreshAfterProviderChange(entry.DisplayName, enabled, warning)
}

// providerLogin handles /provider login <name> [enterprise-domain].
func (m *model) providerLogin(args string) tea.Cmd {
	fields := strings.Fields(args)
	name := ""
	if len(fields) > 0 {
		name = strings.ToLower(strings.TrimSpace(fields[0]))
	}
	if name == "" {
		return m.setNotification("usage: /provider login <provider>")
	}
	entry, ok := providers.Lookup(name)
	if !ok {
		return m.setNotification(fmt.Sprintf("unknown provider %q", name))
	}
	if entry.Auth != providers.AuthOAuth {
		return m.setNotification(fmt.Sprintf("%s doesn't use OAuth - use /provider %s to toggle it", entry.DisplayName, entry.ID))
	}
	// A domain given inline keeps working unchanged for scripting/muscle
	// memory: /provider login github-copilot mycompany.ghe.com.
	if len(fields) > 1 {
		return m.startProviderOAuthLogin(entry, fields[1])
	}

	// GitHub Copilot silently defaulted to personal github.com here before -
	// ask interactively instead, so picking an Enterprise host doesn't
	// require already knowing the positional-arg syntax. Empty input (bare
	// Enter) still means personal GitHub.
	if entry.ID == "github-copilot" {
		return m.presentLocalPrompt(
			"GitHub Copilot host",
			"github.com by default, or type an Enterprise domain",
			"mycompany.ghe.com",
			func(domain string) tea.Cmd {
				return m.startProviderOAuthLogin(entry, strings.TrimSpace(domain))
			},
			func() tea.Cmd { return m.setNotification("login canceled") },
		)
	}

	return m.startProviderOAuthLogin(entry, "")
}

// startProviderOAuthLogin begins a provider's device-code login and reports
// the result via providerLoginStartedMsg. Shared by providerLogin's
// inline-domain-arg path and its interactive Enterprise-domain prompt path.
func (m *model) startProviderOAuthLogin(entry providers.CatalogEntry, enterpriseDomain string) tea.Cmd {
	m.appendMessage("system", providerui.StartMessage(entry.DisplayName))
	return func() tea.Msg {
		session, err := providers.BeginOAuthLogin(m.ctx, entry.ID, providers.LoginOptions{
			EnterpriseDomain: enterpriseDomain,
		})
		opened := false
		if err == nil {
			opened = providerui.TryOpenBrowser(m.ctx, session.DeviceCode.VerificationURI)
		}
		return providerLoginStartedMsg{providerID: entry.ID, displayName: entry.DisplayName, session: session, browserOpened: opened, err: err}
	}
}

func (m *model) providerLoginPoll(providerID, displayName string, session providers.OAuthLoginSession) tea.Cmd {
	return func() tea.Msg {
		creds, err := session.Poll(m.ctx)
		if err != nil {
			return providerLoginResultMsg{displayName: displayName, err: err}
		}
		if err := m.completeProviderLogin(providerID, creds); err != nil {
			return providerLoginResultMsg{displayName: displayName, err: err}
		}
		if m.refresh == nil {
			return providerLoginResultMsg{displayName: displayName}
		}
		models, err := m.refresh(m.ctx)
		return providerLoginResultMsg{displayName: displayName, models: models, err: err}
	}
}

// providerLogout handles /provider logout <name> - disables the provider and
// clears any stored OAuth credentials, for any provider kind.
func (m *model) providerLogout(name string) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return m.setNotification("usage: /provider logout <provider>")
	}
	entry, ok := providers.Lookup(name)
	if !ok {
		return m.setNotification(fmt.Sprintf("unknown provider %q", name))
	}
	if err := providers.NewManage(nil).Logout(entry.ID); err != nil {
		return m.setNotification(err.Error())
	}
	return m.refreshAfterProviderChange(entry.DisplayName, false, "")
}

// refreshAfterProviderChange re-discovers models after a provider toggle and
// returns a Cmd that reports the outcome (enabled/disabled + resulting
// model count) as a single combined scrollback line once the refresh
// completes - e.g. "OpenRouter enabled, models available: 42". Reporting
// the toggle and the refresh separately let cmdRefresh's own
// refreshResultMsg handler silently overwrite the toggle's own transient
// notification with "refreshed: N models available" moments later.
func (m *model) refreshAfterProviderChange(displayName string, enabled bool, warning string) tea.Cmd {
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	if m.refresh == nil {
		m.appendMessage("system", fmt.Sprintf("%s %s (model refresh is not available)", displayName, action))
		return nil
	}
	return func() tea.Msg {
		models, err := m.refresh(m.ctx)
		return providerToggleResultMsg{displayName: displayName, action: action, warning: warning, models: models, err: err}
	}
}

// providerToggleResultMsg carries the outcome of a provider toggle plus its
// model refresh (see refreshAfterProviderChange).
type providerToggleResultMsg struct {
	displayName string
	action      string
	warning     string
	models      []tauchat.ChatModelRef
	err         error
}

type providerLoginStartedMsg struct {
	providerID    string
	displayName   string
	session       providers.OAuthLoginSession
	browserOpened bool
	err           error
}

type providerLoginResultMsg struct {
	displayName string
	models      []tauchat.ChatModelRef
	err         error
}

// providerCfg/providerState mirror internal/tui/inline_providers.go's helpers
// of the same name.
func providerCfg(ctx context.Context) config.Config {
	cfg, _, _ := providers.NewManage(nil).Effective(ctx)
	return cfg
}

func providerState() providers.State {
	s, _ := providers.LoadState()
	return s
}

func (m *model) cmdSkills(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" || args == "list" {
		return sendCommand(m.runtime, tauchat.ListSkillsCommand{
			RequestedAt: time.Now().UTC(),
		})
	}
	return m.setNotification("usage: /skills list")
}

func (m *model) cmdSession(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	switch {
	case args == "" || args == "list":
		return sendCommand(m.runtime, tauchat.ListSessionsCommand{})
	case args == "info" || strings.HasPrefix(args, "info "):
		id := strings.TrimSpace(strings.TrimPrefix(args, "info"))
		if id == "" {
			return m.setNotification("usage: /session info <id>")
		}
		for _, s := range m.sessionSummaries {
			if s.ID == id {
				m.appendMessage("system", sessionInfoText(s))
				return nil
			}
		}
		return m.setNotification("session not found: " + id + " (try /session list first)")
	case strings.HasPrefix(args, "export"):
		id := strings.TrimSpace(strings.TrimPrefix(args, "export"))
		if id == "" {
			id = m.sessionID
		}
		return sendCommand(m.runtime, tauchat.ExportSessionCommand{
			SessionID: id,
			Format:    "jsonl",
		})
	case strings.HasPrefix(args, "delete"):
		id := strings.TrimSpace(strings.TrimPrefix(args, "delete"))
		if id == "" {
			return m.setNotification("usage: /session delete <id>")
		}
		return sendCommand(m.runtime, tauchat.DeleteSessionCommand{
			SessionID: id,
		})
	default:
		// Treat bare arg as a session ID to load.
		return sendCommand(m.runtime, tauchat.LoadSessionCommand{
			SessionID: args,
		})
	}
}

func (m *model) cmdResume(args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" {
		// List sessions so the user can pick.
		return sendCommand(m.runtime, tauchat.ListSessionsCommand{})
	}
	return sendCommand(m.runtime, tauchat.LoadSessionCommand{
		SessionID: args,
	})
}

// runAgentCommand activates a built-in agent (e.g. /plan) by name.
func (m *model) runAgentCommand(name, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	cmd := sendCommand(m.runtime, tauchat.RunAgentCommand{
		SessionID:   m.sessionID,
		Name:        name,
		RequestedAt: time.Now().UTC(),
	})
	if args == "" {
		return cmd
	}
	// When args are non-empty, also submit them as a prompt via the
	// turn-queueing path so they interleave correctly with a running turn.
	return tea.Batch(cmd, m.startOrQueueTurn(args))
}

// --- helpers ---------------------------------------------------------------

// currentModelRef returns the selected model from the available list.
func (m *model) currentModelRef() tauchat.ChatModelRef {
	for _, ref := range m.availableModels {
		if ref.ID == m.modelName {
			return ref
		}
	}
	return tauchat.ChatModelRef{}
}

// effortLevels returns the available reasoning-effort levels for a model.
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
		return levels[0]
	}
	return levels[0]
}

func defaultEffortForModel(model tauchat.ChatModelRef) string {
	if len(model.Config.ReasoningEfforts) > 0 {
		if slices.Contains(model.Config.ReasoningEfforts, "medium") {
			return "medium"
		}
		return model.Config.ReasoningEfforts[0]
	}
	return ""
}

// --- tea.Msg types ---------------------------------------------------------

type refreshResultMsg struct {
	models []tauchat.ChatModelRef
	err    error
}
