package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/views"
)

func (c *ChatPanel) handleDebugCommand(rest string) {
	rest = strings.TrimSpace(rest)
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		c.lastError.Set("usage: /debug <subcommand> [args]\navailable subcommands: components")
		return
	}
	sub := strings.ToLower(parts[0])
	switch sub {
	case "components":
		if len(parts) < 2 {
			c.launchDebugListView()
		} else {
			c.launchDebugView(parts[1])
		}
	default:
		c.lastError.Set(fmt.Sprintf("unknown debug subcommand: %q (available: components)", sub))
	}
}

func (c *ChatPanel) launchDebugListView() {
	if c.app == nil {
		return
	}
	if c.debugListView == nil {
		c.debugListView = views.NewDebugListView(c.showDebugList, func(name string) {
			c.showDebugList.Set(false)
			c.launchDebugView(name)
		})
		c.debugListView.BindApp(c.app)
	}
	c.showDebugList.Set(true)
}

func (c *ChatPanel) launchDebugView(name string) {
	if c.app == nil {
		return
	}
	v := views.NewDebugView(name, c.showDebug)
	if v == nil {
		c.lastError.Set("component not found: " + name)
		return
	}
	v.BindApp(c.app)
	c.debugView = v
	c.showDebug.Set(true)
	c.dumpTreeOnNextRender = true
}

// --- Session management ---

func (c *ChatPanel) handleSessionCommand(rest string) {
	parts := strings.Fields(strings.TrimSpace(rest))
	if len(parts) == 0 {
		// Bare "/session" — print session list.
		c.openSessionList()
		return
	}

	sub := strings.ToLower(parts[0])
	switch sub {
	case "info":
		c.handleSessionInfo(rest)
	case "export":
		c.handleSessionExport(rest)
	case "delete":
		c.handleSessionDelete(rest)
	case "list":
		c.openSessionList()
	case "tree":
		c.openSessionTree()
	default:
		c.loadSession(parts[0])
	}
}

func (c *ChatPanel) handleResumeCommand(rest string) {
	id := strings.TrimSpace(rest)
	if id == "" {
		c.openSessionList()
		return
	}
	c.loadSession(id)
}

func (c *ChatPanel) loadSession(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.sendCommand(tauchat.LoadSessionCommand{SessionID: id, RuntimeSessionID: c.cfg.SessionID})
}

func (c *ChatPanel) openSessionList() {
	if c.app == nil {
		return
	}

	selected := gt.NewState(0)
	selected.BindApp(c.app)

	c.sessionListView = views.NewSessionListView(
		c.showSessionList,
		c.sessionSummaries,
		selected,
		func(summary tauchat.SessionSummary) {
			c.loadSession(summary.ID)
		},
		func() {
			c.sendCommand(tauchat.ListSessionsCommand{Limit: 10, Cursor: c.sessionListCursor})
		},
	)
	c.sessionListView.BindApp(c.app)

	// Fetch the first page.
	c.sendCommand(tauchat.ListSessionsCommand{Limit: 10})
	c.showSessionList.Set(true)
}

func (c *ChatPanel) openSessionTree() {
	if c.app == nil {
		return
	}

	// Close the flat session list if it's open — only one session view at a time.
	c.showSessionList.Set(false)

	c.sessionTreeView = views.NewSessionTreeView(
		c.sessionSummaries,
		func(summary tauchat.SessionSummary) {
			c.loadSession(summary.ID)
			c.showSessionTree.Set(false)
		},
		func() {
			c.showSessionTree.Set(false)
		},
		func() {
			c.sendCommand(tauchat.ListSessionsCommand{Limit: views.SessionTreeFetchLimit})
		},
	)
	c.sessionTreeView.BindApp(c.app)

	// Fetch sessions for tree building.
	c.sendCommand(tauchat.ListSessionsCommand{Limit: views.SessionTreeFetchLimit})
	c.showSessionTree.Set(true)
}

func (c *ChatPanel) handleSessionInfo(rest string) {
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		c.lastError.Set("usage: /session info <id>")
		return
	}
	id := parts[1]
	summaries := c.sessionSummaries.Get()
	for _, s := range summaries {
		if s.ID == id {
			c.printSessionInfo(s)
			return
		}
	}
	c.lastError.Set("session not found: " + id + " (try /session list first)")
}

func (c *ChatPanel) handleSessionExport(rest string) {
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		c.lastError.Set("usage: /session export <id> [path]")
		return
	}
	id := parts[1]
	outputPath := ""
	if len(parts) >= 3 {
		outputPath = parts[2]
	}
	c.sendCommand(tauchat.ExportSessionCommand{
		SessionID: id,
		Format:    "jsonl",
		Output:    outputPath,
	})
}

func (c *ChatPanel) handleSessionDelete(rest string) {
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		c.lastError.Set("usage: /session delete <id>")
		return
	}
	c.sendCommand(tauchat.DeleteSessionCommand{SessionID: parts[1]})
}

func (c *ChatPanel) handleSubmit(value string) {
	c.handleSubmitWithDepth(value, 0)
}

func (c *ChatPanel) handleSubmitWithDepth(value string, depth int) {
	// Debounce rapid submits (rapid Enter, paste CR bytes).
	if elapsed := time.Since(c.lastSubmitTime); elapsed < 300*time.Millisecond {
		return
	}
	c.lastSubmitTime = time.Now()

	text := strings.TrimSpace(value)
	if text == "" {
		return
	}
	if depth > 3 {
		c.lastError.Set("autocomplete recursion limit exceeded")
		return
	}

	// Prevent submitting while a request is already in flight.
	if c.status.Get() == tauchat.ChatSessionStreaming {
		c.notice.Set("a request is already in progress")
		return
	}
	if c.shouldApplyCompletion(value) {
		completed, acceptsArgs := c.applySelectedCompletion()
		if completed != "" && !acceptsArgs {
			c.handleSubmitWithDepth(completed, depth+1)
		}
		return
	}
	c.clearInput()
	c.lastError.Set("")
	c.printStyledAbovef("%s", c.userMessageBlock(text))
	if strings.HasPrefix(text, "/") {
		c.handleSlashCommand(text)
		return
	}

	reqID, err := uuid.NewV7()
	if err != nil {
		c.lastError.Set(err.Error())
		return
	}

	requestID := reqID.String()

	if err := c.runtime.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   c.cfg.SessionID,
		RequestID:   requestID,
		Prompt:      text,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		c.lastError.Set(err.Error())
	}
}

func (c *ChatPanel) setInputText(value string) {
	if c.input != nil {
		c.input.SetText(value)
	} else {
		c.inputValue.Set(value)
	}
	c.syncCompletions(value)
	c.adjustInlineHeight()
}

func (c *ChatPanel) clearInput() {
	if c.input != nil {
		c.input.Clear()
	} else {
		c.inputValue.Set("")
	}
	c.closeCompletions()
	c.adjustInlineHeight()
}

func (c *ChatPanel) handleSlashCommand(text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	command := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	rest := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))

	switch command {
	case "exit", "quit", "q":
		if c.app != nil {
			c.app.Stop()
		}
	case "help", "?":
		c.showHelp.Set(!c.showHelp.Get())
	case "new", "clear", "reset":
		c.sendCommand(tauchat.ResetChatSessionCommand{SessionID: c.cfg.SessionID, RequestedAt: time.Now().UTC()})
	case "reload":
		c.sendCommand(tauchat.ReloadExtensionsCommand{RequestedAt: time.Now().UTC()})
	case "refresh", "models":
		c.refreshModels()
	case "reasoning":
		c.handleReasoningCommand(parts)
	case "model":
		c.handleModelCommand(rest)
	case "system":
		c.handleSystemCommand(rest)
	case "settings":
		c.showSettings.Set(true)
		c.closeCompletions()
	case "session":
		c.handleSessionCommand(rest)
	case "resume":
		c.handleResumeCommand(rest)
	case "debug":
		if c.cfg.Debug {
			c.handleDebugCommand(rest)
		} else {
			c.lastError.Set("unknown command: /" + command)
		}
	default:
		if extCommand, ok := c.extensionCommands.Get()[command]; ok {
			c.sendCommand(tauchat.RunExtensionCommandCommand{
				Name:        extCommand.Name,
				Args:        rest,
				RequestedAt: time.Now().UTC(),
			})
			return
		}
		c.lastError.Set("unknown command: /" + command)
	}
}

func (c *ChatPanel) sendCommand(command tauchat.ChatCommand) {
	if err := c.runtime.Send(command); err != nil {
		c.lastError.Set(err.Error())
	}
}

func (c *ChatPanel) handleReasoningCommand(parts []string) {
	if len(parts) < 2 {
		c.showReasoning.Set(!c.showReasoning.Get())
		return
	}
	switch strings.ToLower(parts[1]) {
	case "on", "true", "yes":
		c.showReasoning.Set(true)
	case "off", "false", "no":
		c.showReasoning.Set(false)
	default:
		c.showReasoning.Set(!c.showReasoning.Get())
	}
}

func (c *ChatPanel) handleModelCommand(modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		c.notice.Set(c.modelHelp())
		return
	}
	for _, model := range c.availableModels.Get() {
		if model.ID != modelID {
			continue
		}
		if !model.Ready {
			c.lastError.Set(fmt.Sprintf("model %q is not ready", modelID))
			return
		}
		c.sendCommand(tauchat.UpdateChatSessionCommand{
			SessionID: c.cfg.SessionID,
			Patch:     tauchat.ChatSessionPatch{Model: &model},
		})
		c.modelName.Set(model.ID)
		return
	}
	c.lastError.Set(fmt.Sprintf("model %q is not in the available model list", modelID))
}

func (c *ChatPanel) modelHelp() string {
	models := c.availableModels.Get()
	if len(models) == 0 {
		return "no models available — try /refresh"
	}
	ids := make([]string, 0, min(len(models), 5))
	for _, model := range models {
		if !model.Ready {
			continue
		}
		ids = append(ids, model.ID)
		if len(ids) >= 5 {
			break
		}
	}
	if len(ids) == 0 {
		return "no ready models available"
	}
	return "available models: " + strings.Join(ids, ", ")
}

func (c *ChatPanel) handleSystemCommand(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		c.lastError.Set("usage: /system <prompt>")
		return
	}
	c.sendCommand(tauchat.UpdateChatSessionCommand{
		SessionID: c.cfg.SessionID,
		Patch:     tauchat.ChatSessionPatch{SystemPrompt: &prompt},
	})
}

func (c *ChatPanel) refreshModels() {
	if c.cfg.RefreshModels == nil {
		c.lastError.Set("model refresh is not available")
		return
	}
	c.notice.Set("refreshing models…")
	go func() {
		models, err := c.cfg.RefreshModels(c.ctx)
		if c.app == nil {
			return
		}
		c.app.QueueUpdate(func() {
			if err != nil {
				c.lastError.Set("model refresh failed: " + err.Error())
				return
			}
			c.availableModels.Set(slices.Clone(models))
			c.notice.Set(fmt.Sprintf("refreshed models: %d available", len(models)))
			c.syncCompletions(c.inputValue.Get())
		})
	}()
}
