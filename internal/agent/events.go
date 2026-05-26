package agent

import "github.com/samcharles93/tau/internal/chat"

// AgentToolCallDeltaEvent is emitted as tool-call chunks stream in,
// giving the TUI real-time visibility into which tools are being invoked.
type AgentToolCallDeltaEvent struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	Delta     chat.ChatToolCallDelta
}

func (AgentToolCallDeltaEvent) IsChatEvent() {}

// AgentToolExecutionStartedEvent is emitted when a tool begins execution.
type AgentToolExecutionStartedEvent struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	Arguments string `json:"arguments"`
}

func (AgentToolExecutionStartedEvent) IsChatEvent() {}
