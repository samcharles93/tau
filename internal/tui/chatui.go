package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	gt "github.com/grindlemire/go-tui"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/internal/tui/views"
)

const (
	inlineTextAreaMaxRows   = 24
	inlineCompletionMaxRows = 4
	inlineHeight            = 6
)

// ChatPanel is the root go-tui component for the interactive chat UI.
type ChatPanel struct {
	ctx         context.Context
	runtime     tauchat.ChatRuntime
	chatSub     *eventbus.Subscriber[tauchat.ChatEvent]
	cfg         TUIConfig
	app         *gt.App
	notifyQueue *notify.Queue

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
	registryCommands     *gt.State[[]tauchat.CommandRef]
	activeRequestID      *gt.State[string]
	completions          *gt.State[[]completionItem]
	completionIndex      *gt.State[int]
	showSettings         *gt.State[bool]
	selectedModelIndex   *gt.State[int]
	settingsView         *views.SettingsView
	helpView             *views.HelpView
	showDebug            *gt.State[bool]
	debugView            *views.DebugView
	showDebugList        *gt.State[bool]
	debugListView        *views.DebugListView
	showSessionList      *gt.State[bool]
	showSessionInfo      *gt.State[bool]
	showSessionTree      *gt.State[bool]
	sessionListView      *views.SessionListView
	sessionTreeView      *views.SessionTreeView
	sessionSummaries     *gt.State[[]tauchat.SessionSummary]
	sessionListCursor    string
	dumpTreeOnNextRender bool
	input                *completionTextArea
	messageViewport      *gt.Element
	scrollToBottomFlag   bool
	streamWriter         *gt.StreamWriter
	streamContentWritten bool
	reasoningWritten     bool
	startupDone          bool
	lastSubmitTime       time.Time
	outgoingQueue        *gt.State[[]string]
	toolLogs             *gt.State[map[string]string]
}

// NewChatPanel creates a new go-tui chat panel with reactive state initialized.
func NewChatPanel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	chatSub *eventbus.Subscriber[tauchat.ChatEvent],
	cfg TUIConfig,
) *ChatPanel {
	commands := make(map[string]tauchat.ExtensionCommand)
	models := slices.Clone(cfg.AvailableModels)
	panel := &ChatPanel{
		ctx:                ctx,
		runtime:            runtime,
		chatSub:            chatSub,
		cfg:                cfg,
		notifyQueue:        notify.NewQueue(),
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
		registryCommands:   gt.NewState(slices.Clone(cfg.InitialCommands)),
		activeRequestID:    gt.NewState(""),
		completions:        gt.NewState(make([]completionItem, 0)),
		completionIndex:    gt.NewState(0),
		showSettings:       gt.NewState(false),
		showDebug:          gt.NewState(false),
		showDebugList:      gt.NewState(false),
		showSessionList:    gt.NewState(false),
		showSessionInfo:    gt.NewState(false),
		showSessionTree:    gt.NewState(false),
		sessionSummaries:   gt.NewState([]tauchat.SessionSummary{}),
		outgoingQueue:      gt.NewState([]string{}),
		selectedModelIndex: gt.NewState(0),
		toolLogs:           gt.NewState(make(map[string]string)),
	}
	panel.settingsView = views.NewSettingsView(
		panel.showSettings,
		cfg.Provider,
		panel.modelName,
		panel.availableModels,
		panel.selectedModelIndex,
		panel.showReasoning,
		func() string { return panel.cfg.SessionID },
		func(modelID string) {
			panel.handleModelCommand(modelID)
		},
	)
	panel.helpView = views.NewHelpView(panel.showHelp)
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
	c.registryCommands.BindApp(app)
	c.activeRequestID.BindApp(app)
	c.outgoingQueue.BindApp(app)
	c.completions.BindApp(app)
	c.completionIndex.BindApp(app)
	c.showSettings.BindApp(app)
	c.selectedModelIndex.BindApp(app)
	c.settingsView.BindApp(app)
	c.showDebug.BindApp(app)
	c.showDebugList.BindApp(app)
	c.showSessionList.BindApp(app)
	c.showSessionInfo.BindApp(app)
	c.showSessionTree.BindApp(app)
	c.sessionSummaries.BindApp(app)
	c.toolLogs.BindApp(app)
}

// Watchers bridges runtime event channels into the go-tui event loop.
func (c *ChatPanel) Watchers() []gt.Watcher {
	watchers := make([]gt.Watcher, 0, 6)
	if c.chatSub != nil {
		watchers = append(watchers, gt.Watch(c.chatSub.Events(), c.handleRuntimeEvent))
	}
	if c.input != nil {
		watchers = append(watchers, c.input.Watchers()...)
	}
	watchers = append(watchers,
		gt.OnChange(c.inputValue, c.handleInputValueChanged),
		gt.OnChange(c.showSettings, c.handleSettingsVisibilityChanged),
		gt.OnChange(c.showSessionTree, c.handleSessionTreeVisibilityChanged),
		gt.OnChange(c.showSessionList, c.handleSessionListVisibilityChanged),
		gt.OnChange(c.showDebug, c.handleDebugVisibilityChanged),
		gt.OnChange(c.showDebugList, c.handleDebugListVisibilityChanged),
		gt.OnChange(c.showHelp, c.handleHelpVisibilityChanged),
		gt.OnTimer(time.Second, c.applyNotifyQueue),
	)
	return watchers
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
		c.scrollToBottomFlag = true
		c.writeAssistantDelta(ev.Delta)
	case tauchat.ChatReasoningDeltaEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		c.streamingReasoning.Set(ev.Snapshot)
		c.scrollToBottomFlag = true
		if c.showReasoning.Get() {
			c.writeReasoningDelta(ev.Delta)
		}
	case tauchat.ChatToolExecutionStartedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		message := fmt.Sprintf("tool started: %s %s", ev.ToolName, ev.ArgumentsSummary)
		c.notice.Set(message)
		c.appendMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleSystem, Content: message})
	case tauchat.ChatToolExecutionCompletedEvent:
		if !c.matchesRequest(ev.SessionID, ev.RequestID) {
			return
		}
		statusColor := theme.ColorPaleGreen
		if ev.IsError {
			statusColor = theme.ColorYellow
		}
		message := fmt.Sprintf("tool completed: %s %s (%s)", ev.ToolName, ev.Status, ev.Duration)
		c.notice.Set(message)
		c.appendMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleSystem, Content: message})
		c.printStyledAbovef("%s", ansify(message, statusColor))
	case tauchat.ChatToolOutputEvent:
		c.toolLogs.Update(func(m map[string]string) map[string]string {
			m[ev.CallID] += ev.Chunk
			return m
		})
		c.scrollToBottomFlag = true
	case tauchat.ChatResponseCompletedEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		c.scrollToBottomFlag = true
		if !c.streamContentWritten {
			c.printLatestAssistantMessage(ev.State)
		}
		c.closeStream()
		c.syncState(ev.State)
		c.drainQueue()
	case tauchat.ChatResponseCancelledEvent:
		if ev.State.SessionID != c.cfg.SessionID {
			return
		}
		c.closeStream()
		c.syncState(ev.State)
		c.notice.Set("chat request cancelled")
		c.printStyledAbovef("%s", ansify("\nchat request cancelled", theme.ColorDimGray))
		c.drainQueue()
	case tauchat.ChatRuntimeErrorEvent:
		if ev.SessionID != "" && ev.SessionID != c.cfg.SessionID {
			return
		}
		c.closeStream()
		c.lastError.Set(ev.Message)
		c.notice.Set(ev.Message)
		c.status.Set(tauchat.ChatSessionIdle)
		c.printStyledAbovef("%s", ansify(fmt.Sprintf("\nerror: %s", ev.Message), theme.ColorRed))
		c.drainQueue()
	case tauchat.ChatNotificationEvent:
		c.notifyQueue.Push(notify.Notification{
			Message:  ev.Message,
			Level:    notifyLevelFromChat(ev.Level),
			Duration: notifyDurationFromChat(ev.Level),
		})
		c.applyNotifyQueue()
		if ev.Level == tauchat.ChatNotificationError {
			c.lastError.Set(ev.Message)
			c.printStyledAbovef("%s", ansify(fmt.Sprintf("\nerror: %s", ev.Message), theme.ColorRed))
		} else {
			c.printStyledAbovef("\n%s", ansify(ev.Message, theme.ColorDimGray))
		}
	case tauchat.ExtensionsReloadedEvent:
		c.setExtensionCommands(ev.Result.Commands)
		message := fmt.Sprintf("reloaded extensions: %d loaded", ev.Result.ExtensionCount)
		c.notice.Set(message)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
	case tauchat.ExtensionCommandsChangedEvent:
		c.setExtensionCommands(ev.Commands)
	case tauchat.CommandsChangedEvent:
		c.registryCommands.Set(ev.Commands)
	case tauchat.ExtensionCommandResultEvent:
		c.appendMessage(tauchat.ChatMessage{Role: tauchat.ChatRoleTool, Content: ev.Output})
	case tauchat.InteractivePromptRequestedEvent:
		message := ev.Title + ": " + ev.Message
		c.notice.Set(message)
		c.printStyledAbovef("\n%s", ansify(message, theme.ColorDimGray))
	case tauchat.SessionsListedEvent:
		c.sessionSummaries.Set(ev.Sessions)
		c.sessionListCursor = ev.NextCursor
		// Suppress text output when the tree view is active.
		if !c.showSessionTree.Get() {
			c.printSessionSummaries(ev.Sessions, ev.NextCursor)
		}
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

// applyNotifyQueue drains expired entries from the notification queue
// and updates the notice state with the current front.
func (c *ChatPanel) applyNotifyQueue() {
	if c.notifyQueue == nil {
		return
	}
	current := c.notifyQueue.Current()
	if current == nil {
		c.notice.Set("")
		return
	}
	c.notice.Set(current.Message)
}

// notifyLevelFromChat maps the chat notification level to the notify package level.
func notifyLevelFromChat(level tauchat.ChatNotificationLevel) notify.Level {
	switch level {
	case tauchat.ChatNotificationError:
		return notify.LevelError
	case tauchat.ChatNotificationWarn:
		return notify.LevelWarn
	default:
		return notify.LevelInfo
	}
}

// notifyDurationFromChat returns the auto-dismiss duration for a chat notification level.
// Errors do not auto-dismiss (0); warnings get 8 seconds; info gets 5 seconds.
func notifyDurationFromChat(level tauchat.ChatNotificationLevel) time.Duration {
	switch level {
	case tauchat.ChatNotificationError:
		return 0 // errors persist until dismissed by the user
	case tauchat.ChatNotificationWarn:
		return 8 * time.Second
	default:
		return 5 * time.Second
	}
}

func (c *ChatPanel) matchesRequest(sessionID, requestID string) bool {
	if sessionID != c.cfg.SessionID {
		return false
	}
	active := c.activeRequestID.Get()
	return active == "" || requestID == "" || active == requestID
}

func (c *ChatPanel) drainQueue() {
	var next string
	c.outgoingQueue.Update(func(q []string) []string {
		if len(q) == 0 {
			return q
		}
		next = q[0]
		return q[1:]
	})

	if next != "" {
		// Run in a goroutine because handleSubmit might trigger more events
		// and we want to avoid blocking the event loop.
		go func() {
			if c.app != nil {
				// We use app.QueueUpdate because we are updating state from a goroutine.
				c.app.QueueUpdate(func() {
					c.handleSubmit(next)
				})
			} else {
				// Fallback for tests or un-bound panel.
				c.handleSubmit(next)
			}
		}()
	}
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
	c.scrollToBottomFlag = true
	c.printMessage(message)
}

func (c *ChatPanel) scrollToBottom() {
	messageLines := len(c.messages.Get()) * 4
	streamingLines := strings.Count(c.streamingContent.Get(), "\n") + strings.Count(c.streamingReasoning.Get(), "\n")
	c.scrollY.Set(max(0, messageLines+streamingLines-1))
}

func (c *ChatPanel) handleInputValueChanged(value string) {
	c.syncCompletions(value)
	if c.cfg.InlineMode {
		c.adjustInlineHeight()
	}
}

// adjustInlineHeight grows the inline area to accommodate multi-line
// textarea input. Only used in inline mode.
func (c *ChatPanel) adjustInlineHeight() {
	if c.app == nil || c.app.IsInAlternateScreen() || c.input == nil {
		return
	}
	// textarea height + divider(1) + status bar(1) + buffer(1)
	h := c.input.Height() + 3
	if completions := len(c.completions.Get()); completions > 0 {
		h += min(completions, inlineCompletionMaxRows)
	}
	if strings.TrimSpace(c.lastError.Get()) != "" {
		h++
	}
	c.app.SetInlineHeight(max(h, inlineHeight))
}

// resetInlineHeight resets the inline area to its minimum height.
// Only meaningful in inline mode.
func (c *ChatPanel) resetInlineHeight() {
	if c.app == nil {
		return
	}
	c.app.SetInlineHeight(inlineHeight)
}

// Render builds the go-tui element tree. In inline mode the root is only the
// input widget; conversation output is printed/streamed above it into terminal
// scrollback. In full alternate-screen mode the whole screen is a chat viewport.
func (c *ChatPanel) Render(app *gt.App) *gt.Element {
	if !c.startupDone {
		c.startupDone = true
		// Startup hint shown only in inline mode; in full alternate screen
		// the chat viewport replaces the terminal scrollback.
		if c.cfg.InlineMode {
			app.QueuePrintAboveln(" ")
			app.QueuePrintAboveStyled("%s", ansify("Tau can explain its own features and look up its docs. Ask it about usage or configuration.", theme.ColorDimGray))
		}
	}

	if c.showSettings.Get() {
		return c.settingsView.Render(app)
	}
	if c.showSessionTree.Get() {
		return c.renderFullscreenSessionTree(app)
	}
	if c.showSessionList.Get() && c.sessionListView != nil {
		return c.sessionListView.Render(app)
	}
	if c.showDebugList.Get() && c.debugListView != nil {
		return c.debugListView.Render(app)
	}
	if c.showDebug.Get() && c.debugView != nil {
		return c.debugView.Render(app)
	}
	if c.showHelp.Get() && c.helpView != nil {
		return c.helpView.Render(app)
	}

	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
	)

	if c.cfg.InlineMode {
		// Inline mode keeps the original input-at-bottom layout with
		// streamed output above the inline frame.
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
		if queue := c.renderQueue(); queue != nil {
			root.AddChild(queue)
		}
		root.AddChild(gt.New(
			gt.WithText("───"+strings.Repeat("─", max(c.messageWidth()-3, 1))),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithHeight(1),
		))
		root.AddChild(c.renderInput(app))
	} else {
		// Full alternate-screen mode: messages fill the available space,
		// input/status sit at the bottom, and completions render just
		// above the input.
		root.AddChild(c.renderMessagesViewport(app))
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
		if queue := c.renderQueue(); queue != nil {
			root.AddChild(queue)
		}
		root.AddChild(gt.New(
			gt.WithText("───"+strings.Repeat("─", max(c.messageWidth()-3, 1))),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithHeight(1),
			gt.WithFlexShrink(0),
		))
		root.AddChild(c.renderInput(app))
	}
	root.AddChild(c.renderStatusBar())

	if c.dumpTreeOnNextRender {
		c.dumpTreeOnNextRender = false
		c.writeTreeDump(root)
	}

	return root
}

// renderFullscreenSessionTree returns the session tree dashboard for
// alternate-screen mode.
func (c *ChatPanel) renderFullscreenSessionTree(app *gt.App) *gt.Element {
	if c.sessionTreeView == nil {
		return gt.New(gt.WithText("tree view not initialised"))
	}
	return c.sessionTreeView.Render(app)
}

// alternateScreenStates returns every state that controls a full-screen
// alternate-screen view. Mutual exclusion is enforced so only one view is
// open at a time.
func (c *ChatPanel) alternateScreenStates() []*gt.State[bool] {
	return []*gt.State[bool]{
		c.showSettings,
		c.showSessionTree,
		c.showSessionList,
		c.showDebug,
		c.showDebugList,
		c.showHelp,
	}
}

func (c *ChatPanel) closeAlternateScreenViews(except *gt.State[bool]) {
	for _, s := range c.alternateScreenStates() {
		if s != except && s.Get() {
			s.Set(false)
		}
	}
}

func (c *ChatPanel) enterAlternateScreen(except *gt.State[bool]) {
	if c.app == nil {
		return
	}
	c.closeAlternateScreenViews(except)
	if !c.app.IsInAlternateScreen() {
		if err := c.app.EnterAlternateScreen(); err != nil {
			c.lastError.Set("enter alternate screen: " + err.Error())
		}
	}
}

func (c *ChatPanel) exitAlternateScreenIfIdle() {
	if c.app == nil {
		return
	}
	for _, s := range c.alternateScreenStates() {
		if s.Get() {
			return
		}
	}
	if c.app.IsInAlternateScreen() {
		if err := c.app.ExitAlternateScreen(); err != nil {
			c.lastError.Set(err.Error())
		}
	}
	c.resetInlineHeight()
}

func (c *ChatPanel) handleSettingsVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showSettings)
		return
	}
	c.exitAlternateScreenIfIdle()
}

func (c *ChatPanel) handleSessionTreeVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showSessionTree)
		return
	}
	c.exitAlternateScreenIfIdle()
}

func (c *ChatPanel) handleSessionListVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showSessionList)
		return
	}
	c.exitAlternateScreenIfIdle()
}

func (c *ChatPanel) handleDebugVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showDebug)
		return
	}
	c.exitAlternateScreenIfIdle()
}

func (c *ChatPanel) handleDebugListVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showDebugList)
		return
	}
	c.exitAlternateScreenIfIdle()
}

func (c *ChatPanel) handleHelpVisibilityChanged(open bool) {
	if open {
		c.enterAlternateScreen(c.showHelp)
		return
	}
	c.exitAlternateScreenIfIdle()
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

func (c *ChatPanel) renderQueue() *gt.Element {
	queue := c.outgoingQueue.Get()
	if len(queue) == 0 {
		return nil
	}

	el := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
	)

	for _, msg := range queue {
		el.AddChild(gt.New(
			gt.WithText("queued: "+msg),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithTruncate(true),
		))
	}
	return el
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
	w, _ := app.Size()
	if w < 40 {
		w = max(w, 40)
	}
	w -= 2 // Account for prompt "› "
	input := app.MountPersistent(c, 0, func() gt.Component {
		if c.input == nil {
			c.input = newCompletionTextArea(
				c.inputValue,
				c.completions,
				c.handleSubmit,
				c.handleSteerSubmit,
				c.popQueueToInput,
				func() { c.selectCompletion(-1) },
				func() { c.selectCompletion(1) },
				w,
			)
		}
		return c.input
	})
	// Update width reactively on terminal resize. Recreating the textarea
	// preserves the text but resets the cursor; resizing is infrequent.
	if c.input != nil {
		c.input.SetWidth(w)
	}
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
