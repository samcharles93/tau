// Package gotui provides the go-tui based chat interface for Tau.
package gotui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	gotui "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// ModelRefresher re-discovers available models from the selected provider.
type ModelRefresher func(ctx context.Context) ([]tauchat.ChatModelRef, error)

// Config holds go-tui chat panel configuration.
type Config struct {
	SessionID          string
	ModelName          string
	Provider           string
	AvailableModels    []tauchat.ChatModelRef
	AvailableProviders []string
	NotifySub          *pubsub.Subscription[notify.Notification]
	RefreshModels      ModelRefresher
	ShowReasoning      bool
}

type completionItem struct {
	Value       string
	Label       string
	Description string
	AcceptsArgs bool
}

// ChatPanel is the root go-tui component for the interactive chat UI.
type ChatPanel struct {
	ctx      context.Context
	runtime  tauchat.ChatRuntime
	eventSub *pubsub.Subscription[tauchat.ChatEvent]
	cfg      Config
	app      *gotui.App

	messages           *gotui.State[[]tauchat.ChatMessage]
	streamingContent   *gotui.State[string]
	streamingReasoning *gotui.State[string]
	inputValue         *gotui.State[string]
	scrollY            *gotui.State[int]
	status             *gotui.State[tauchat.ChatSessionStatus]
	lastError          *gotui.State[string]
	notice             *gotui.State[string]
	showHelp           *gotui.State[bool]
	showReasoning      *gotui.State[bool]
	modelName          *gotui.State[string]
	availableModels    *gotui.State[[]tauchat.ChatModelRef]
	extensionCommands  *gotui.State[map[string]tauchat.ExtensionCommand]
	activeRequestID    *gotui.State[string]
	completions        *gotui.State[[]completionItem]
	completionIndex    *gotui.State[int]
	showSettings       *gotui.State[bool]
	settingsModal      *gotui.Modal
	input              *chatInput
}

// NewChatPanel creates a new go-tui chat panel with reactive state initialized.
func NewChatPanel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	eventSub *pubsub.Subscription[tauchat.ChatEvent],
	cfg Config,
) *ChatPanel {
	commands := make(map[string]tauchat.ExtensionCommand)
	models := slices.Clone(cfg.AvailableModels)
	panel := &ChatPanel{
		ctx:                ctx,
		runtime:            runtime,
		eventSub:           eventSub,
		cfg:                cfg,
		messages:           gotui.NewState(make([]tauchat.ChatMessage, 0)),
		streamingContent:   gotui.NewState(""),
		streamingReasoning: gotui.NewState(""),
		inputValue:         gotui.NewState(""),
		scrollY:            gotui.NewState(0),
		status:             gotui.NewState(tauchat.ChatSessionIdle),
		lastError:          gotui.NewState(""),
		notice:             gotui.NewState(""),
		showHelp:           gotui.NewState(false),
		showReasoning:      gotui.NewState(cfg.ShowReasoning),
		modelName:          gotui.NewState(cfg.ModelName),
		availableModels:    gotui.NewState(models),
		extensionCommands:  gotui.NewState(commands),
		activeRequestID:    gotui.NewState(""),
		completions:        gotui.NewState(make([]completionItem, 0)),
		completionIndex:    gotui.NewState(0),
		showSettings:       gotui.NewState(false),
	}
	panel.settingsModal = gotui.NewModal(
		gotui.WithModalOpen(panel.showSettings),
		gotui.WithModalBackdrop("dim"),
		gotui.WithModalTrapFocus(true),
		gotui.WithModalElementOptions(
			gotui.WithDisplay(gotui.DisplayFlex),
			gotui.WithDirection(gotui.Column),
			gotui.WithJustify(gotui.JustifyCenter),
			gotui.WithAlign(gotui.AlignCenter),
		),
		gotui.WithModalKeyMap(gotui.KeyMap{
			gotui.OnPreemptStop(gotui.Rune('q'), func(gotui.KeyEvent) {
				panel.showSettings.Set(false)
			}),
		}),
	)
	return panel
}

// BindApp binds all reactive state to the running app.
func (c *ChatPanel) BindApp(app *gotui.App) {
	c.app = app
	c.messages.BindApp(app)
	c.streamingContent.BindApp(app)
	c.streamingReasoning.BindApp(app)
	c.inputValue.BindApp(app)
	c.scrollY.BindApp(app)
	c.status.BindApp(app)
	c.lastError.BindApp(app)
	c.notice.BindApp(app)
	c.showHelp.BindApp(app)
	c.showReasoning.BindApp(app)
	c.modelName.BindApp(app)
	c.availableModels.BindApp(app)
	c.extensionCommands.BindApp(app)
	c.activeRequestID.BindApp(app)
	c.completions.BindApp(app)
	c.completionIndex.BindApp(app)
	c.showSettings.BindApp(app)
	c.settingsModal.BindApp(app)
}

// Watchers bridges runtime and notification channels into the go-tui event loop.
func (c *ChatPanel) Watchers() []gotui.Watcher {
	watchers := make([]gotui.Watcher, 0, 2)
	if c.eventSub != nil && c.eventSub.Channel() != nil {
		watchers = append(watchers, gotui.Watch(c.eventSub.Channel(), c.handleRuntimeEvent))
	}
	if c.cfg.NotifySub != nil && c.cfg.NotifySub.Channel() != nil {
		watchers = append(watchers, gotui.Watch(c.cfg.NotifySub.Channel(), c.handleNotification))
	}
	return watchers
}

func (c *ChatPanel) handleNotification(n notify.Notification) {
	if strings.TrimSpace(n.Message) == "" {
		return
	}
	c.notice.Set(n.Message)
	if n.Level == notify.LevelError {
		c.lastError.Set(n.Message)
	}
}

func (c *ChatPanel) handleRuntimeEvent(event tauchat.ChatEvent) {
	switch ev := event.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		c.syncState(ev.State)
	case tauchat.ChatResponseStartedEvent:
		if ev.SessionID != c.cfg.SessionID {
			return
		}
		c.activeRequestID.Set(ev.RequestID)
		c.status.Set(tauchat.ChatSessionStreaming)
		c.lastError.Set("")
	case tauchat.ChatResponseDeltaEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.streamingContent.Set(ev.Snapshot)
		c.scrollToBottom()
	case tauchat.ChatReasoningDeltaEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.streamingReasoning.Set(ev.Snapshot)
		c.scrollToBottom()
	case tauchat.ChatToolExecutionStartedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.notice.Set(fmt.Sprintf("tool started: %s %s", ev.ToolName, ev.ArgumentsSummary))
	case tauchat.ChatToolExecutionCompletedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.notice.Set(fmt.Sprintf("tool completed: %s %s (%s)", ev.ToolName, ev.Status, ev.Duration))
	case tauchat.ChatResponseCompletedEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		c.syncState(ev.State)
	case tauchat.ChatResponseCancelledEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		c.syncState(ev.State)
		c.notice.Set("chat request cancelled")
	case tauchat.ChatRuntimeErrorEvent:
		if ev.SessionID != "" && ev.SessionID != c.cfg.SessionID {
			return
		}
		c.lastError.Set(ev.Message)
		c.notice.Set(ev.Message)
		c.status.Set(tauchat.ChatSessionIdle)
	case tauchat.ChatNotificationEvent:
		c.notice.Set(ev.Message)
		if ev.Level == tauchat.ChatNotificationError {
			c.lastError.Set(ev.Message)
		}
	case tauchat.ExtensionsReloadedEvent:
		c.setExtensionCommands(ev.Result.Commands)
		c.notice.Set(fmt.Sprintf("reloaded extensions: %d loaded", ev.Result.ExtensionCount))
	case tauchat.ExtensionCommandsChangedEvent:
		c.setExtensionCommands(ev.Commands)
	case tauchat.ExtensionCommandResultEvent:
		c.appendMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleTool, Content: ev.Output})
	case tauchat.InteractivePromptRequestedEvent:
		c.notice.Set(ev.Title + ": " + ev.Message)
	}
}

func (c *ChatPanel) matchesRequest(sessionID, requestID string) bool {
	if sessionID != c.cfg.SessionID {
		return false
	}
	active := c.activeRequestID.Get()
	return active == "" || requestID == "" || active == requestID
}

func (c *ChatPanel) syncState(state tauchat.ChatSessionState) {
	if state.SessionID != c.cfg.SessionID {
		return
	}
	c.status.Set(state.Status)
	c.messages.Set(slices.Clone(state.Messages))
	c.streamingContent.Set(state.PendingAssistant)
	c.activeRequestID.Set(state.ActiveRequestID)
	c.lastError.Set(state.LastError)
	if state.Model.ID != "" {
		c.modelName.Set(state.Model.ID)
	}
	if state.Status == tauchat.ChatSessionIdle || state.Status == tauchat.ChatSessionClosed {
		c.streamingReasoning.Set("")
	}
	c.scrollToBottom()
}

func (c *ChatPanel) setExtensionCommands(commands []tauchat.ExtensionCommand) {
	next := make(map[string]tauchat.ExtensionCommand, len(commands))
	for _, command := range commands {
		name := strings.TrimSpace(command.Name)
		if name == "" {
			continue
		}
		next[name] = command
	}
	c.extensionCommands.Set(next)
}

func (c *ChatPanel) appendMessage(message tauchat.ChatMessage) {
	messages := slices.Clone(c.messages.Get())
	messages = append(messages, message)
	c.messages.Set(messages)
	c.scrollToBottom()
}

func (c *ChatPanel) scrollToBottom() {
	messageLines := len(c.messages.Get()) * 4
	streamingLines := strings.Count(c.streamingContent.Get(), "\n") + strings.Count(c.streamingReasoning.Get(), "\n")
	c.scrollY.Set(max(0, messageLines+streamingLines-1))
}

// Render builds the go-tui element tree.
func (c *ChatPanel) Render(app *gotui.App) *gotui.Element {
	root := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithWidthPercent(100),
		gotui.WithHeightPercent(100),
	)

	root.AddChild(c.renderHeader())
	root.AddChild(c.renderMessages())
	if errMsg := strings.TrimSpace(c.lastError.Get()); errMsg != "" {
		root.AddChild(gotui.New(
			gotui.WithText("Error: "+errMsg),
			gotui.WithTextStyle(theme.ErrorStyle()),
			gotui.WithPaddingTRBL(0, 1, 0, 1),
			gotui.WithFlexShrink(0),
		))
	}
	if completions := c.renderCompletions(); completions != nil {
		root.AddChild(completions)
	}
	root.AddChild(c.renderInput(app))
	root.AddChild(c.renderStatusBar())
	root.AddChild(c.renderSettingsModal(app))
	return root
}

func (c *ChatPanel) renderHeader() *gotui.Element {
	header := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithBorder(gotui.BorderSingle),
		gotui.WithBorderStyle(theme.BorderStyle()),
		gotui.WithPaddingTRBL(0, 1, 0, 1),
		gotui.WithFlexShrink(0),
	)
	header.AddChild(gotui.New(
		gotui.WithText("τ tau"),
		gotui.WithTextStyle(theme.BrandStyle()),
	))
	header.AddChild(gotui.New(gotui.WithFlexGrow(1)))
	right := c.modelName.Get()
	if c.cfg.Provider != "" {
		right += " · " + c.cfg.Provider
	}
	header.AddChild(gotui.New(
		gotui.WithText(right),
		gotui.WithTextStyle(theme.DimStyle()),
	))
	return header
}

func (c *ChatPanel) renderMessages() *gotui.Element {
	messageList := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithFlexGrow(1),
		gotui.WithScrollable(gotui.ScrollVertical),
		gotui.WithScrollOffset(0, c.scrollY.Get()),
		gotui.WithScrollbarHidden(true),
		gotui.WithPadding(1),
		gotui.WithGap(1),
	)

	messages := c.messages.Get()
	for _, msg := range messages {
		messageList.AddChild(c.renderMessage(msg))
	}
	if c.streamingContent.Get() != "" || c.streamingReasoning.Get() != "" {
		messageList.AddChild(c.renderStreamingContent())
	}
	if len(messages) == 0 && c.streamingContent.Get() == "" {
		messageList.AddChild(gotui.New(
			gotui.WithText("Start a conversation…"),
			gotui.WithTextStyle(theme.DimStyle().Italic()),
			gotui.WithTextAlign(gotui.TextAlignCenter),
		))
	}
	return messageList
}

func (c *ChatPanel) renderMessage(msg tauchat.ChatMessage) *gotui.Element {
	block := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithGap(1),
	)
	block.AddChild(gotui.New(
		gotui.WithWidth(1),
		gotui.WithText(roleRail(msg.Role)),
		gotui.WithTextStyle(roleStyle(msg.Role)),
	))

	body := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithFlexGrow(1),
	)
	if label := roleLabel(msg.Role); label != "" {
		body.AddChild(gotui.New(
			gotui.WithText(label),
			gotui.WithTextStyle(roleStyle(msg.Role).Bold()),
		))
	}
	if c.showReasoning.Get() && msg.ReasoningContent != "" {
		body.AddChild(gotui.New(
			gotui.WithText("Reasoning\n"+msg.ReasoningContent),
			gotui.WithTextStyle(theme.DimStyle()),
			gotui.WithWrap(true),
		))
	}
	if msg.Content != "" {
		body.AddChild(gotui.New(
			gotui.WithText(msg.Content),
			gotui.WithTextStyle(theme.BodyStyle()),
			gotui.WithWrap(true),
		))
	}
	block.AddChild(body)
	return block
}

func (c *ChatPanel) renderStreamingContent() *gotui.Element {
	content := c.streamingContent.Get()
	reasoning := ""
	if c.showReasoning.Get() {
		reasoning = c.streamingReasoning.Get()
	}
	if c.status.Get() == tauchat.ChatSessionStreaming {
		switch {
		case content != "":
			content += " ▌"
		case reasoning != "":
			reasoning += " ▌"
		default:
			content = "▌"
		}
	}
	return c.renderMessage(tauchat.ChatMessage{
		Role:             tauchat.ChatRoleAssistant,
		Content:          content,
		ReasoningContent: reasoning,
	})
}

func (c *ChatPanel) renderInput(app *gotui.App) *gotui.Element {
	inputContainer := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithBorder(gotui.BorderSingle),
		gotui.WithBorderStyle(theme.BorderStyle()),
		gotui.WithPaddingTRBL(0, 1, 0, 1),
		gotui.WithFlexShrink(0),
	)
	inputContainer.AddChild(gotui.New(
		gotui.WithText("› "),
		gotui.WithTextStyle(theme.BrandStyle()),
	))
	input := app.MountPersistent(c, 0, func() gotui.Component {
		if c.input == nil {
			c.input = newChatInput(
				c.inputValue,
				120,
				"Send a message…",
				theme.BodyStyle(),
				theme.DimStyle(),
				theme.ColorPurple,
				c.handleSubmit,
				c.syncCompletions,
			)
		}
		return c.input
	})
	inputContainer.AddChild(input)
	return inputContainer
}

func (c *ChatPanel) renderStatusBar() *gotui.Element {
	statusBar := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithBorder(gotui.BorderSingle),
		gotui.WithBorderStyle(theme.BorderStyle()),
		gotui.WithPaddingTRBL(0, 1, 0, 1),
		gotui.WithFlexShrink(0),
	)
	statusBar.AddChild(gotui.New(
		gotui.WithText(c.statusText()),
		gotui.WithTextStyle(c.statusStyle()),
	))
	if notice := strings.TrimSpace(c.notice.Get()); notice != "" {
		statusBar.AddChild(gotui.New(
			gotui.WithText(" · "+notice),
			gotui.WithTextStyle(theme.DimStyle()),
			gotui.WithTruncate(true),
		))
	}
	statusBar.AddChild(gotui.New(gotui.WithFlexGrow(1)))
	modelStatus := c.modelName.Get()
	if c.cfg.Provider != "" {
		modelStatus += " · " + c.cfg.Provider
	}
	if modelStatus != "" {
		statusBar.AddChild(gotui.New(
			gotui.WithText(modelStatus+" · "),
			gotui.WithTextStyle(theme.BrandStyle()),
			gotui.WithTruncate(true),
		))
	}
	help := "enter send · tab complete · ctrl+r reasoning · ctrl+c cancel/quit"
	if c.showHelp.Get() {
		help = "/new · /model <id> · /system <prompt> · /reload · /refresh · /settings · /exit"
	}
	statusBar.AddChild(gotui.New(
		gotui.WithText(help),
		gotui.WithTextStyle(theme.DimStyle()),
		gotui.WithTruncate(true),
	))
	return statusBar
}

func (c *ChatPanel) renderSettingsModal(app *gotui.App) *gotui.Element {
	modal := c.settingsModal.Render(app)
	content := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithWidth(64),
		gotui.WithBorder(gotui.BorderRounded),
		gotui.WithBorderStyle(theme.BrandStyle()),
		gotui.WithPadding(1),
		gotui.WithGap(1),
	)
	content.AddChild(gotui.New(
		gotui.WithText("Settings"),
		gotui.WithTextStyle(theme.BrandStyle()),
	))
	content.AddChild(gotui.New(
		gotui.WithText("Model: "+c.modelName.Get()),
		gotui.WithTextStyle(theme.BodyStyle()),
		gotui.WithTruncate(true),
	))
	if c.cfg.Provider != "" {
		content.AddChild(gotui.New(
			gotui.WithText("Provider: "+c.cfg.Provider),
			gotui.WithTextStyle(theme.BodyStyle()),
			gotui.WithTruncate(true),
		))
	}
	reasoning := "off"
	if c.showReasoning.Get() {
		reasoning = "on"
	}
	content.AddChild(gotui.New(
		gotui.WithText("Reasoning: "+reasoning+" (Ctrl+R or /reasoning toggle)"),
		gotui.WithTextStyle(theme.BodyStyle()),
		gotui.WithTruncate(true),
	))
	content.AddChild(gotui.New(
		gotui.WithText("Commands: /model <id>, /refresh, /reload, /system <prompt>"),
		gotui.WithTextStyle(theme.DimStyle()),
		gotui.WithWrap(true),
	))
	content.AddChild(gotui.New(
		gotui.WithText("Esc or q closes this dialog."),
		gotui.WithTextStyle(theme.DimStyle()),
	))
	modal.AddChild(content)
	return modal
}

func (c *ChatPanel) statusText() string {
	switch c.status.Get() {
	case tauchat.ChatSessionStreaming:
		return "Streaming"
	case tauchat.ChatSessionCancelling:
		return "Cancelling"
	case tauchat.ChatSessionClosed:
		return "Closed"
	default:
		return "Ready"
	}
}

func (c *ChatPanel) statusStyle() gotui.Style {
	switch c.status.Get() {
	case tauchat.ChatSessionStreaming, tauchat.ChatSessionCancelling:
		return theme.BrandStyle()
	case tauchat.ChatSessionClosed:
		return theme.DimStyle()
	default:
		return theme.ReadyStyle()
	}
}

func (c *ChatPanel) handleSubmit(value string) {
	text := strings.TrimSpace(value)
	if text == "" {
		return
	}
	if c.shouldApplyCompletion(value) {
		c.applySelectedCompletion()
		return
	}
	c.inputValue.Set("")
	c.closeCompletions()
	c.lastError.Set("")
	if strings.HasPrefix(text, "/") {
		c.handleSlashCommand(text)
		return
	}
	requestID, err := newID("req")
	if err != nil {
		c.lastError.Set(err.Error())
		return
	}
	if err := c.runtime.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   c.cfg.SessionID,
		RequestID:   requestID,
		Prompt:      text,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		c.lastError.Set(err.Error())
	}
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
	case "toggle":
		c.showReasoning.Set(!c.showReasoning.Get())
	default:
		c.lastError.Set("usage: /reasoning on|off|toggle")
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

func (c *ChatPanel) renderCompletions() *gotui.Element {
	items := c.completions.Get()
	if len(items) == 0 {
		return nil
	}
	selected := clamp(c.completionIndex.Get(), 0, len(items)-1)
	container := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithBorder(gotui.BorderSingle),
		gotui.WithBorderStyle(theme.BorderStyle()),
		gotui.WithPaddingTRBL(0, 1, 0, 1),
		gotui.WithFlexShrink(0),
		gotui.WithGap(0),
	)
	for i, item := range items {
		prefix := "  "
		style := theme.DimStyle()
		if i == selected {
			prefix = "› "
			style = theme.BrandStyle()
		}
		line := prefix + item.Label
		if item.Description != "" {
			line += " — " + item.Description
		}
		container.AddChild(gotui.New(
			gotui.WithText(line),
			gotui.WithTextStyle(style),
			gotui.WithTruncate(true),
		))
	}
	return container
}

func (c *ChatPanel) syncCompletions(value string) {
	items := c.completionItems(value)
	c.completions.Set(items)
	if len(items) == 0 {
		c.completionIndex.Set(0)
		return
	}
	c.completionIndex.Set(clamp(c.completionIndex.Get(), 0, len(items)-1))
}

func (c *ChatPanel) completionItems(value string) []completionItem {
	if !strings.HasPrefix(value, "/") {
		return nil
	}
	parts := strings.SplitN(value, " ", 2)
	commandPrefix := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
	if len(parts) == 1 {
		return c.commandCompletions(commandPrefix)
	}

	argPrefix := strings.TrimSpace(parts[1])
	switch commandPrefix {
	case "model":
		return c.modelCompletions(argPrefix)
	case "reasoning":
		return c.reasoningCompletions(argPrefix)
	default:
		return nil
	}
}

func (c *ChatPanel) commandCompletions(prefix string) []completionItem {
	commands := builtinCompletionItems()
	for _, ext := range c.sortedExtensionCommands() {
		commands = append(commands, completionItem{
			Value:       "/" + ext.Name,
			Label:       "/" + ext.Name,
			Description: ext.Description,
		})
	}

	matches := make([]completionItem, 0, len(commands))
	for _, item := range commands {
		name := strings.TrimPrefix(strings.ToLower(item.Label), "/")
		if prefix == "" || strings.HasPrefix(name, prefix) {
			matches = append(matches, item)
		}
	}
	return limitCompletions(matches)
}

func (c *ChatPanel) modelCompletions(prefix string) []completionItem {
	prefix = strings.ToLower(prefix)
	models := slices.Clone(c.availableModels.Get())
	slices.SortFunc(models, func(a, b tauchat.ChatModelRef) int {
		return strings.Compare(a.ID, b.ID)
	})
	items := make([]completionItem, 0, len(models))
	for _, model := range models {
		if !model.Ready {
			continue
		}
		if prefix != "" && !strings.Contains(strings.ToLower(model.ID), prefix) {
			continue
		}
		items = append(items, completionItem{
			Value:       "/model " + model.ID,
			Label:       model.ID,
			Description: model.URL,
		})
	}
	return limitCompletions(items)
}

func (c *ChatPanel) reasoningCompletions(prefix string) []completionItem {
	options := []completionItem{
		{Value: "/reasoning on", Label: "on", Description: "show reasoning before Tau responses"},
		{Value: "/reasoning off", Label: "off", Description: "hide reasoning"},
		{Value: "/reasoning toggle", Label: "toggle", Description: "toggle reasoning visibility"},
	}
	prefix = strings.ToLower(prefix)
	matches := make([]completionItem, 0, len(options))
	for _, item := range options {
		if prefix == "" || strings.HasPrefix(item.Label, prefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

func (c *ChatPanel) sortedExtensionCommands() []tauchat.ExtensionCommand {
	commands := c.extensionCommands.Get()
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]tauchat.ExtensionCommand, 0, len(names))
	for _, name := range names {
		out = append(out, commands[name])
	}
	return out
}

func (c *ChatPanel) shouldApplyCompletion(value string) bool {
	items := c.completions.Get()
	if len(items) == 0 {
		return false
	}
	item := items[clamp(c.completionIndex.Get(), 0, len(items)-1)]
	return item.Value != value
}

func (c *ChatPanel) applySelectedCompletion() {
	items := c.completions.Get()
	if len(items) == 0 {
		return
	}
	item := items[clamp(c.completionIndex.Get(), 0, len(items)-1)]
	if c.input != nil {
		c.input.SetText(item.Value)
		return
	}
	c.inputValue.Set(item.Value)
	c.syncCompletions(item.Value)
}

func (c *ChatPanel) selectCompletion(delta int) {
	items := c.completions.Get()
	if len(items) == 0 {
		return
	}
	idx := c.completionIndex.Get() + delta
	if idx < 0 {
		idx = len(items) - 1
	} else if idx >= len(items) {
		idx = 0
	}
	c.completionIndex.Set(idx)
}

func (c *ChatPanel) closeCompletions() {
	c.completions.Set(nil)
	c.completionIndex.Set(0)
}

func completionOpen(items []completionItem) bool {
	return len(items) > 0
}

func builtinCompletionItems() []completionItem {
	return []completionItem{
		{Value: "/new", Label: "/new", Description: "start a new conversation"},
		{Value: "/system ", Label: "/system", Description: "set system prompt", AcceptsArgs: true},
		{Value: "/model ", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Value: "/refresh", Label: "/refresh", Description: "refresh models"},
		{Value: "/reload", Label: "/reload", Description: "reload extensions while idle"},
		{Value: "/reasoning ", Label: "/reasoning", Description: "show or hide reasoning", AcceptsArgs: true},
		{Value: "/settings", Label: "/settings", Description: "show settings help"},
		{Value: "/exit", Label: "/exit", Description: "quit"},
	}
}

func limitCompletions(items []completionItem) []completionItem {
	const maxCompletions = 8
	if len(items) <= maxCompletions {
		return items
	}
	return items[:maxCompletions]
}

// KeyMap defines app-level keyboard shortcuts.
func (c *ChatPanel) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.On(gotui.KeyEscape, func(ke gotui.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.closeCompletions()
				return
			}
			if c.inputValue.Get() != "" {
				c.inputValue.Set("")
				return
			}
			ke.App().Stop()
		}),
		gotui.On(gotui.KeyTab, func(ke gotui.KeyEvent) {
			c.applySelectedCompletion()
		}),
		gotui.On(gotui.KeyUp, func(ke gotui.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.selectCompletion(-1)
				return
			}
			c.scrollY.Set(max(0, c.scrollY.Get()-2))
		}),
		gotui.On(gotui.KeyDown, func(ke gotui.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.selectCompletion(1)
				return
			}
			c.scrollY.Set(c.scrollY.Get() + 2)
		}),
		gotui.On(gotui.KeyCtrlC, func(ke gotui.KeyEvent) {
			if c.status.Get() == tauchat.ChatSessionStreaming {
				c.status.Set(tauchat.ChatSessionCancelling)
				c.sendCommand(tauchat.CancelChatRequestCommand{
					SessionID:   c.cfg.SessionID,
					RequestID:   c.activeRequestID.Get(),
					RequestedAt: time.Now().UTC(),
				})
				c.notice.Set("cancelling… Ctrl+C again to quit")
				return
			}
			ke.App().Stop()
		}),
		gotui.On(gotui.KeyCtrlR, func(ke gotui.KeyEvent) {
			c.showReasoning.Set(!c.showReasoning.Get())
		}),
		gotui.On(gotui.KeyPageUp, func(ke gotui.KeyEvent) {
			c.scrollY.Set(max(0, c.scrollY.Get()-10))
		}),
		gotui.On(gotui.KeyPageDown, func(ke gotui.KeyEvent) {
			c.scrollY.Set(c.scrollY.Get() + 10)
		}),
	}
}

func roleLabel(role tauchat.ChatRole) string {
	switch role {
	case tauchat.ChatRoleAssistant:
		return "Tau"
	case tauchat.ChatRoleTool:
		return "tool"
	case tauchat.ChatRoleSystem:
		return "system"
	default:
		return ""
	}
}

func roleRail(role tauchat.ChatRole) string {
	switch role {
	case tauchat.ChatRoleUser:
		return "│"
	case tauchat.ChatRoleAssistant:
		return "τ"
	case tauchat.ChatRoleTool:
		return "t"
	case tauchat.ChatRoleSystem:
		return "s"
	default:
		return "•"
	}
}

func roleStyle(role tauchat.ChatRole) gotui.Style {
	switch role {
	case tauchat.ChatRoleUser:
		return gotui.NewStyle().Foreground(theme.ColorNavyBlue)
	case tauchat.ChatRoleAssistant:
		return theme.BrandStyle()
	case tauchat.ChatRoleTool:
		return gotui.NewStyle().Foreground(theme.ColorGreen)
	case tauchat.ChatRoleSystem:
		return theme.DimStyle()
	default:
		return theme.DimStyle()
	}
}

func clamp(v, low, high int) int {
	if high < low {
		return low
	}
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func newID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}
