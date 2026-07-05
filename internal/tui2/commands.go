package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// --- command table ---------------------------------------------------------

// slashEntry is one row in the tui2 command table. It mirrors the legacy
// slashCommand type but handlers return tea.Cmd instead of mutating directly.
type slashEntry struct {
	name        string
	aliases     []string
	usage       string
	description string
	isAgent     bool
	run         func(m *model, args string) tea.Cmd
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
			name: "copy", description: "copy the assistant's last response to the clipboard",
			run: (*model).cmdCopy,
		},
		{
			name: "login", usage: "[provider]", description: "enable a provider (or list providers)",
			run: (*model).cmdLogin,
		},
		{
			name: "logout", usage: "<provider>", description: "disable a provider / remove its login",
			run: (*model).cmdLogout,
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
	if err == nil {
		for _, def := range defs {
			if !def.UserInvocable {
				continue
			}
			name := def.Name
			usage := def.ArgumentHint
			if usage == "" {
				usage = "[prompt]"
			}
			slashTable = append(slashTable, slashEntry{
				name:        name,
				usage:       usage,
				description: def.Description,
				isAgent:     true,
				run: func(m *model, args string) tea.Cmd {
					return m.runAgentCommand(name, args)
				},
			})
		}
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

	// /skills-reload — hot-reload skills from disk.
	if name == "skills-reload" {
		return sendCommand(m.runtime, tauchat.ReloadSkillsCommand{
			RequestedAt: time.Now().UTC(),
		})
	}

	// /skill:<name> — user-invoked skill activation.
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

func (m *model) cmdHelp(_ string) tea.Cmd {
	var b strings.Builder
	b.WriteString("Commands:")
	for i := range slashTable {
		e := &slashTable[i]
		label := "/" + e.name
		if e.usage != "" {
			label += " " + e.usage
		}
		fmt.Fprintf(&b, "\n  %-38s %s", label, e.description)
	}
	fmt.Fprintf(&b, "\n  %-38s %s", "Ctrl+S", "steer the agent mid-turn")
	fmt.Fprintf(&b, "\n  %-38s %s", "Ctrl+Shift+G", "copy the assistant's last response")
	m.appendMessage("system", b.String())
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
		if len(m.availableModels) == 0 {
			return m.setNotification("no models available — try /refresh")
		}
		// Prefill input for completion picker.
		m.input = "/model "
		return nil
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
		return m.setNotification(fmt.Sprintf("model %q not found — try /refresh", modelID))
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

func (m *model) cmdCost(_ string) tea.Cmd {
	if m.usage == nil {
		return m.setNotification("no usage recorded yet")
	}
	totals := m.usage.Snapshot(m.sessionID)
	if totals == nil || totals.TotalTokens == 0 {
		return m.setNotification("no usage recorded yet")
	}
	msg := fmt.Sprintf("Prompt: %d  Completion: %d  Total: %d  Cost: $%.4f",
		totals.PromptTokens, totals.CompletionTokens, totals.TotalTokens, totals.Cost,
	)
	return m.setNotification(msg)
}

func (m *model) cmdCopy(_ string) tea.Cmd {
	content := m.lastAssistantContent()
	if content == "" {
		return m.setNotification("nothing to copy")
	}
	return tea.SetClipboard(content)
}

func (m *model) cmdLogin(args string) tea.Cmd {
	return m.setNotification("/login not yet wired in tui2 — use legacy TUI")
}

func (m *model) cmdLogout(args string) tea.Cmd {
	return m.setNotification("/logout not yet wired in tui2 — use legacy TUI")
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
	case args == "info":
		return m.setNotification(fmt.Sprintf("session: %s  model: %s @ %s", m.sessionID, m.modelName, m.provider))
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

// lastAssistantContent returns the content of the last assistant message.
func (m *model) lastAssistantContent() string {
	for i := len(m.renderedLines) - 1; i >= 0; i-- {
		// Search backwards for the most recent assistant line.
		line := m.renderedLines[i]
		if after, ok := strings.CutPrefix(line, "tau: "); ok {
			return after
		}
	}
	return ""
}

// --- tea.Msg types ---------------------------------------------------------

type refreshResultMsg struct {
	models []tauchat.ChatModelRef
	err    error
}
