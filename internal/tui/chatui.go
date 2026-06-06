package tui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	gt "github.com/grindlemire/go-tui"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/internal/tui/views"
)

const (
	inlineMinHeight         = 3
	inlineTextAreaMaxRows   = 24
	inlineCompletionMaxRows = 4
	inlineMaxHeight         = inlineTextAreaMaxRows + inlineCompletionMaxRows + 2
)

type completionItem struct {
	Value       string
	Label       string
	Description string
	AcceptsArgs bool
}

type completionTextArea struct {
	textarea    *gt.TextArea
	completions *gt.State[[]completionItem]
	onUp        func()
	onDown      func()
}

func newCompletionTextArea(
	value *gt.State[string],
	completions *gt.State[[]completionItem],
	onSubmit func(string),
	onUp func(),
	onDown func(),
) *completionTextArea {
	return &completionTextArea{
		textarea: gt.NewTextArea(
			gt.WithTextAreaValue(value),
			gt.WithTextAreaWidth(120),
			gt.WithTextAreaMaxHeight(inlineTextAreaMaxRows),
			gt.WithTextAreaPlaceholder("Send a message…"),
			gt.WithTextAreaPlaceholderStyle(theme.DimStyle()),
			gt.WithTextAreaTextStyle(theme.BodyStyle()),
			gt.WithTextAreaFocusColor(theme.ColorPurple),
			gt.WithTextAreaAutoFocus(true),
			gt.WithTextAreaOnSubmit(onSubmit),
		),
		completions: completions,
		onUp:        onUp,
		onDown:      onDown,
	}
}

func (i *completionTextArea) BindApp(app *gt.App) { i.textarea.BindApp(app) }

func (i *completionTextArea) Render(app *gt.App) *gt.Element { return i.textarea.Render(app) }

func (i *completionTextArea) Watchers() []gt.Watcher { return i.textarea.Watchers() }

func (i *completionTextArea) IsFocused() bool { return i.textarea.IsFocused() }

func (i *completionTextArea) Focus() { i.textarea.Focus() }

func (i *completionTextArea) Clear() { i.textarea.Clear() }

func (i *completionTextArea) SetText(value string) { i.textarea.SetText(value) }

func (i *completionTextArea) Text() string { return i.textarea.Text() }

func (i *completionTextArea) Height() int { return i.textarea.Height() }

func (i *completionTextArea) KeyMap() gt.KeyMap {
	if len(i.completions.Get()) == 0 {
		return i.textarea.KeyMap()
	}
	km := make(gt.KeyMap, 0, len(i.textarea.KeyMap()))
	for _, binding := range i.textarea.KeyMap() {
		if binding.Pattern.Key == gt.KeyUp || binding.Pattern.Key == gt.KeyDown {
			continue
		}
		km = append(km, binding)
	}
	km = append(km,
		gt.OnFocused(gt.KeyUp, func(gt.KeyEvent) { i.onUp() }),
		gt.OnFocused(gt.KeyDown, func(gt.KeyEvent) { i.onDown() }),
	)
	return km
}

// ChatPanel is the root go-tui component for the interactive chat UI.
type ChatPanel struct {
	ctx       context.Context
	runtime   tauchat.ChatRuntime
	eventSub  *pubsub.Subscription[tauchat.ChatEvent]
	cfg       Config
	app       *gt.App
	notifySub *pubsub.Subscription[notify.Notification]

	messages             *gt.State[[]tauchat.ChatMessage]
	streamingContent     *gt.State[string]
	streamingReasoning   *gt.State[string]
	inputValue           *gt.State[string]
	scrollY              *gt.State[int]
	status               *gt.State[tauchat.ChatSessionStatus]
	lastError            *gt.State[string]
	notice               *gt.State[string]
	showHelp             *gt.State[bool]
	showReasoning        *gt.State[bool]
	modelName            *gt.State[string]
	availableModels      *gt.State[[]tauchat.ChatModelRef]
	extensionCommands    *gt.State[map[string]tauchat.ExtensionCommand]
	activeRequestID      *gt.State[string]
	completions          *gt.State[[]completionItem]
	completionIndex      *gt.State[int]
	showSettings         *gt.State[bool]
	settingsModal        *gt.Modal
	showDebug            *gt.State[bool]
	debugView            *views.DebugView
	showDebugList        *gt.State[bool]
	debugListView        *views.DebugListView
	showSessionList      *gt.State[bool]
	showSessionInfo      *gt.State[bool]
	sessionListView      *views.SessionListView
	sessionSummaries     *gt.State[[]tauchat.SessionSummary]
	sessionListCursor    string
	dumpTreeOnNextRender bool
	input                *completionTextArea
	streamWriter         *gt.StreamWriter
	streamContentWritten bool
	reasoningWritten     bool
	startupDone          bool
	lastSubmitTime       time.Time
}

// NewChatPanel creates a new go-tui chat panel with reactive state initialized.
func NewChatPanel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	eventSub *pubsub.Subscription[tauchat.ChatEvent],
	notifySub *pubsub.Subscription[notify.Notification],
	cfg Config,
) *ChatPanel {
	commands := make(map[string]tauchat.ExtensionCommand)
	models := slices.Clone(cfg.AvailableModels)
	panel := &ChatPanel{
		ctx:                ctx,
		runtime:            runtime,
		eventSub:           eventSub,
		notifySub:          notifySub,
		cfg:                cfg,
		messages:           gt.NewState(make([]tauchat.ChatMessage, 0)),
		streamingContent:   gt.NewState(""),
		streamingReasoning: gt.NewState(""),
		inputValue:         gt.NewState(""),
		scrollY:            gt.NewState(0),
		status:             gt.NewState(tauchat.ChatSessionIdle),
		lastError:          gt.NewState(""),
		notice:             gt.NewState(""),
		showHelp:           gt.NewState(false),
		showReasoning:      gt.NewState(cfg.ShowReasoning),
		modelName:          gt.NewState(cfg.ModelName),
		availableModels:    gt.NewState(models),
		extensionCommands:  gt.NewState(commands),
		activeRequestID:    gt.NewState(""),
		completions:        gt.NewState(make([]completionItem, 0)),
		completionIndex:    gt.NewState(0),
		showSettings:       gt.NewState(false),
		showDebug:          gt.NewState(false),
		showDebugList:      gt.NewState(false),
		showSessionList:    gt.NewState(false),
		showSessionInfo:    gt.NewState(false),
		sessionSummaries:   gt.NewState([]tauchat.SessionSummary{}),
	}
	panel.settingsModal = gt.NewModal(
		gt.WithModalOpen(panel.showSettings),
		gt.WithModalBackdrop("dim"),
		gt.WithModalTrapFocus(true),
		gt.WithModalElementOptions(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithJustify(gt.JustifyCenter),
			gt.WithAlign(gt.AlignCenter),
		),
	)
	return panel
}

// BindApp binds all reactive state to the running app.
func (c *ChatPanel) BindApp(app *gt.App) {
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
	c.showDebug.BindApp(app)
	c.showDebugList.BindApp(app)
	c.showSessionList.BindApp(app)
	c.showSessionInfo.BindApp(app)
	c.sessionSummaries.BindApp(app)
}

// Watchers bridges runtime and notification channels into the go-tui event loop.
func (c *ChatPanel) Watchers() []gt.Watcher {
	watchers := make([]gt.Watcher, 0, 4)
	if c.eventSub != nil && c.eventSub.Channel() != nil {
		watchers = append(watchers, gt.Watch(c.eventSub.Channel(), c.handleRuntimeEvent))
	}
	if c.notifySub != nil && c.notifySub.Channel() != nil {
		watchers = append(watchers, gt.Watch(c.notifySub.Channel(), c.handleNotification))
	}
	watchers = append(watchers,
		gt.OnChange(c.inputValue, c.handleInputValueChanged),
		gt.OnChange(c.showSettings, c.handleSettingsVisibilityChanged),
	)
	return watchers
}

func (c *ChatPanel) handleNotification(n notify.Notification) {
	message := strings.TrimSpace(n.Message)
	if message == "" {
		return
	}
	c.notice.Set(message)
	if n.Level == notify.LevelError {
		c.lastError.Set(message)
		c.printStyledAbovef("%s", ansify(fmt.Sprintf("\nerror: %s", message), theme.ColorRed))
		return
	}
	c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
}

func (c *ChatPanel) handleRuntimeEvent(event tauchat.ChatEvent) {
	switch ev := event.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		c.syncState(ev.State)
	case tauchat.ChatResponseStartedEvent:
		if ev.SessionID != c.cfg.SessionID {
			return
		}
		c.closeStream()
		c.activeRequestID.Set(ev.RequestID)
		c.status.Set(tauchat.ChatSessionStreaming)
		c.lastError.Set("")
	case tauchat.ChatResponseDeltaEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.streamingContent.Set(ev.Snapshot)
		c.writeAssistantDelta(ev.Delta)
	case tauchat.ChatReasoningDeltaEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.streamingReasoning.Set(ev.Snapshot)
		if c.showReasoning.Get() {
			c.writeReasoningDelta(ev.Delta)
		}
	case tauchat.ChatToolExecutionStartedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		message := fmt.Sprintf("\ntool started: %s %s", ev.ToolName, ev.ArgumentsSummary)
		c.notice.Set(message)
		c.printStyledAbovef("%s", ansify(message, theme.ColorDimGray))
	case tauchat.ChatToolExecutionCompletedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		message := fmt.Sprintf("tool completed: %s %s (%s)", ev.ToolName, ev.Status, ev.Duration)
		c.notice.Set(message)
		c.printStyledAbovef("%s", ansify(message, theme.ColorDimGray))
	case tauchat.ChatResponseCompletedEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		if !c.streamContentWritten {
			c.printLatestAssistantMessage(ev.State)
		}
		c.closeStream()
		c.syncState(ev.State)
	case tauchat.ChatResponseCancelledEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		c.closeStream()
		c.syncState(ev.State)
		c.notice.Set("chat request cancelled")
		c.printStyledAbovef("%s", ansify("\nchat request cancelled", theme.ColorDimGray))
	case tauchat.ChatRuntimeErrorEvent:
		if ev.SessionID != "" && ev.SessionID != c.cfg.SessionID {
			return
		}
		c.closeStream()
		c.lastError.Set(ev.Message)
		c.notice.Set(ev.Message)
		c.status.Set(tauchat.ChatSessionIdle)
		c.printStyledAbovef("%s", ansify(fmt.Sprintf("\nerror: %s", ev.Message), theme.ColorRed))
	case tauchat.ChatNotificationEvent:
		c.notice.Set(ev.Message)
		if ev.Level == tauchat.ChatNotificationError {
			c.lastError.Set(ev.Message)
			c.printStyledAbovef("%s", ansify(fmt.Sprintf("\nerror: %s", ev.Message), theme.ColorRed))
			return
		}
		c.printStyledAbovef("\n%s", ansify(ev.Message, theme.ColorDimGray))
	case tauchat.ExtensionsReloadedEvent:
		c.setExtensionCommands(ev.Result.Commands)
		message := fmt.Sprintf("reloaded extensions: %d loaded", ev.Result.ExtensionCount)
		c.notice.Set(message)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
	case tauchat.ExtensionCommandsChangedEvent:
		c.setExtensionCommands(ev.Commands)
	case tauchat.ExtensionCommandResultEvent:
		c.appendMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleTool, Content: ev.Output})
	case tauchat.InteractivePromptRequestedEvent:
		message := ev.Title + ": " + ev.Message
		c.notice.Set(message)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
	case tauchat.SessionsListedEvent:
		c.sessionSummaries.Set(ev.Sessions)
		c.sessionListCursor = ev.NextCursor
		c.printSessionSummaries(ev.Sessions, ev.NextCursor)
		if c.sessionListView != nil {
			c.sessionListView.SetCursor(ev.NextCursor, ev.NextCursor != "")
		}
	case tauchat.SessionLoadedEvent:
		c.cfg.SessionID = ev.State.SessionID
		c.syncState(ev.State)
		message := fmt.Sprintf("Session %s loaded (%d messages)", ev.State.SessionID, len(ev.State.Messages))
		c.notice.Set(message)
		c.showSessionList.Set(false)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
		c.printSessionMessages(ev.State.Messages)
	case tauchat.SessionDeletedEvent:
		message := "Session deleted: " + ev.SessionID
		c.notice.Set(message)
		c.showSessionInfo.Set(false)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
	case tauchat.SessionExportedEvent:
		if ev.Path != "" {
			c.notice.Set(fmt.Sprintf("Session exported to %s", ev.Path))
		} else {
			c.notice.Set("Session exported to stdout")
		}
		c.printAbovef("%s", c.notice.Get())
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
	c.printMessage(message)
}

func (c *ChatPanel) scrollToBottom() {
	messageLines := len(c.messages.Get()) * 4
	streamingLines := strings.Count(c.streamingContent.Get(), "\n") + strings.Count(c.streamingReasoning.Get(), "\n")
	c.scrollY.Set(max(0, messageLines+streamingLines-1))
}

func (c *ChatPanel) printAbovef(format string, args ...any) {
	if c.app == nil {
		return
	}
	c.app.PrintAboveln(format, args...)
}

func (c *ChatPanel) ensureStreamWriter() *gt.StreamWriter {
	if c.app == nil {
		return nil
	}
	if c.streamWriter == nil {
		c.streamWriter = c.app.StreamAbove()
	}
	return c.streamWriter
}

func (c *ChatPanel) writeAssistantDelta(delta string) {
	if delta == "" {
		return
	}
	w := c.ensureStreamWriter()
	if w == nil {
		return
	}
	if !c.streamContentWritten && !c.reasoningWritten {
		_, _ = w.Write([]byte("\n"))
	}
	if c.reasoningWritten && c.streamContentWritten {
		_, _ = w.Write([]byte("\n"))
	}
	_, _ = w.Write([]byte(delta))
	c.streamContentWritten = true
}

func (c *ChatPanel) writeReasoningDelta(delta string) {
	if delta == "" {
		return
	}
	w := c.ensureStreamWriter()
	if w == nil {
		return
	}
	if !c.reasoningWritten {
		_, _ = w.Write([]byte("Reasoning: "))
		c.reasoningWritten = true
	}
	_, _ = w.Write([]byte(delta))
}

func (c *ChatPanel) closeStream() {
	if c.streamWriter != nil {
		if c.streamContentWritten || c.reasoningWritten {
			_, _ = c.streamWriter.Write([]byte("\n\n"))
		}
		_ = c.streamWriter.Close()
	}
	c.streamWriter = nil
	c.streamContentWritten = false
	c.reasoningWritten = false
}

func (c *ChatPanel) printLatestAssistantMessage(state tauchat.ChatSessionState) {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		if msg.Role == tauchat.ChatRoleAssistant && strings.TrimSpace(msg.Content) != "" {
			c.printMessage(msg)
			return
		}
	}
}

func (c *ChatPanel) printSessionMessages(messages []tauchat.ChatMessage) {
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" {
			continue
		}
		c.printMessage(msg)
	}
}

func (c *ChatPanel) printMessage(msg tauchat.ChatMessage) {
	if c.showReasoning.Get() && strings.TrimSpace(msg.ReasoningContent) != "" {
		c.printStyledAbovef("\n%s\n", ansify("reasoning:\n"+msg.ReasoningContent, theme.ColorDimGray))
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	switch msg.Role {
	case tauchat.ChatRoleUser:
		c.printStyledAbovef("%s", c.userMessageBlock(msg.Content))
	case tauchat.ChatRoleAssistant:
		c.printStyledAbovef("\n%s\n\n", msg.Content)
	default:
		c.printStyledAbovef("\n%s\n\n", ansify(fmt.Sprintf("%s: %s", messageRoleLabel(msg.Role), msg.Content), theme.ColorDimGray))
	}
}

func (c *ChatPanel) printStyledAbovef(format string, args ...any) {
	if c.app == nil {
		return
	}
	c.app.PrintAboveStyled(format, args...)
}

// ansify wraps text in ANSI SGR sequences matching the given go-tui Style.
// The result can be passed to PrintAboveStyled which preserves ANSI escapes.
func ansify(text string, c gt.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("\033[38;2;%d;%d;%dm%s\033[0m", r, g, b, text)
}

func (c *ChatPanel) userMessageBlock(text string) string {
	lines := wrapUserMessageLines(text, max(1, c.messageWidth()-2))
	rows := make([]string, 0, len(lines)+2)
	rows = append(rows, ansiBackgroundLine("", theme.ColorLightGray, theme.ColorGray800))
	for _, line := range lines {
		rows = append(rows, ansiBackgroundLine(" "+line, theme.ColorLightGray, theme.ColorGray800))
	}
	rows = append(rows, ansiBackgroundLine("", theme.ColorLightGray, theme.ColorGray800))
	return strings.Join(rows, "\n")
}

func (c *ChatPanel) messageWidth() int {
	if c.app != nil {
		width, _ := c.app.Size()
		if width > 0 {
			return width
		}
	}
	width := 1
	for line := range strings.SplitSeq(c.inputValue.Get(), "\n") {
		width = max(width, len([]rune(line))+2)
	}
	return width
}

func wrapUserMessageLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return out
}

func ansiBackgroundLine(text string, fg gt.Color, bg gt.Color) string {
	fr, fgG, fb := fg.RGB()
	br, bgG, bb := bg.RGB()
	return fmt.Sprintf("\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm%s\033[K\033[0m", fr, fgG, fb, br, bgG, bb, text)
}

func (c *ChatPanel) printSessionSummaries(summaries []tauchat.SessionSummary, nextCursor string) {
	if len(summaries) == 0 {
		c.printStyledAbovef("\n%s", ansify("Sessions: no saved sessions", theme.ColorDimGray))
		return
	}
	var b strings.Builder
	b.WriteString("Sessions:\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "- %s · %d messages · %s\n", summary.ID, summary.MessageCount, summary.ModelID)
	}
	if nextCursor != "" {
		b.WriteString("More sessions available.")
	}
	c.printStyledAbovef("\n%s", ansify(strings.TrimRight(b.String(), "\n"), theme.ColorDimGray))
}

func (c *ChatPanel) printSessionInfo(summary tauchat.SessionSummary) {
	c.printStyledAbovef("\n%s", ansify(fmt.Sprintf(
		"Session %s\nModel: %s\nProvider: %s\nMessages: %d\nTokens: %d\nCreated: %s\nUpdated: %s",
		summary.ID, summary.ModelID, summary.Provider,
		summary.MessageCount, summary.TotalTokens,
		summary.CreatedAt.Format(time.RFC3339),
		summary.UpdatedAt.Format(time.RFC3339),
	), theme.ColorDimGray))
}

func (c *ChatPanel) handleInputValueChanged(value string) {
	c.syncCompletions(value)
	c.adjustInlineHeight()
}

func (c *ChatPanel) adjustInlineHeight() {
	if c.app == nil || c.app.IsInAlternateScreen() {
		return
	}
	height := inlineMinHeight
	if c.input != nil {
		height = c.input.Height() + 1
	} else if value := c.inputValue.Get(); value != "" {
		height = strings.Count(value, "\n") + 2
	}
	if completions := len(c.completions.Get()); completions > 0 {
		height += min(completions, inlineCompletionMaxRows)
	}
	if strings.TrimSpace(c.lastError.Get()) != "" {
		height++
	}
	c.app.SetInlineHeight(clamp(height, inlineMinHeight, inlineMaxHeight))
}

func (c *ChatPanel) handleSettingsVisibilityChanged(open bool) {
	if c.app == nil {
		return
	}
	if open {
		if !c.app.IsInAlternateScreen() {
			if err := c.app.EnterAlternateScreen(); err != nil {
				c.lastError.Set("enter settings screen: " + err.Error())
			}
		}
		return
	}
	if c.app.IsInAlternateScreen() {
		if err := c.app.ExitAlternateScreen(); err != nil {
			c.lastError.Set("exit settings screen: " + err.Error())
		}
	}
	c.adjustInlineHeight()
}

// Render builds the go-tui element tree. In inline mode the root is only the
// input widget; conversation output is printed/streamed above it into terminal
// scrollback. Settings temporarily switch to the alternate screen and render as
// a conventional full-screen modal.
func (c *ChatPanel) Render(app *gt.App) *gt.Element {
	if !c.startupDone {
		c.startupDone = true
		// Schedule a blank line after the first render to mitigate the
		// visual screen-clearing effect of go-tui's initial inline frame.
		app.QueuePrintAboveln(" ")
	}

	if c.showSettings.Get() && app.IsInAlternateScreen() {
		return c.renderFullscreenSettings(app)
	}

	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
	)

	if completions := c.renderCompletions(); completions != nil {
		root.AddChild(completions)
	}
	if errMsg := strings.TrimSpace(c.lastError.Get()); errMsg != "" {
		root.AddChild(gt.New(
			gt.WithText("error: "+errMsg),
			gt.WithTextStyle(theme.ErrorStyle()),
			gt.WithTruncate(true),
		))
	}
	root.AddChild(c.renderInput(app))
	root.AddChild(c.renderStatusBar())

	if c.dumpTreeOnNextRender {
		c.dumpTreeOnNextRender = false
		c.writeTreeDump(root)
	}

	return root
}

func (c *ChatPanel) renderFullscreenSettings(app *gt.App) *gt.Element {
	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
	)
	root.AddChild(c.renderSettingsModal(app))
	return root
}

func (c *ChatPanel) writeTreeDump(root *gt.Element) {
	f, err := os.OpenFile("tree.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		c.lastError.Set("failed to write tree.log: " + err.Error())
		return
	}
	defer f.Close()
	c.dumpElementTree(root, 0, f)
}

func (c *ChatPanel) dumpElementTree(el *gt.Element, depth int, f *os.File) {
	if el == nil {
		return
	}
	compType := "nil"
	isKeyListener := false
	if el.Component() != nil {
		compType = fmt.Sprintf("%T", el.Component())
		_, isKeyListener = el.Component().(gt.KeyListener)
	}
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(f, "%s- Element: hidden=%v, overlay=%v, focusable=%v, tabStop=%v, component=%s, isKeyListener=%v\n",
		indent, el.Hidden(), el.IsOverlay(), el.IsFocusable(), el.IsTabStop(), compType, isKeyListener)
	for _, child := range el.Children() {
		c.dumpElementTree(child, depth+1, f)
	}
}

func (c *ChatPanel) renderMessage(msg tauchat.ChatMessage) *gt.Element {
	block := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithGap(1),
	)
	block.AddChild(gt.New(
		gt.WithWidth(1),
		gt.WithText(roleRail(msg.Role)),
		gt.WithTextStyle(roleStyle(msg.Role)),
	))

	body := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexGrow(1),
	)
	if label := roleLabel(msg.Role); label != "" {
		body.AddChild(gt.New(
			gt.WithText(label),
			gt.WithTextStyle(roleStyle(msg.Role).Bold()),
		))
	}
	if c.showReasoning.Get() && msg.ReasoningContent != "" {
		body.AddChild(gt.New(
			gt.WithText("Reasoning\n"+msg.ReasoningContent),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithWrap(true),
		))
	}
	if msg.Content != "" {
		body.AddChild(gt.New(
			gt.WithText(msg.Content),
			gt.WithTextStyle(theme.BodyStyle()),
			gt.WithWrap(true),
		))
	}
	block.AddChild(body)
	return block
}

func (c *ChatPanel) renderInput(app *gt.App) *gt.Element {
	inputContainer := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithWidthPercent(100),
		gt.WithFlexShrink(0),
	)
	inputContainer.AddChild(gt.New(
		gt.WithText("› "),
		gt.WithTextStyle(theme.BrandStyle()),
		gt.WithFlexShrink(0),
	))
	input := app.MountPersistent(c, 0, func() gt.Component {
		if c.input == nil {
			c.input = newCompletionTextArea(
				c.inputValue,
				c.completions,
				c.handleSubmit,
				func() { c.selectCompletion(-1) },
				func() { c.selectCompletion(1) },
			)
		}
		return c.input
	})
	inputContainer.AddChild(input)
	return inputContainer
}

func (c *ChatPanel) renderStatusBar() *gt.Element {
	statusBar := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithWidthPercent(100),
		gt.WithFlexShrink(0),
	)
	statusBar.AddChild(gt.New(
		gt.WithText("τ tau"),
		gt.WithTextStyle(theme.BrandStyle()),
		gt.WithFlexShrink(0),
	))
	if notice := strings.TrimSpace(c.notice.Get()); notice != "" {
		statusBar.AddChild(gt.New(
			gt.WithText(" · "+notice),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithTruncate(true),
		))
	}
	statusBar.AddChild(gt.New(gt.WithFlexGrow(1)))
	modelStatus := c.modelName.Get()
	if c.cfg.Provider != "" {
		modelStatus += " · " + c.cfg.Provider
	}
	if modelStatus != "" {
		statusBar.AddChild(gt.New(
			gt.WithText(modelStatus+" · "),
			gt.WithTextStyle(theme.BrandStyle()),
			gt.WithTruncate(true),
		))
	}
	return statusBar
}

func (c *ChatPanel) renderSettingsModal(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(c, 1, func() gt.Component {
		return c.settingsModal
	})
	modalEl.AddChild(c.buildSettingsContent())
	return modalEl
}

func (c *ChatPanel) buildSettingsContent() *gt.Element {
	content := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(56),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BrandStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
	)
	content.AddChild(gt.New(
		gt.WithText("Settings"),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	content.AddChild(gt.New(
		gt.WithText("Model: "+c.modelName.Get()),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithTruncate(true),
	))
	if c.cfg.Provider != "" {
		content.AddChild(gt.New(
			gt.WithText("Provider: "+c.cfg.Provider),
			gt.WithTextStyle(theme.BodyStyle()),
			gt.WithTruncate(true),
		))
	}
	reasoning := "off"
	if c.showReasoning.Get() {
		reasoning = "on"
	}
	content.AddChild(gt.New(
		gt.WithText("Reasoning: "+reasoning+"  (Ctrl+R toggles)"),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithTruncate(true),
	))
	content.AddChild(gt.New(
		gt.WithText("Available models:"),
		gt.WithTextStyle(theme.BodyStyle()),
	))
	modelList := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithGap(0),
		gt.WithMaxHeight(12),
		gt.WithScrollable(gt.ScrollVertical),
	)
	currentModel := c.modelName.Get()
	for _, model := range c.availableModels.Get() {
		prefix := "  "
		style := theme.DimStyle()
		if model.ID == currentModel {
			prefix = "› "
			style = theme.BrandStyle()
		} else if !model.Ready {
			style = theme.DimStyle().Italic()
		}
		label := prefix + model.ID
		if !model.Ready {
			label += " (not ready)"
		}
		modelList.AddChild(gt.New(
			gt.WithText(label),
			gt.WithTextStyle(style),
			gt.WithTruncate(true),
		))
	}
	content.AddChild(modelList)
	content.AddChild(gt.New(
		gt.WithText("Esc closes this dialog."),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))
	return content
}

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
			c.sendCommand(tauchat.LoadSessionCommand{SessionID: summary.ID})
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

func (c *ChatPanel) renderCompletions() *gt.Element {
	items := c.completions.Get()
	if len(items) == 0 {
		return nil
	}
	selected := clamp(c.completionIndex.Get(), 0, len(items)-1)
	container := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexShrink(0),
		gt.WithGap(0),
	)
	// Compute visible window around the selected index.
	maxRows := min(inlineCompletionMaxRows, len(items))
	startRow := max(selected-maxRows/2, 0)
	if end := startRow + maxRows; end > len(items) {
		startRow = len(items) - maxRows
	}
	endRow := startRow + maxRows
	for i := startRow; i < endRow; i++ {
		item := items[i]
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
		container.AddChild(gt.New(
			gt.WithText(line),
			gt.WithTextStyle(style),
			gt.WithTruncate(true),
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

	argPrefix := parts[1]
	switch commandPrefix {
	case "model":
		return c.modelCompletions(strings.TrimSpace(argPrefix))
	case "reasoning":
		return c.reasoningCompletions(strings.TrimSpace(argPrefix))
	case "debug":
		return c.debugCompletions(argPrefix)
	default:
		return nil
	}
}

func (c *ChatPanel) commandCompletions(prefix string) []completionItem {
	commands := builtinCompletionItems(c.cfg.Debug)
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
	return matches
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
	return items
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

func (c *ChatPanel) debugCompletions(arg string) []completionItem {
	parts := strings.Fields(arg)

	// Case 1: user typed "/debug" or "/debug " or "/debug sub" -> suggest subcommands
	if len(parts) == 0 || (len(parts) == 1 && !strings.HasSuffix(arg, " ")) {
		prefix := ""
		if len(parts) == 1 {
			prefix = strings.ToLower(parts[0])
		}
		options := []completionItem{
			{Value: "/debug components", Label: "components", Description: "list/preview TUI components"},
		}
		matches := make([]completionItem, 0, len(options))
		for _, item := range options {
			if prefix == "" || strings.HasPrefix(item.Label, prefix) {
				matches = append(matches, item)
			}
		}
		return matches
	}

	// Case 2: user typed "/debug components " or "/debug components name"
	sub := strings.ToLower(parts[0])
	if sub == "components" {
		prefix := ""
		if len(parts) > 1 {
			prefix = strings.ToLower(parts[1])
		}
		options := []completionItem{}
		for _, name := range components.ListNames() {
			options = append(options, completionItem{
				Value:       "/debug components " + name,
				Label:       name,
				Description: "Debug component: " + name,
			})
		}
		matches := make([]completionItem, 0, len(options))
		for _, item := range options {
			if prefix == "" || strings.HasPrefix(item.Label, prefix) {
				matches = append(matches, item)
			}
		}
		return matches
	}

	return nil
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

func (c *ChatPanel) applySelectedCompletion() (completed string, acceptsArgs bool) {
	items := c.completions.Get()
	if len(items) == 0 {
		return "", false
	}
	item := items[clamp(c.completionIndex.Get(), 0, len(items)-1)]
	c.setInputText(item.Value)
	return item.Value, item.AcceptsArgs
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

func builtinCompletionItems(debug bool) []completionItem {
	items := []completionItem{
		{Value: "/new", Label: "/new", Description: "start a new conversation"},
		{Value: "/system ", Label: "/system", Description: "set system prompt", AcceptsArgs: true},
		{Value: "/model ", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Value: "/refresh", Label: "/refresh", Description: "refresh models"},
		{Value: "/reload", Label: "/reload", Description: "reload extensions while idle"},
		{Value: "/reasoning ", Label: "/reasoning", Description: "show or hide reasoning", AcceptsArgs: true},
		{Value: "/settings", Label: "/settings", Description: "show settings help"},
		{Value: "/session ", Label: "/session", Description: "manage saved sessions", AcceptsArgs: true},
		{Value: "/resume", Label: "/resume", Description: "resume a saved session"},
	}
	if debug {
		items = append(items, completionItem{Value: "/debug ", Label: "/debug", Description: "preview TUI components (developer)", AcceptsArgs: true})
	}
	items = append(items, completionItem{Value: "/exit", Label: "/exit", Description: "quit"})
	return items
}

// KeyMap defines app-level keyboard shortcuts.
func (c *ChatPanel) KeyMap() gt.KeyMap {
	km := gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.closeCompletions()
				return
			}
			if c.inputValue.Get() != "" {
				c.clearInput()
				return
			}
			c.notice.Set("Press Ctrl+C to quit")
		}),
		gt.On(gt.KeyTab, func(ke gt.KeyEvent) {
			c.applySelectedCompletion()
		}),
		gt.On(gt.KeyCtrlC, func(ke gt.KeyEvent) {
			switch {
			case c.status.Get() == tauchat.ChatSessionStreaming:
				c.status.Set(tauchat.ChatSessionCancelling)
				c.sendCommand(tauchat.CancelChatRequestCommand{
					SessionID:   c.cfg.SessionID,
					RequestID:   c.activeRequestID.Get(),
					RequestedAt: time.Now().UTC(),
				})
				c.notice.Set("cancelling… Ctrl+C again to quit")
			case c.inputValue.Get() != "":
				c.clearInput()
			default:
				ke.App().Stop()
			}
		}),
		gt.On(gt.KeyCtrlR, func(ke gt.KeyEvent) {
			c.showReasoning.Set(!c.showReasoning.Get())
		}),
	}
	if len(c.completions.Get()) > 0 {
		km = append(km,
			gt.OnPreemptStop(gt.KeyUp, func(gt.KeyEvent) { c.selectCompletion(-1) }),
			gt.OnPreemptStop(gt.KeyDown, func(gt.KeyEvent) { c.selectCompletion(1) }),
		)
	}
	return km
}

func messageRoleLabel(role tauchat.ChatRole) string {
	switch role {
	case tauchat.ChatRoleTool:
		return "tool"
	case tauchat.ChatRoleSystem:
		return "system"
	default:
		return "message"
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

func roleStyle(role tauchat.ChatRole) gt.Style {
	switch role {
	case tauchat.ChatRoleUser:
		return gt.NewStyle().Foreground(theme.ColorNavyBlue)
	case tauchat.ChatRoleAssistant:
		return theme.BrandStyle()
	case tauchat.ChatRoleTool:
		return gt.NewStyle().Foreground(theme.ColorGreen)
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
