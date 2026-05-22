package tui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/completions"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/pubsub"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/theme"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/tui/dialog"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/tui/notify"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	statusBarHeight = 1
	inputHeight     = 3
	chromeHeight    = statusBarHeight + inputHeight + 2 // borders/padding

	ctrlCDebounceWindow = 1 * time.Second
)

// ModelInfo holds discovery data for an available model.
type ModelInfo struct {
	ID    string
	URL   string
	Ready bool
}

// ModelRefresher is a function the app layer provides that re-discovers
// available models from MaaS. The TUI calls it asynchronously when the
// user runs /refresh. This keeps the TUI decoupled from maas/platform
// packages (per dependency rules).
type ModelRefresher func(ctx context.Context) ([]ModelInfo, error)

// Config holds parameters for constructing the TUI model.
type Config struct {
	SessionID       string
	ModelName       string
	Endpoint        string
	AvailableModels []ModelInfo
	NotifyBus       *pubsub.Bus[notify.Notification]
	RefreshModels   ModelRefresher
}

// Model is the root Bubble Tea model for the AIM chat TUI.
// All methods use pointer receivers so mutations persist across the
// Bubble Tea lifecycle (matching the pattern used by Crush).
type Model struct {
	// Core dependencies.
	runtime  *aimchat.Runtime
	eventSub *pubsub.Subscription[aimchat.ChatEvent]
	config   Config

	// Sub-models.
	viewport    viewport.Model
	input       textarea.Model
	spinner     spinner.Model
	completions *completions.Completions
	help        help.Model
	keys        keyMap

	// Theme.
	tuiTheme tuiTheme

	// Dialog overlay (command palette, model picker).
	overlay *dialog.Overlay

	// Conversation state.
	turns            []turnBlock
	streamingContent string
	streamMD         streamingMarkdown
	status           aimchat.ChatSessionStatus
	lastError        string

	// Dimensions.
	width  int
	height int
	ready  bool

	// Ctrl+C debounce: first press cancels, second press exits.
	lastCtrlC time.Time

	// Track whether the user has scrolled up (disable auto-scroll).
	userScrolled bool

	// Notification queue (transient status bar messages).
	notifications *notify.Queue
	notifySub     *pubsub.Subscription[notify.Notification]
}

// New creates a new TUI model wired to the given runtime.
// The textarea is focused during construction so state persists.
func New(runtime *aimchat.Runtime, sub *pubsub.Subscription[aimchat.ChatEvent], cfg Config) *Model {
	ta := textarea.New()
	ta.Placeholder = "Send a message…"
	ta.SetHeight(inputHeight)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // unlimited
	ta.Focus()       // set focus state during construction so it persists

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	t := newTheme()
	sp.Style = t.Spinner

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(24))
	vp.MouseWheelEnabled = true
	vp.SoftWrap = true
	vp.FillHeight = true

	comp := completions.New(t.CompletionNormal, t.CompletionSelected, t.Dim)
	comp.SetCommands(builtinCommands())
	if len(cfg.AvailableModels) > 0 {
		modelIDs := make([]string, 0, len(cfg.AvailableModels))
		for _, m := range cfg.AvailableModels {
			modelIDs = append(modelIDs, m.ID)
		}
		comp.SetArgumentCompletions("/model", modelIDs)
	}

	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(theme.ColorDimGray)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(theme.ColorDimGray)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(theme.ColorDimGray)
	h.Styles.FullKey = lipgloss.NewStyle().Foreground(theme.ColorDimGray)
	h.Styles.FullDesc = lipgloss.NewStyle().Foreground(theme.ColorDimGray)
	h.Styles.FullSeparator = lipgloss.NewStyle().Foreground(theme.ColorDimGray)

	m := &Model{
		runtime:       runtime,
		eventSub:      sub,
		config:        cfg,
		viewport:      vp,
		input:         ta,
		spinner:       sp,
		completions:   comp,
		overlay:       dialog.NewOverlay(),
		help:          h,
		keys:          newKeyMap(),
		tuiTheme:      t,
		status:        aimchat.ChatSessionIdle,
		notifications: notify.NewQueue(),
	}

	if cfg.NotifyBus != nil {
		nsub, err := cfg.NotifyBus.Subscribe("notifications", 16)
		if err == nil {
			m.notifySub = nsub
		}
	}

	return m
}

// Init returns the initial commands: textarea cursor blink, spinner tick,
// and the event pump that bridges runtime events into the Bubble Tea loop.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.input.Focus(), // returns cursor blink cmd; state already set in New()
		m.spinner.Tick,
		waitForRuntimeEvent(m.eventSub.Channel()),
	}
	if m.notifySub != nil {
		cmds = append(cmds, waitForNotification(m.notifySub.Channel()))
	}
	return tea.Batch(cmds...)
}

// Update handles all incoming messages and returns the (possibly mutated)
// model plus any commands to execute.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.recalculateLayout()
		m.ready = true
		m.refreshViewport()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)

	case runtimeEventMsg:
		m.handleRuntimeEvent(msg.event)
		m.refreshViewport()
		// Re-arm the event pump for the next event.
		cmds = append(cmds, waitForRuntimeEvent(m.eventSub.Channel()))
		return m, tea.Batch(cmds...)

	case runtimeClosedMsg:
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case notifyExpiredMsg:
		// Prune is automatic; schedule next tick if queue still has items.
		if !m.notifications.IsEmpty() {
			return m, m.scheduleNotifyTick()
		}
		return m, nil

	case helpExpiredMsg:
		m.help.ShowAll = false
		return m, nil

	case notifyReceivedMsg:
		m.notifications.Push(msg.notification)
		cmds = append(cmds, m.scheduleNotifyTick())
		if m.notifySub != nil {
			cmds = append(cmds, waitForNotification(m.notifySub.Channel()))
		}
		return m, tea.Batch(cmds...)

	case modelsRefreshedMsg:
		return m.handleModelsRefreshed(msg)
	}

	// Pass all other messages (paste, cursor blink, etc.) to sub-components.
	cmds = append(cmds, m.updateSubModels(msg)...)

	return m, tea.Batch(cmds...)
}

// View renders the full TUI screen.
func (m *Model) View() tea.View {
	if !m.ready {
		v := tea.NewView("Initializing…")
		v.AltScreen = true
		return v
	}

	var b strings.Builder

	// Conversation viewport.
	b.WriteString(m.viewport.View())
	b.WriteByte('\n')

	// Completions popup (rendered above the input area).
	if popup := m.completions.Render(m.width - 2); popup != "" {
		b.WriteString(popup)
		b.WriteByte('\n')
	}

	// Input textarea.
	b.WriteString(m.input.View())
	b.WriteByte('\n')

	// Status bar (bottom).
	b.WriteString(m.renderStatusBar())

	// Dialog overlay (rendered on top of everything).
	if m.overlay.HasDialogs() {
		base := b.String()
		overlay := m.overlay.View(m.width, m.height)
		if overlay != "" {
			// Place overlay in the vertical center.
			baseLines := strings.Split(base, "\n")
			overlayLines := strings.Split(overlay, "\n")
			startRow := max((len(baseLines)-len(overlayLines))/2, 1)
			for i, ol := range overlayLines {
				row := startRow + i
				if row < len(baseLines) {
					baseLines[row] = ol
				}
			}
			base = strings.Join(baseLines, "\n")
		}
		v := tea.NewView(base)
		v.AltScreen = true
		return v
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// updateSubModels forwards messages to textarea and viewport.
func (m *Model) updateSubModels(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	// Textarea handles paste messages, cursor blink, etc.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Sync completions after any input mutation (handles paste, etc.).
	m.completions.Sync(m.input.Value())
	m.adjustViewportHeight()

	// Viewport handles mouse wheel, page up/down from non-key sources.
	m.viewport, cmd = m.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return cmds
}

// handleKeyPress dispatches key events.
func (m *Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()

	// Ctrl+C: cancel in-flight → exit on double-press (always active).
	if k.Code == 'c' && k.Mod == tea.ModCtrl {
		return m.handleCtrlC()
	}

	// If a dialog is open, route all keys to it.
	if m.overlay.HasDialogs() {
		action := m.overlay.Update(msg)
		return m.handleDialogAction(action)
	}

	// Ctrl+P: open command palette.
	if k.Code == 'p' && k.Mod == tea.ModCtrl {
		m.openCommandPalette()
		return m, nil
	}

	// Ctrl+K: open model picker.
	if k.Code == 'k' && k.Mod == tea.ModCtrl {
		m.openModelPicker()
		return m, nil
	}

	// Toggle help (only when input is empty so '?' can be typed normally).
	if k.Code == '?' && k.Mod == 0 && strings.TrimSpace(m.input.Value()) == "" {
		if m.help.ShowAll {
			m.help.ShowAll = false
			return m, nil
		}
		m.help.ShowAll = true
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return helpExpiredMsg{}
		})
	}

	// If completions popup is open, let it handle navigation keys.
	if m.completions.IsOpen() {
		result, consumed := m.completions.Update(msg)
		if consumed {
			if sel, ok := result.(completions.SelectionMsg); ok {
				return m.handleCompletionSelection(sel)
			}
			return m, nil
		}
	}

	// Enter (without modifiers) submits the prompt.
	if k.Code == tea.KeyEnter && k.Mod == 0 {
		return m.handleSubmit()
	}

	// Escape: dismiss help → close completions → clear input → show quit hint.
	if k.Code == tea.KeyEscape {
		if m.help.ShowAll {
			m.help.ShowAll = false
			return m, nil
		}
		if m.completions.IsOpen() {
			m.completions.Close()
			m.adjustViewportHeight()
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			return m, nil
		}
		return m, m.quitHintCmd()
	}

	// Ctrl+Backspace: delete previous word.
	if k.Code == tea.KeyBackspace && k.Mod == tea.ModCtrl {
		m.deleteWordBackward()
		m.completions.Sync(m.input.Value())
		m.adjustViewportHeight()
		return m, nil
	}

	// Page Up/Down → viewport scroll (mark user scrolled).
	if k.Code == tea.KeyPgUp || k.Code == tea.KeyPgDown {
		m.userScrolled = true
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// All other keys go to the textarea, then sync completions.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.completions.Sync(m.input.Value())
	m.adjustViewportHeight()
	return m, cmd
}

// handleCtrlC always warns before quitting.
// First press: clears input/completions and shows quit hint.
// Second press within the debounce window: quits.
func (m *Model) handleCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()

	// Second press within debounce window → quit.
	if now.Sub(m.lastCtrlC) < ctrlCDebounceWindow {
		return m, tea.Quit
	}

	m.lastCtrlC = now

	// Clear any input or completions on the first press.
	if strings.TrimSpace(m.input.Value()) != "" {
		m.input.Reset()
	}
	m.completions.Close()

	// Show the quit hint.
	return m, m.quitHintCmd()
}

// quitHintCmd pushes the "Ctrl+C to quit" notification.
func (m *Model) quitHintCmd() tea.Cmd {
	return m.pushNotification(notify.Notification{
		Message:  "Ctrl+C to quit",
		Level:    notify.LevelWarn,
		Duration: 3 * time.Second,
	})
}

// handleSubmit sends the user's prompt or executes a slash command.
func (m *Model) handleSubmit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}

	// Slash commands.
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// Can't submit while streaming.
	if m.status == aimchat.ChatSessionStreaming || m.status == aimchat.ChatSessionCancelling {
		return m, nil
	}

	// Add the user turn immediately for responsiveness.
	m.turns = append(m.turns, turnBlock{
		role:     aimchat.ChatRoleUser,
		content:  text,
		finished: true,
	})
	m.input.Reset()
	m.userScrolled = false
	m.refreshViewport()

	return m, m.sendPrompt(text)
}

// handleSlashCommand processes /commands.
func (m *Model) handleSlashCommand(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(text)
	command := strings.ToLower(parts[0])

	switch command {
	case "/exit", "/quit":
		return m, tea.Quit

	case "/new":
		m.input.Reset()
		m.turns = nil
		m.streamingContent = ""
		m.streamMD.Reset()
		m.lastError = ""
		m.refreshViewport()
		return m, m.sendReset()

	case "/model":
		m.input.Reset()
		if len(parts) < 2 {
			m.lastError = "usage: /model <model-name>"
			m.refreshViewport()
			return m, nil
		}
		modelName := parts[1]
		modelRef, ok := m.findModel(modelName)
		if !ok {
			m.lastError = fmt.Sprintf("model %q not found", modelName)
			m.refreshViewport()
			return m, nil
		}
		if !modelRef.Ready {
			m.lastError = fmt.Sprintf("model %q is not ready", modelName)
			m.refreshViewport()
			return m, nil
		}
		m.config.ModelName = modelRef.ID
		m.lastError = ""
		m.refreshViewport()
		return m, m.sendUpdateModel(aimchat.ChatModelRef{ID: modelRef.ID, URL: modelRef.URL})

	case "/system":
		m.input.Reset()
		if len(parts) < 2 {
			m.lastError = "usage: /system <prompt text>"
			m.refreshViewport()
			return m, nil
		}
		newPrompt := strings.TrimPrefix(text, parts[0]+" ")
		m.lastError = ""
		return m, m.sendUpdateSystem(newPrompt)

	case "/refresh":
		m.input.Reset()
		return m, m.refreshModelsCmd()

	default:
		m.lastError = fmt.Sprintf("unknown command: %s", command)
		m.input.Reset()
		m.refreshViewport()
		return m, nil
	}
}

// handleCompletionSelection fills or executes the selected slash command.
func (m *Model) handleCompletionSelection(sel completions.SelectionMsg) (tea.Model, tea.Cmd) {
	if sel.IsArg {
		// Argument selected (e.g. a model name) — execute the full command.
		m.input.Reset()
		m.completions.Close()
		return m.handleSlashCommand(sel.Text)
	}

	cmd := sel.Command

	if cmd.AcceptsArgs {
		// Fill the input with the command name + trailing space for args.
		m.input.Reset()
		m.input.SetValue(cmd.Name + " ")
		m.input.CursorEnd()
		m.completions.Sync(m.input.Value())
		return m, nil
	}

	// Execute immediately for commands that don't take args.
	m.input.Reset()
	m.completions.Close()
	return m.handleSlashCommand(cmd.Name)
}

// --- Dialog helpers ---

// openCommandPalette shows the command palette overlay.
func (m *Model) openCommandPalette() {
	items := []dialog.PaletteItem{
		{ID: "new", Label: "/new", Description: "Start a new conversation"},
		{ID: "model", Label: "/model", Description: "Switch model"},
		{ID: "system", Label: "/system", Description: "Set system prompt"},
		{ID: "exit", Label: "/exit", Description: "Quit the app"},
	}
	m.overlay.Open(dialog.NewPalette("Commands", items))
}

// openModelPicker shows the model picker overlay.
func (m *Model) openModelPicker() {
	items := make([]dialog.ModelItem, 0, len(m.config.AvailableModels))
	for _, mi := range m.config.AvailableModels {
		items = append(items, dialog.ModelItem{
			ID:    mi.ID,
			URL:   mi.URL,
			Ready: mi.Ready,
		})
	}
	if len(items) == 0 {
		m.lastError = "no models available — try /refresh"
		return
	}
	m.overlay.Open(dialog.NewModelPicker(items))
}

// handleDialogAction processes the result of a dialog interaction.
func (m *Model) handleDialogAction(action dialog.Action) (tea.Model, tea.Cmd) {
	switch a := action.(type) {
	case nil:
		return m, nil
	case dialog.CloseAction:
		return m, nil
	case dialog.SelectAction:
		m.overlay.CloseFront()
		return m.executePaletteAction(a)
	case dialog.ModelSelectedAction:
		m.overlay.CloseFront()
		m.config.ModelName = a.ID
		m.lastError = ""
		m.refreshViewport()
		return m, m.sendUpdateModel(aimchat.ChatModelRef{ID: a.ID, URL: a.URL})
	}
	return m, nil
}

// executePaletteAction runs the action selected from the command palette.
func (m *Model) executePaletteAction(a dialog.SelectAction) (tea.Model, tea.Cmd) {
	switch a.ID {
	case "new":
		m.turns = nil
		m.streamingContent = ""
		m.streamMD.Reset()
		m.lastError = ""
		m.refreshViewport()
		return m, m.sendReset()
	case "model":
		m.openModelPicker()
		return m, nil
	case "system":
		// Put /system in the input so the user can type the prompt.
		m.input.SetValue("/system ")
		m.input.CursorEnd()
		m.completions.Sync(m.input.Value())
		return m, nil
	case "exit":
		return m, tea.Quit
	}
	return m, nil
}

// builtinCommands returns the set of built-in slash commands.
func builtinCommands() []completions.CommandDef {
	return []completions.CommandDef{
		{Name: "/new", Description: "Start a new conversation"},
		{Name: "/system", Description: "Set system prompt", AcceptsArgs: true},
		{Name: "/model", Description: "Switch model", AcceptsArgs: true},
		{Name: "/refresh", Description: "Refresh app state (models, skills, etc.)"},
		{Name: "/exit", Description: "Quit the app"},
	}
}

// handleRuntimeEvent processes an event from the chat runtime.
func (m *Model) handleRuntimeEvent(event aimchat.ChatEvent) {
	switch ev := event.(type) {
	case aimchat.ChatSessionSnapshotEvent:
		m.status = ev.State.Status
		if ev.State.Status == aimchat.ChatSessionIdle {
			// Safety net: if we have streaming content but state went idle
			// (e.g., CompletedEvent was missed), finalize the turn.
			if m.streamingContent != "" {
				m.turns = append(m.turns, turnBlock{
					role:     aimchat.ChatRoleAssistant,
					content:  m.streamingContent,
					finished: true,
				})
				m.streamingContent = ""
				m.streamMD.Reset()
			} else {
				m.syncTurnsFromState(ev.State)
			}
		}

	case aimchat.ChatResponseStartedEvent:
		m.status = aimchat.ChatSessionStreaming
		m.streamingContent = ""
		m.streamMD.Reset()
		m.lastError = ""
		m.userScrolled = false

	case aimchat.ChatResponseDeltaEvent:
		m.streamingContent = ev.Snapshot

	case aimchat.ChatResponseCompletedEvent:
		m.status = aimchat.ChatSessionIdle
		if m.streamingContent != "" {
			m.turns = append(m.turns, turnBlock{
				role:     aimchat.ChatRoleAssistant,
				content:  m.streamingContent,
				finished: true,
			})
		}
		m.streamingContent = ""
		m.streamMD.Reset()

	case aimchat.ChatResponseCancelledEvent:
		m.status = aimchat.ChatSessionIdle
		if m.streamingContent != "" {
			m.turns = append(m.turns, turnBlock{
				role:     aimchat.ChatRoleAssistant,
				content:  m.streamingContent + "\n\n[cancelled]",
				finished: true,
			})
		}
		m.streamingContent = ""
		m.streamMD.Reset()

	case aimchat.ChatRuntimeErrorEvent:
		m.lastError = ev.Message
		if m.status == aimchat.ChatSessionStreaming || m.status == aimchat.ChatSessionCancelling {
			m.status = aimchat.ChatSessionIdle
			m.streamingContent = ""
			m.streamMD.Reset()
		}
	}
}

// syncTurnsFromState rebuilds turns from the authoritative session state.
func (m *Model) syncTurnsFromState(state aimchat.ChatSessionState) {
	m.turns = make([]turnBlock, 0, len(state.Messages))
	for _, msg := range state.Messages {
		if msg.Role == aimchat.ChatRoleSystem {
			continue
		}
		m.turns = append(m.turns, turnBlock{
			role:     msg.Role,
			content:  msg.Content,
			finished: true,
		})
	}
}

// refreshViewport rebuilds and sets the viewport content from structured turns.
func (m *Model) refreshViewport() {
	// Recalculate viewport height to account for current completions popup.
	m.adjustViewportHeight()

	contentWidth := max(m.width-2, 20)
	content := renderConversation(m.turns, m.streamingContent, &m.streamMD, contentWidth, m.tuiTheme)

	if m.lastError != "" {
		if content != "" {
			content += "\n"
		}
		content += m.tuiTheme.Error.Render("Error: " + m.lastError)
	}

	m.viewport.SetContent(content)

	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// adjustViewportHeight recalculates viewport height based on completions popup.
func (m *Model) adjustViewportHeight() {
	completionLines := m.completions.Height()
	viewportHeight := max(m.height-chromeHeight-completionLines, 1)
	m.viewport.SetHeight(viewportHeight)
}

// recalculateLayout adjusts sub-model dimensions after a window resize.
func (m *Model) recalculateLayout() {
	completionLines := m.completions.Height()
	viewportHeight := max(m.height-chromeHeight-completionLines, 1)

	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(viewportHeight)
	m.input.SetWidth(m.width - 2)

	// Invalidate cached renders since width changed.
	for i := range m.turns {
		m.turns[i].rendered = ""
	}
}

// renderStatusBar builds the bottom status line.
func (m *Model) renderStatusBar() string {
	var left string
	switch m.status {
	case aimchat.ChatSessionStreaming:
		left = m.spinner.View() + " streaming…"
	case aimchat.ChatSessionCancelling:
		left = m.tuiTheme.Dim.Render("cancelling…")
	default:
		left = m.help.View(m.keys)
	}

	var right string
	if n := m.notifications.Current(); n != nil {
		style := m.tuiTheme.Dim
		switch n.Level {
		case notify.LevelWarn:
			style = m.tuiTheme.Help
		case notify.LevelError:
			style = m.tuiTheme.Error
		}
		right = style.Render(n.Message)
	} else {
		right = m.tuiTheme.Dim.Render(m.config.ModelName + " (" + m.config.Endpoint + ")")
	}

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	bar := left + strings.Repeat(" ", gap) + right
	return m.tuiTheme.StatusBar.Width(m.width).Render(bar)
}

// --- Runtime command helpers (wrapped as tea.Cmd for side-effect isolation) ---

func (m *Model) sendPrompt(text string) tea.Cmd {
	runtime := m.runtime
	sessionID := m.config.SessionID

	return func() tea.Msg {
		requestID, err := generateID("req")
		if err != nil {
			return nil
		}
		_ = runtime.Send(aimchat.SubmitChatPromptCommand{
			SessionID:   sessionID,
			RequestID:   requestID,
			Prompt:      text,
			SubmittedAt: time.Now().UTC(),
		})
		return nil
	}
}

func (m *Model) sendCancel() tea.Cmd {
	runtime := m.runtime
	sessionID := m.config.SessionID

	return func() tea.Msg {
		_ = runtime.Send(aimchat.CancelChatRequestCommand{
			SessionID:   sessionID,
			RequestedAt: time.Now().UTC(),
		})
		return nil
	}
}

func (m *Model) sendReset() tea.Cmd {
	runtime := m.runtime
	sessionID := m.config.SessionID

	return func() tea.Msg {
		_ = runtime.Send(aimchat.ResetChatSessionCommand{
			SessionID:   sessionID,
			RequestedAt: time.Now().UTC(),
		})
		return nil
	}
}

func (m *Model) sendUpdateSystem(prompt string) tea.Cmd {
	runtime := m.runtime
	sessionID := m.config.SessionID

	return func() tea.Msg {
		_ = runtime.Send(aimchat.UpdateChatSessionCommand{
			SessionID: sessionID,
			Patch:     aimchat.ChatSessionPatch{SystemPrompt: &prompt},
		})
		return nil
	}
}

func (m *Model) sendUpdateModel(ref aimchat.ChatModelRef) tea.Cmd {
	runtime := m.runtime
	sessionID := m.config.SessionID

	return func() tea.Msg {
		_ = runtime.Send(aimchat.UpdateChatSessionCommand{
			SessionID: sessionID,
			Patch:     aimchat.ChatSessionPatch{Model: &ref},
		})
		return nil
	}
}

func (m *Model) findModel(name string) (ModelInfo, bool) {
	nameLower := strings.ToLower(name)
	for _, mi := range m.config.AvailableModels {
		if strings.ToLower(mi.ID) == nameLower {
			return mi, true
		}
	}
	return ModelInfo{}, false
}

// deleteWordBackward removes the word before the cursor in the textarea.
func (m *Model) deleteWordBackward() {
	val := m.input.Value()
	if val == "" {
		return
	}

	// Walk backwards past trailing whitespace, then past the word.
	i := len(val)
	for i > 0 && val[i-1] == ' ' {
		i--
	}
	for i > 0 && val[i-1] != ' ' {
		i--
	}
	m.input.Reset()
	m.input.SetValue(val[:i])
	m.input.CursorEnd()
}

// pushNotification enqueues a notification and schedules the expiry tick.
func (m *Model) pushNotification(n notify.Notification) tea.Cmd {
	m.notifications.Push(n)
	return m.scheduleNotifyTick()
}

// scheduleNotifyTick returns a tea.Cmd that fires notifyExpiredMsg when
// the front notification expires.
func (m *Model) scheduleNotifyTick() tea.Cmd {
	expiresAt := m.notifications.ExpiresAt()
	if expiresAt.IsZero() {
		return nil
	}
	d := max(time.Until(expiresAt), 0)
	return tea.Tick(d, func(time.Time) tea.Msg {
		return notifyExpiredMsg{}
	})
}

func generateID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// refreshModelsCmd returns a tea.Cmd that asynchronously re-discovers
// available models and delivers the result as a modelsRefreshedMsg.
func (m *Model) refreshModelsCmd() tea.Cmd {
	refresher := m.config.RefreshModels
	if refresher == nil {
		return func() tea.Msg {
			return modelsRefreshedMsg{err: fmt.Errorf("model refresh not available")}
		}
	}
	return func() tea.Msg {
		models, err := refresher(context.Background())
		return modelsRefreshedMsg{models: models, err: err}
	}
}

// handleModelsRefreshed processes the results of an async model refresh.
func (m *Model) handleModelsRefreshed(msg modelsRefreshedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.pushNotification(notify.Notification{
			Message:  "Model refresh failed: " + msg.err.Error(),
			Level:    notify.LevelError,
			Duration: 5 * time.Second,
		})
	}

	m.config.AvailableModels = msg.models

	// Update completions with the new model list.
	modelIDs := make([]string, 0, len(msg.models))
	for _, mi := range msg.models {
		modelIDs = append(modelIDs, mi.ID)
	}
	m.completions.SetArgumentCompletions("/model", modelIDs)
	m.refreshViewport()

	return m, m.pushNotification(notify.Notification{
		Message:  fmt.Sprintf("Refreshed: %d models available", len(msg.models)),
		Level:    notify.LevelInfo,
		Duration: 3 * time.Second,
	})
}
