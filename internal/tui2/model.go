package tui2

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- model ------------------------------------------------------------------

// model is the root Bubbletea model for the chat TUI.
type model struct {
	ctx     context.Context
	runtime tauchat.ChatRuntime
	chatSub *eventbus.Subscriber[tauchat.ChatEvent]

	sessionID string
	modelName string
	provider  string

	// Terminal dimensions (updated on WindowSizeMsg).
	width  int
	height int

	// Conversation state — stored as raw content string fed to the viewport.
	viewport   viewport.Model
	streaming  string // current streaming text delta
	reasoning  string // current reasoning delta
	inResponse bool   // true while a response is in progress

	// Tool state.
	tools []toolState // active tool calls in display order

	// Input state.
	input      string   // current line buffer
	history    []string // submitted inputs for up/down recall
	historyIdx int      // -1 = not navigating; 0..len(history) = navigating

	// Completion state (tab cycling).
	compWords []string // current completion candidates
	compIdx   int      // cycling index

	// Viewport content — rendered lines, built incrementally.
	renderedLines []string

	// Status / transient.
	statusText      string // one-line status bar (model @ provider only)
	notification    string // transient notification (Phase 1 compat)
	notificationGen int    // bumped every time notification is set; guards clear race
	notifyQ         *notify.Queue

	// Model / provider state (populated by run.go).
	availableModels []tauchat.ChatModelRef
	refresh         func(context.Context) ([]tauchat.ChatModelRef, error)
	showReasoning   bool
	reasoningEffort string
	ctxWindow       int // context window size for % display

	// Extension commands (populated from ExtensionCommandsChangedEvent).
	extensionCommands map[string]tauchat.ExtensionCommand

	// Usage tracking.
	usage *metrics.UsageTracker

	// Configuration.
	webURL string
	debug  bool

	// Session management.
	sessionSummaries []tauchat.SessionSummary

	// Turn management.
	turnQueue  []string // queued prompts behind a running turn
	lastSubmit time.Time

	// Steering.
	steering bool

	// Interactive prompts.
	activePrompt *tauchat.InteractivePromptRequestedEvent
	promptQueue  []tauchat.InteractivePromptRequestedEvent

	// Plugin views.
	panels map[string]pluginPanel

	// Bash mode.
	bashRunning bool
	bashCallID  string
}

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"
}

type pluginPanel struct {
	id      string
	title   string
	content string
}

// --- constructor -----------------------------------------------------------

func newModel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	chatSub *eventbus.Subscriber[tauchat.ChatEvent],
	sessionID, modelName, provider string,
	availableModels []tauchat.ChatModelRef,
	refresh func(context.Context) ([]tauchat.ChatModelRef, error),
	usage *metrics.UsageTracker,
	webURL string,
	debug bool,
) *model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true
	return &model{
		ctx:               ctx,
		runtime:           runtime,
		chatSub:           chatSub,
		sessionID:         sessionID,
		modelName:         modelName,
		provider:          provider,
		viewport:          vp,
		historyIdx:        -1,
		availableModels:   availableModels,
		refresh:           refresh,
		usage:             usage,
		webURL:            webURL,
		debug:             debug,
		notifyQ:           notify.NewQueue(),
		extensionCommands: make(map[string]tauchat.ExtensionCommand),
		panels:            make(map[string]pluginPanel),
	}
}

// --- Bubbletea model interface ---------------------------------------------

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		readNextEvent(m.chatSub),
		func() tea.Msg { return startupMsg{m.sessionID, m.modelName, m.provider} },
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle quit request from the program (Ctrl+C signal, etc.).
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		// Reserve space for header (~3 lines), streaming text, separator,
		// input line, and status bar (~5 lines total).
		vpHeight := max(msg.Height-5, 4)
		m.viewport.SetHeight(vpHeight)
		return m, nil

	case tea.PasteMsg:
		m.input += msg.Content
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.KeyReleaseMsg:
		return m, nil

	case tea.KeyMsg:
		// Dispatch known key types from the KeyMsg interface.
		if kp, ok := msg.(tea.KeyPressMsg); ok {
			return m, m.handleKey(kp)
		}
		return m, nil

	case chatEventMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, readNextEvent(m.chatSub)) // re-arm
		if eventCmd := m.handleChatEvent(msg.event); eventCmd != nil {
			cmds = append(cmds, eventCmd)
		}
		return m, tea.Batch(cmds...)

	case chatEventsClosedMsg:
		m.notification = "event stream closed — exiting"
		return m, tea.Quit

	case clearNotificationMsg:
		if msg.gen == m.notificationGen {
			m.notification = ""
		}
		return m, nil

	case sendResultMsg:
		if msg.err != nil {
			m.inResponse = false
			m.notification = fmt.Sprintf("send failed: %v", msg.err)
		}
		return m, nil

	case refreshResultMsg:
		if msg.err != nil {
			return m, m.setNotification("refresh failed: " + msg.err.Error())
		}
		m.availableModels = msg.models
		return m, m.setNotification(fmt.Sprintf("refreshed: %d models available", len(msg.models)))

	case startupMsg:
		m.sessionID = msg.sessionID
		m.modelName = msg.modelName
		m.provider = msg.provider
		return m, nil

	default:
		return m, nil
	}
}

func (m *model) View() tea.View {
	var sb strings.Builder

	// Header.
	sb.WriteString(headerStyle.Render(" tau — Bubbletea v2 (experimental)"))
	sb.WriteString("\n\n")

	// Plugin panel (if active).
	if p := m.activePanel(); p != nil {
		sb.WriteString(panelStyle.Render("┌─ " + p.title + " ─┐"))
		sb.WriteString("\n")
		sb.WriteString(p.content)
		sb.WriteString("\n")
		sb.WriteString(panelStyle.Render("└" + strings.Repeat("─", min(m.width, 40)) + "┘"))
		sb.WriteString("\n\n")
	}

	// Messages — rendered through the viewport (scrollable, no cap).
	sb.WriteString(m.viewport.View())

	// Streaming text (in-progress assistant response).
	if m.reasoning != "" && m.showReasoning {
		sb.WriteString(reasoningStyle.Render("💭 " + m.reasoning))
		sb.WriteString("\n")
	}
	if m.streaming != "" {
		sb.WriteString(streamStyle.Render(m.streaming))
	}

	// Tool calls.
	for _, t := range m.tools {
		sb.WriteString("\n")
		sb.WriteString(renderTool(t))
	}
	if len(m.tools) > 0 {
		sb.WriteString("\n")
	}

	// Interactive prompt (modal).
	if m.activePrompt != nil {
		sb.WriteString("\n")
		sb.WriteString(renderPrompt(m.activePrompt))
		sb.WriteString("\n")
	}

	// Completion dropdown.
	if comps := m.computeCompletions(); len(comps) > 0 && !m.inResponse {
		sb.WriteString("\n")
		sb.WriteString(renderCompletions(comps))
	}

	// Notification (Phase 1 compat).
	if m.notification != "" {
		sb.WriteString("\n")
		sb.WriteString(notifyStyle.Render("📢 " + m.notification))
	}

	sb.WriteString("\n")

	// Separator before input area.
	sepWidth := m.width
	if sepWidth <= 0 {
		sepWidth = 80
	}
	sb.WriteString(strings.Repeat("─", sepWidth))
	sb.WriteString("\n")

	// Input area.
	if m.activePrompt != nil {
		sb.WriteString(inputStyle.Render("❓ responding to prompt…"))
	} else if m.inResponse || m.bashRunning {
		if m.bashRunning {
			sb.WriteString(inputStyle.Render("⚡ bash running… (esc to cancel)"))
		} else if m.steering {
			sb.WriteString(inputStyle.Render("🎯 steering…"))
		} else {
			sb.WriteString(inputStyle.Render("⏳ waiting for response…"))
		}
	} else if m.input != "" {
		sb.WriteString(inputStyle.Render("> " + m.input))
	} else {
		sb.WriteString(inputStyle.Render("> "))
	}
	sb.WriteString("\n")

	// Status bar — rich, segmented.
	if m.width > 0 {
		sb.WriteString(m.computeStatusBar())
	}

	return tea.NewView(sb.String())
}

// --- key handling ----------------------------------------------------------

func (m *model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// Interactive prompt active: route keys to prompt handler.
	if m.activePrompt != nil {
		return m.handlePromptKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return tea.Quit

	case "ctrl+d":
		if m.input == "" {
			return tea.Quit
		}
		m.input = ""
		return nil

	case "ctrl+s":
		return m.handleSteer()

	case "esc":
		if m.bashRunning {
			return m.cancelBash()
		}
		if m.input != "" {
			m.input = ""
			return nil
		}
		return nil

	case "up":
		return m.recallHistory(-1)
	case "down":
		return m.recallHistory(1)

	case "tab":
		m.applyTabCompletion()
		return nil

	case "enter":
		return m.submitInput()

	case "backspace":
		if len(m.input) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.input)
			m.input = m.input[:len(m.input)-size]
		}
		return nil

	default:
		// Append printable characters using rune-based check so multi-byte
		// UTF-8 (accented chars, emoji, CJK) is not silently dropped (N3).
		if text := msg.Key().Text; text != "" {
			r, _ := utf8.DecodeRuneInString(text)
			if r >= 32 && r != utf8.RuneError {
				m.input += text
			}
		}
		return nil
	}
}

func (m *model) recallHistory(delta int) tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	if m.historyIdx == -1 {
		// Start navigating from the end.
		if delta < 0 {
			m.historyIdx = len(m.history) - 1
		} else {
			m.historyIdx = 0
		}
	} else {
		m.historyIdx += delta
		if m.historyIdx < 0 {
			m.historyIdx = 0
		}
		if m.historyIdx >= len(m.history) {
			m.historyIdx = len(m.history) - 1
		}
	}
	m.input = m.history[m.historyIdx]
	return nil
}

func (m *model) submitInput() tea.Cmd {
	// Interactive prompt active: handle prompt input.
	if m.activePrompt != nil {
		return m.resolvePrompt(m.input)
	}

	// N6: guard against double-submit while a response is in-flight.
	if m.inResponse || m.bashRunning {
		return m.setNotification("still waiting for a response…")
	}

	text := strings.TrimSpace(m.input)
	m.input = ""
	m.historyIdx = -1 // reset history navigation
	m.compIdx = 0     // reset completion cycling
	if text == "" {
		return nil
	}

	// Slash commands.
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// Bash mode: !command runs outside the LLM turn loop.
	if after, ok := strings.CutPrefix(text, "!"); ok {
		return m.handleBashCommand(after)
	}

	// Debounce guard: 300ms between submits (P2 #27).
	if elapsed := time.Since(m.lastSubmit); elapsed < 300*time.Millisecond {
		return m.setNotification("slow down — submit debounced")
	}
	m.lastSubmit = time.Now()

	// Record in history.
	m.history = append(m.history, text)

	// Queue or start a turn.
	return m.startOrQueueTurn(text)
}

// startOrQueueTurn sends a prompt, queueing it behind a running turn.
func (m *model) startOrQueueTurn(text string) tea.Cmd {
	if m.inResponse {
		m.turnQueue = append(m.turnQueue, text)
		return m.setNotification("queued — will send after current response")
	}

	// Record the user message locally for immediate display.
	m.appendMessage("user", text)
	m.inResponse = true
	m.steering = false

	return sendCommand(m.runtime, tauchat.SubmitChatPromptCommand{
		SessionID: m.sessionID,
		Prompt:    text,
	})
}

// drainTurnQueue sends the next queued prompt, if any.
func (m *model) drainTurnQueue() tea.Cmd {
	if len(m.turnQueue) == 0 {
		return nil
	}
	next := m.turnQueue[0]
	m.turnQueue = m.turnQueue[1:]
	return m.startOrQueueTurn(next)
}

// handleSteer sends a steering command mid-turn.
func (m *model) handleSteer() tea.Cmd {
	if !m.inResponse {
		return m.setNotification("no active response to steer")
	}
	text := strings.TrimSpace(m.input)
	m.input = ""
	if text == "" {
		m.steering = !m.steering
		if m.steering {
			return m.setNotification("steering: type a message and press enter")
		}
		return nil
	}
	m.steering = true
	return sendCommand(m.runtime, tauchat.SteerChatPromptCommand{
		SessionID: m.sessionID,
		Prompt:    text,
	})
}

// handleBashCommand runs a shell command outside the LLM turn loop.
func (m *model) handleBashCommand(cmd string) tea.Cmd {
	m.bashRunning = true
	m.bashCallID = ""
	m.appendMessage("user", "!"+cmd)
	return sendCommand(m.runtime, tauchat.RunBashCommand{
		SessionID:   m.sessionID,
		Command:     cmd,
		RequestedAt: time.Now().UTC(),
	})
}

func (m *model) cancelBash() tea.Cmd {
	if !m.bashRunning {
		return nil
	}
	m.bashRunning = false
	return sendCommand(m.runtime, tauchat.CancelBashCommand{
		SessionID:   m.sessionID,
		RequestedAt: time.Now().UTC(),
	})
}

// --- chat event handling ---------------------------------------------------

// handleChatEvent mutates model state and returns an optional Cmd (e.g. for
// notification-clear timers or auto-responses). It MUST be called from within
// Update so the returned Cmd is properly composed.
func (m *model) handleChatEvent(evt tauchat.ChatEvent) tea.Cmd {
	switch e := evt.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		m.applySnapshot(e)

	case tauchat.ChatResponseStartedEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.inResponse = true

	case tauchat.ChatResponseDeltaEvent:
		m.streaming += e.Delta

	case tauchat.ChatReasoningDeltaEvent:
		m.reasoning += e.Delta

	case tauchat.ChatToolCallDeltaEvent:
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary)

	case tauchat.ChatToolExecutionStartedEvent:
		m.setToolStatus(e.CallID, "running")

	case tauchat.ChatToolExecutionCompletedEvent:
		status := "done"
		if e.IsError {
			status = "error"
		}
		m.setToolStatus(e.CallID, status)
		if e.ResultSummary != "" {
			m.setToolResult(e.CallID, e.ResultSummary)
		}

	case tauchat.ChatToolOutputEvent:
		m.setToolResult(e.CallID, e.Chunk)

	case tauchat.ChatResponseCompletedEvent:
		m.finalizeResponse()
		// Drain turn queue after completion.
		return m.drainTurnQueue()

	case tauchat.ChatResponseCancelledEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.inResponse = false
		m.steering = false
		return m.drainTurnQueue()

	case tauchat.ChatRuntimeErrorEvent:
		m.inResponse = false
		return m.setNotification(fmt.Sprintf("error: %s", e.Message))

	case tauchat.ChatNotificationEvent:
		m.notifyQ.Push(notify.Notification{
			Message:  e.Message,
			Level:    notify.LevelInfo,
			Duration: 5 * time.Second,
		})
		return nil

	case tauchat.InteractivePromptRequestedEvent:
		return m.enqueuePrompt(e)

	// Session events.
	case tauchat.SessionsListedEvent:
		m.sessionSummaries = e.Sessions
		return m.setNotification(fmt.Sprintf("%d sessions", len(e.Sessions)))

	case tauchat.SessionLoadedEvent:
		return m.setNotification("session loaded: " + e.State.SessionID)

	case tauchat.SessionDeletedEvent:
		return m.setNotification("session deleted: " + e.SessionID)

	case tauchat.SessionExportedEvent:
		return m.setNotification("session exported: " + e.Path)

	// Extension / plugin events.
	case tauchat.ExtensionsReloadedEvent:
		return m.setNotification(fmt.Sprintf("extensions: %d loaded, %d diagnostics", e.Result.ExtensionCount, len(e.Result.Diagnostics)))

	case tauchat.ExtensionCommandsChangedEvent:
		m.extensionCommands = make(map[string]tauchat.ExtensionCommand, len(e.Commands))
		for _, ext := range e.Commands {
			m.extensionCommands[ext.Name] = ext
		}
		return nil

	case tauchat.CommandsChangedEvent:
		// Registry commands changed — no-op for now (completion picks them up
		// from slashTable which is static).
		return nil

	case tauchat.ExtensionCommandResultEvent:
		if e.Output != "" {
			m.appendMessage("system", e.Output)
		}
		return nil

	case tauchat.ExtensionViewRenderedEvent:
		m.panels[e.ViewID] = pluginPanel{
			id:      e.ViewID,
			title:   e.View.Title,
			content: fmt.Sprintf("%d widgets", len(e.View.Widgets)),
		}
		return nil

	case tauchat.ExtensionViewClosedEvent:
		delete(m.panels, e.ViewID)
		return nil

	// Skills events.
	case tauchat.SkillsChangedEvent:
		return m.setNotification(fmt.Sprintf("skills: %d available", len(e.Skills)))

	default:
		return nil
	}
	return nil
}

func (m *model) applySnapshot(e tauchat.ChatSessionSnapshotEvent) {
	state := e.State
	if state.Model.ID != "" {
		m.modelName = state.Model.ID
	}
	if state.ProviderName != "" {
		m.provider = state.ProviderName
	}
	// N15: statusText consistently means "model @ provider"; never carry
	// error text here — errors go through m.notification.
	m.statusText = fmt.Sprintf("%s @ %s", m.modelName, m.provider)

	// Rebuild viewport content from session history.
	m.renderedLines = m.renderedLines[:0]
	for _, msg := range state.Messages {
		role := ""
		switch msg.Role {
		case tauchat.ChatRoleUser:
			role = "user"
		case tauchat.ChatRoleAssistant:
			role = "assistant"
		default:
			continue
		}
		lines := strings.Split(msg.Content, "\n")
		for i, l := range lines {
			if i == 0 {
				m.renderedLines = append(m.renderedLines, renderLine(role, l))
			} else {
				m.renderedLines = append(m.renderedLines, continuationStyle.Render(l))
			}
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
	m.viewport.GotoBottom()
}

// N7: finalizeResponse synthesises a placeholder when a turn consisted
// purely of tool calls with no trailing assistant text, so the user sees a
// trace of what happened instead of nothing.
func (m *model) finalizeResponse() {
	hadTools := len(m.tools) > 0
	content := m.streaming
	if m.reasoning != "" && content == "" {
		content = "[reasoning only]"
	}
	if content == "" && hadTools {
		var names []string
		for _, t := range m.tools {
			names = append(names, t.name)
		}
		content = fmt.Sprintf("[tools: %s]", strings.Join(names, ", "))
	}
	if content != "" {
		m.appendMessage("assistant", content)
	}
	m.streaming = ""
	m.reasoning = ""
	m.inResponse = false
}

// --- tool state helpers ----------------------------------------------------

func (m *model) upsertToolCall(callID, toolName, argumentsSummary string) {
	for i := range m.tools {
		if m.tools[i].id == callID {
			m.tools[i].name = toolName
			if argumentsSummary != "" {
				m.tools[i].args += argumentsSummary
			}
			return
		}
	}
	m.tools = append(m.tools, toolState{
		id:     callID,
		name:   toolName,
		args:   argumentsSummary,
		status: "pending",
	})
}

func (m *model) setToolStatus(id, status string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].status = status
			return
		}
	}
}

func (m *model) setToolResult(id, result string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].result = result
			return
		}
	}
}

// --- notification helper ---------------------------------------------------

// setNotification sets m.notification and returns a tea.Cmd that clears it
// after 4 seconds, using a generation counter so a newer notification is not
// clobbered by an older timer that fires late (N1).
func (m *model) setNotification(text string) tea.Cmd {
	m.notificationGen++
	m.notification = text
	gen := m.notificationGen
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return clearNotificationMsg{gen: gen}
	})
}

// --- viewport helpers ------------------------------------------------------

// appendMessage writes a styled message line to the viewport and scrolls to
// the bottom. Multi-line content is split so each visual line gets its own
// style wrapping; only the first line carries the role prefix.
func (m *model) appendMessage(role, content string) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if i == 0 {
			m.renderedLines = append(m.renderedLines, renderLine(role, l))
		} else {
			m.renderedLines = append(m.renderedLines, continuationStyle.Render(l))
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
	m.viewport.GotoBottom()
}

// --- message cap -----------------------------------------------------------
// Removed — replaced by bubbles/viewport. Content is bounded by the terminal
// session, not an arbitrary in-memory cap. See commit history for N17 fix.

// --- rendering helpers -----------------------------------------------------

func renderLine(role, content string) string {
	switch role {
	case "user":
		return userStyle.Render("You: " + content)
	case "assistant":
		return assistantStyle.Render("tau: " + content)
	default:
		return content
	}
}

func renderTool(t toolState) string {
	icon := map[string]string{
		"pending": "⏳",
		"running": "⚙️ ",
		"done":    "✅",
		"error":   "❌",
	}[t.status]
	if icon == "" {
		icon = "  "
	}
	line := fmt.Sprintf("  %s %s", icon, t.name)
	// N11: render a short result summary when available.
	const resultLimit = 60
	if t.result != "" {
		summary := strings.ReplaceAll(t.result, "\n", " ")
		if len(summary) > resultLimit {
			summary = summary[:resultLimit] + "…"
		}
		line += " — " + summary
	}
	return toolStyle.Render(line)
}

// --- tea.Msg types ---------------------------------------------------------

// chatEventMsg wraps a ChatEvent for delivery to the Bubbletea update loop.
type chatEventMsg struct {
	event tauchat.ChatEvent
}

// chatEventsClosedMsg is delivered when the subscriber channel closes, either
// from subscriber.Done() or from the events channel itself closing (N12).
type chatEventsClosedMsg struct{}

// clearNotificationMsg clears the notification only if the generation counter
// matches, preventing a stale timer from clearing a newer notification (N1).
type clearNotificationMsg struct {
	gen int
}

// sendResultMsg carries the result of a runtime.Send call (N5).
type sendResultMsg struct {
	err error
}

// startupMsg is sent from Init to set initial display state.
type startupMsg struct {
	sessionID string
	modelName string
	provider  string
}

// --- tea.Cmd constructors --------------------------------------------------

// readNextEvent returns a tea.Cmd that blocks on the next event from the bus
// subscriber, delivering it as a chatEventMsg. Uses select against sub.Done()
// so the goroutine does not leak when the subscriber is closed (N4, N12).
func readNextEvent(sub *eventbus.Subscriber[tauchat.ChatEvent]) tea.Cmd {
	return func() tea.Msg {
		select {
		case evt, ok := <-sub.Events():
			if !ok {
				return chatEventsClosedMsg{}
			}
			return chatEventMsg{event: evt}
		case <-sub.Done():
			return chatEventsClosedMsg{}
		}
	}
}

// sendCommand returns a tea.Cmd that sends a ChatCommand to the coordinator
// and delivers the result (including any error) as a sendResultMsg (N5).
func sendCommand(runtime tauchat.ChatRuntime, cmd tauchat.ChatCommand) tea.Cmd {
	return func() tea.Msg {
		return sendResultMsg{err: runtime.Send(cmd)}
	}
}

// --- interactive prompt handling -------------------------------------------

func (m *model) enqueuePrompt(e tauchat.InteractivePromptRequestedEvent) tea.Cmd {
	if m.activePrompt != nil {
		m.promptQueue = append(m.promptQueue, e)
		return m.setNotification("prompt queued")
	}
	m.activePrompt = &e
	m.input = ""
	return nil
}

func (m *model) handlePromptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		return m.resolvePromptCancel()
	case "enter":
		return m.resolvePrompt(m.input)
	case "y", "Y":
		if p.Kind == "confirm" {
			return m.resolvePromptConfirm(true)
		}
		m.input += msg.Key().Text
	case "n", "N":
		if p.Kind == "confirm" {
			return m.resolvePromptConfirm(false)
		}
		m.input += msg.Key().Text
	case "backspace":
		if len(m.input) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.input)
			m.input = m.input[:len(m.input)-size]
		}
	default:
		if text := msg.Key().Text; text != "" {
			r, _ := utf8.DecodeRuneInString(text)
			if r >= 32 && r != utf8.RuneError {
				m.input += text
			}
		}
	}
	return nil
}

func (m *model) resolvePrompt(input string) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	m.activePrompt = nil
	m.input = ""

	var cmd tauchat.RespondInteractivePromptCommand
	cmd.RequestID = p.RequestID
	cmd.RespondedAt = time.Now().UTC()
	if p.Kind == "confirm" {
		cmd.Confirmed = true
	} else {
		cmd.Response = input
	}

	// Present next queued prompt, if any.
	if len(m.promptQueue) > 0 {
		next := m.promptQueue[0]
		m.promptQueue = m.promptQueue[1:]
		m.activePrompt = &next
		m.input = ""
	}

	return sendCommand(m.runtime, cmd)
}

func (m *model) resolvePromptConfirm(confirmed bool) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	m.activePrompt = nil
	m.input = ""

	// Present next queued prompt, if any.
	if len(m.promptQueue) > 0 {
		next := m.promptQueue[0]
		m.promptQueue = m.promptQueue[1:]
		m.activePrompt = &next
		m.input = ""
	}

	return sendCommand(m.runtime, tauchat.RespondInteractivePromptCommand{
		RequestID:   p.RequestID,
		Confirmed:   confirmed,
		Canceled:    !confirmed,
		RespondedAt: time.Now().UTC(),
	})
}

func (m *model) resolvePromptCancel() tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	m.activePrompt = nil
	m.input = ""

	// Present next queued prompt, if any.
	if len(m.promptQueue) > 0 {
		next := m.promptQueue[0]
		m.promptQueue = m.promptQueue[1:]
		m.activePrompt = &next
		m.input = ""
	}

	return sendCommand(m.runtime, tauchat.RespondInteractivePromptCommand{
		RequestID:   p.RequestID,
		Canceled:    true,
		RespondedAt: time.Now().UTC(),
	})
}

// activePanel returns the first active plugin panel (if any).
func (m *model) activePanel() *pluginPanel {
	for _, p := range m.panels {
		return &p
	}
	return nil
}

// --- rendering helpers -----------------------------------------------------

func renderPrompt(p *tauchat.InteractivePromptRequestedEvent) string {
	var sb strings.Builder
	sb.WriteString(promptBoxStyle.Render("┌─ " + p.Title + " ─┐"))
	sb.WriteString("\n")
	sb.WriteString(promptTextStyle.Render("  " + p.Message))
	sb.WriteString("\n")
	if p.Kind == "confirm" {
		sb.WriteString(promptHintStyle.Render("  [y/n]"))
	} else {
		sb.WriteString(promptHintStyle.Render("  [type + enter, esc to cancel]"))
	}
	sb.WriteString("\n")
	sb.WriteString(promptBoxStyle.Render("└" + strings.Repeat("─", 40) + "┘"))
	return sb.String()
}

func renderCompletions(groups []compGroup) string {
	var sb strings.Builder
	for _, g := range groups {
		sb.WriteString(compTitleStyle.Render(g.Title))
		sb.WriteString("\n")
		for _, m := range g.Matches {
			if m.Description != "" {
				sb.WriteString(compItemStyle.Render(fmt.Sprintf("  %-30s %s", m.Word, m.Description)))
			} else {
				sb.WriteString(compItemStyle.Render("  " + m.Word))
			}
			sb.WriteString("\n")
		}
	}
	// Truncate to at most 8 lines.
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(lines) > 8 {
		lines = lines[:8]
		lines = append(lines, compMoreStyle.Render(fmt.Sprintf("  … and %d more", len(lines)-8)))
	}
	return strings.Join(lines, "\n")
}

// --- styles (N8: sourced from internal/theme colors, converted to hex) -----

// themeHex converts a termkit.Color RGB triple to a lipgloss-compatible color.
func themeHex(c termkit.Color) color.Color {
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2]))
}

var (
	// Brand color — no exact theme match.
	headerColor = lipgloss.Color("#7B2FBE")
	// Semantic colors sourced from internal/theme.
	userColor      = themeHex(theme.CommandFG)
	assistantColor = themeHex(theme.ToolSuccess.FG)
	reasoningColor = themeHex(theme.ToneWarn)
	streamColor    = lipgloss.Color("#FFFFFF") // no theme equivalent
	toolColor      = lipgloss.Color("#FFDC00") // no theme equivalent
	notifyColor    = themeHex(theme.ToolFailed.FG)
	inputColor     = lipgloss.Color("#7FDBFF") // no theme equivalent

	headerStyle       = lipgloss.NewStyle().Bold(true).Foreground(headerColor).Padding(0, 1)
	userStyle         = lipgloss.NewStyle().Foreground(userColor)
	assistantStyle    = lipgloss.NewStyle().Foreground(assistantColor)
	reasoningStyle    = lipgloss.NewStyle().Foreground(reasoningColor).Italic(true)
	streamStyle       = lipgloss.NewStyle().Foreground(streamColor)
	toolStyle         = lipgloss.NewStyle().Foreground(toolColor)
	notifyStyle       = lipgloss.NewStyle().Foreground(notifyColor).Bold(true)
	inputStyle        = lipgloss.NewStyle().Foreground(inputColor)
	continuationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).PaddingLeft(6)

	// Prompt / completion styles.
	promptBoxStyle  = lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn))
	promptTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	promptHintStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	compTitleStyle  = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Bold(true)
	compItemStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	compMoreStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	panelStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG))
)
