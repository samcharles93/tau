package tui2

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
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

	// Conversation state.
	messages   []chatMessage // finalized messages
	streaming  string        // current streaming text delta
	reasoning  string        // current reasoning delta
	inResponse bool          // true while a response is in progress

	// Tool state.
	tools []toolState // active tool calls in display order

	// Input state.
	input string // current line buffer

	// Status / transient.
	statusText   string // one-line status bar
	notification string // transient notification (shown for one frame)
}

type chatMessage struct {
	role    string // "user", "assistant"
	content string
}

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"
}

// --- constructor -----------------------------------------------------------

func newModel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	chatSub *eventbus.Subscriber[tauchat.ChatEvent],
	sessionID, modelName, provider string,
) *model {
	return &model{
		ctx:       ctx,
		runtime:   runtime,
		chatSub:   chatSub,
		sessionID: sessionID,
		modelName: modelName,
		provider:  provider,
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
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case chatEventMsg:
		m.handleChatEvent(msg.event)
		// Re-arm: read the next event from the bus. This is mandatory —
		// forgetting to return readNextEvent silently stops event delivery
		// after the first message.
		return m, readNextEvent(m.chatSub)

	case startupMsg:
		m.sessionID = msg.sessionID
		m.modelName = msg.modelName
		m.provider = msg.provider
		if m.modelName != "" {
			m.statusText = fmt.Sprintf("%s @ %s  |  type a message and press enter  |  ctrl+c to quit", m.modelName, m.provider)
		} else {
			m.statusText = "no model selected  |  type a message and press enter  |  ctrl+c to quit"
		}
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

	// Messages.
	for _, msg := range m.messages {
		sb.WriteString(renderMessage(msg))
		sb.WriteString("\n")
	}

	// Streaming text (in-progress assistant response).
	if m.reasoning != "" {
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

	// Notification.
	if m.notification != "" {
		sb.WriteString("\n")
		sb.WriteString(notifyStyle.Render("📢 " + m.notification))
	}

	sb.WriteString("\n")

	// Separator before input area.
	sb.WriteString(strings.Repeat("─", 80))
	sb.WriteString("\n")

	// Input area.
	if m.inResponse {
		sb.WriteString(inputStyle.Render("⏳ waiting for response…"))
	} else if m.input != "" {
		sb.WriteString(inputStyle.Render("> " + m.input))
	} else {
		sb.WriteString(inputStyle.Render("> "))
	}
	sb.WriteString("\n")

	// Status bar.
	if m.statusText != "" {
		sb.WriteString(statusStyle.Render(m.statusText))
	}

	return tea.NewView(sb.String())
}

// --- key handling ----------------------------------------------------------

func (m *model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit

	case "esc":
		if m.input != "" {
			m.input = ""
			return nil
		}
		return nil

	case "enter":
		return m.submitInput()

	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return nil

	default:
		// Append printable characters.
		if text := msg.Key().Text; len(text) == 1 && text[0] >= 32 {
			m.input += text
		}
		return nil
	}
}

func (m *model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.input)
	m.input = ""
	if text == "" {
		return nil
	}

	// Special commands.
	if strings.HasPrefix(text, "/quit") || strings.HasPrefix(text, "/q") {
		return tea.Quit
	}

	// Record the user message locally for immediate display.
	m.messages = append(m.messages, chatMessage{role: "user", content: text})
	m.inResponse = true

	// Send to the coordinator asynchronously.
	return sendCommand(m.runtime, tauchat.SubmitChatPromptCommand{
		SessionID: m.sessionID,
		Prompt:    text,
	})
}

// --- chat event handling ---------------------------------------------------

func (m *model) handleChatEvent(evt tauchat.ChatEvent) {
	switch e := evt.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		m.applySnapshot(e)

	case tauchat.ChatResponseStartedEvent:
		// New response starting — clear streaming state.
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
		// Finalize the streaming content as a completed message.
		m.finalizeResponse()

	case tauchat.ChatResponseCancelledEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.inResponse = false

	case tauchat.ChatRuntimeErrorEvent:
		m.notification = fmt.Sprintf("error: %s", e.Message)
		if m.modelName != "" {
			m.statusText = fmt.Sprintf("%s @ %s  |  error: %s", m.modelName, m.provider, e.Message)
		} else {
			m.statusText = fmt.Sprintf("error: %s", e.Message)
		}

	case tauchat.ChatNotificationEvent:
		m.notification = e.Message
	}
}

func (m *model) applySnapshot(e tauchat.ChatSessionSnapshotEvent) {
	state := e.State
	if state.Model.ID != "" {
		m.modelName = state.Model.ID
	}
	if state.ProviderName != "" {
		m.provider = state.ProviderName
	}
	m.statusText = fmt.Sprintf("%s @ %s", m.modelName, m.provider)

	// Rebuild message list from history.
	m.messages = nil
	for _, msg := range state.Messages {
		switch msg.Role {
		case tauchat.ChatRoleUser:
			m.messages = append(m.messages, chatMessage{role: "user", content: msg.Content})
		case tauchat.ChatRoleAssistant:
			m.messages = append(m.messages, chatMessage{role: "assistant", content: msg.Content})
		}
	}
}

func (m *model) finalizeResponse() {
	content := m.streaming
	if m.reasoning != "" && content == "" {
		content = "[reasoning only]"
	}
	if content != "" {
		m.messages = append(m.messages, chatMessage{role: "assistant", content: content})
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

// --- rendering helpers -----------------------------------------------------

func renderMessage(msg chatMessage) string {
	switch msg.role {
	case "user":
		return userStyle.Render("You: " + msg.content)
	case "assistant":
		return assistantStyle.Render("tau: " + msg.content)
	default:
		return msg.content
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
	return toolStyle.Render(fmt.Sprintf("  %s %s", icon, t.name))
}

// --- tea.Msg types ---------------------------------------------------------

// chatEventMsg wraps a ChatEvent for delivery to the Bubbletea update loop.
type chatEventMsg struct {
	event tauchat.ChatEvent
}

// startupMsg is sent from Init to set initial display state.
type startupMsg struct {
	sessionID string
	modelName string
	provider  string
}

// --- tea.Cmd constructors --------------------------------------------------

// readNextEvent returns a tea.Cmd that blocks on the next event from the bus
// subscriber, delivering it as a chatEventMsg. The caller MUST re-arm after
// each event by returning another readNextEvent from Update — forgetting to
// do so silently stops event delivery.
func readNextEvent(sub *eventbus.Subscriber[tauchat.ChatEvent]) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-sub.Events()
		if !ok {
			return tea.Quit()
		}
		return chatEventMsg{event: evt}
	}
}

// sendCommand returns a tea.Cmd that sends a ChatCommand to the coordinator.
// The send is wrapped in its own goroutine so a slow/blocked coordinator
// can't stall the Bubbletea message loop.
func sendCommand(runtime tauchat.ChatRuntime, cmd tauchat.ChatCommand) tea.Cmd {
	return func() tea.Msg {
		_ = runtime.Send(cmd)
		return nil
	}
}

// --- styles ----------------------------------------------------------------

var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7B2FBE")).Padding(0, 1)
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A90D9"))
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#2ECC40"))
	reasoningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF851B")).Italic(true)
	streamStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFDC00"))
	notifyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#DA1710")).Bold(true)
	inputStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7FDBFF"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
)
