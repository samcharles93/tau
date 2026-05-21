package tui

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/completions"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/pubsub"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	headerHeight    = 1
	statusBarHeight = 1
	inputHeight     = 3
	chromeHeight    = headerHeight + statusBarHeight + inputHeight + 2 // borders/padding

	ctrlCDebounceWindow = 1 * time.Second
)

// Config holds parameters for constructing the TUI model.
type Config struct {
	SessionID       string
	ModelName       string
	Endpoint        string
	AvailableModels []string
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

	// Theme.
	theme theme

	// Conversation state.
	turns            []turnBlock
	streamingContent string
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
		comp.SetArgumentCompletions("/model", cfg.AvailableModels)
	}

	return &Model{
		runtime:     runtime,
		eventSub:    sub,
		config:      cfg,
		viewport:    vp,
		input:       ta,
		spinner:     sp,
		completions: comp,
		theme:       t,
		status:      aimchat.ChatSessionIdle,
	}
}

// Init returns the initial commands: textarea cursor blink, spinner tick,
// and the event pump that bridges runtime events into the Bubble Tea loop.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Focus(), // returns cursor blink cmd; state already set in New()
		m.spinner.Tick,
		waitForRuntimeEvent(m.eventSub.Channel()),
	)
}

// Update handles all incoming messages and returns the (possibly mutated)
// model plus any commands to execute.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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

	// Header bar.
	header := m.theme.Header.Width(m.width).Render(
		fmt.Sprintf(" aim chat — %s (%s)", m.config.ModelName, m.config.Endpoint),
	)
	b.WriteString(header)
	b.WriteByte('\n')

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

	// Status bar.
	b.WriteString(m.renderStatusBar())

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

	// Viewport handles mouse wheel, page up/down from non-key sources.
	m.viewport, cmd = m.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return cmds
}

// handleKeyPress dispatches key events.
func (m *Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	// Ctrl+C: cancel in-flight → exit on double-press.
	if key.Code == 'c' && key.Mod == tea.ModCtrl {
		return m.handleCtrlC()
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
	if key.Code == tea.KeyEnter && key.Mod == 0 {
		return m.handleSubmit()
	}

	// Escape: close completions first, then clear input, then quit.
	if key.Code == tea.KeyEscape {
		if m.completions.IsOpen() {
			m.completions.Close()
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			m.completions.Close()
			return m, nil
		}
		return m, tea.Quit
	}

	// Page Up/Down → viewport scroll (mark user scrolled).
	if key.Code == tea.KeyPgUp || key.Code == tea.KeyPgDown {
		m.userScrolled = true
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// All other keys go to the textarea, then sync completions.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.completions.Sync(m.input.Value())
	return m, cmd
}

// handleCtrlC implements cancel-on-first, quit-on-second semantics.
// When idle: first press clears the input, second press quits.
// When streaming: first press cancels the request.
func (m *Model) handleCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()

	if m.status == aimchat.ChatSessionStreaming {
		m.lastCtrlC = now
		return m, m.sendCancel()
	}

	// If there's text in the input, clear it first.
	if strings.TrimSpace(m.input.Value()) != "" {
		m.input.Reset()
		m.completions.Close()
		m.lastCtrlC = now
		return m, nil
	}

	if now.Sub(m.lastCtrlC) < ctrlCDebounceWindow {
		return m, tea.Quit
	}

	m.lastCtrlC = now
	return m, nil
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
		role:    aimchat.ChatRoleUser,
		content: text,
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
		m.lastError = ""
		m.refreshViewport()
		return m, m.sendReset()

	case "/model":
		m.input.Reset()
		if len(parts) < 2 {
			m.lastError = "usage: /model <model-name>"
		} else {
			m.lastError = fmt.Sprintf("model switching to %q will be available in a future update", parts[1])
		}
		m.refreshViewport()
		return m, nil

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

	default:
		m.lastError = fmt.Sprintf("unknown command: %s", command)
		m.input.Reset()
		m.refreshViewport()
		return m, nil
	}
}

// handleCompletionSelection fills or executes the selected slash command.
func (m *Model) handleCompletionSelection(sel completions.SelectionMsg) (tea.Model, tea.Cmd) {
	cmd := sel.Command

	if cmd.AcceptsArgs {
		// Fill the input with the command name + trailing space for args.
		m.input.Reset()
		m.input.SetValue(cmd.Name + " ")
		// Move cursor to end.
		m.input.CursorEnd()
		return m, nil
	}

	// Execute immediately for commands that don't take args.
	m.input.Reset()
	return m.handleSlashCommand(cmd.Name)
}

// builtinCommands returns the set of built-in slash commands.
func builtinCommands() []completions.CommandDef {
	return []completions.CommandDef{
		{Name: "/new", Description: "Start a new conversation"},
		{Name: "/system", Description: "Set system prompt", AcceptsArgs: true},
		{Name: "/model", Description: "Switch model", AcceptsArgs: true},
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
					role:    aimchat.ChatRoleAssistant,
					content: m.streamingContent,
				})
				m.streamingContent = ""
			} else {
				m.syncTurnsFromState(ev.State)
			}
		}

	case aimchat.ChatResponseStartedEvent:
		m.status = aimchat.ChatSessionStreaming
		m.streamingContent = ""
		m.lastError = ""
		m.userScrolled = false

	case aimchat.ChatResponseDeltaEvent:
		m.streamingContent = ev.Snapshot

	case aimchat.ChatResponseCompletedEvent:
		m.status = aimchat.ChatSessionIdle
		if m.streamingContent != "" {
			m.turns = append(m.turns, turnBlock{
				role:    aimchat.ChatRoleAssistant,
				content: m.streamingContent,
			})
		}
		m.streamingContent = ""

	case aimchat.ChatResponseCancelledEvent:
		m.status = aimchat.ChatSessionIdle
		if m.streamingContent != "" {
			m.turns = append(m.turns, turnBlock{
				role:    aimchat.ChatRoleAssistant,
				content: m.streamingContent + "\n\n[cancelled]",
			})
		}
		m.streamingContent = ""

	case aimchat.ChatRuntimeErrorEvent:
		m.lastError = ev.Message
		if m.status == aimchat.ChatSessionStreaming || m.status == aimchat.ChatSessionCancelling {
			m.status = aimchat.ChatSessionIdle
			m.streamingContent = ""
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
			role:    msg.Role,
			content: msg.Content,
		})
	}
}

// refreshViewport rebuilds and sets the viewport content from structured turns.
func (m *Model) refreshViewport() {
	contentWidth := max(m.width-2, 20)
	content := renderConversation(m.turns, m.streamingContent, contentWidth, m.theme)

	if m.lastError != "" {
		if content != "" {
			content += "\n"
		}
		content += m.theme.Error.Render("Error: " + m.lastError)
	}

	m.viewport.SetContent(content)

	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
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
		left = m.theme.Dim.Render("cancelling…")
	default:
		left = m.theme.Help.Render("/new  /system  /exit")
	}

	right := m.theme.Dim.Render("ctrl+c cancel • esc quit")

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	bar := left + strings.Repeat(" ", gap) + right
	return m.theme.StatusBar.Width(m.width).Render(bar)
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

func generateID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b), nil
}
