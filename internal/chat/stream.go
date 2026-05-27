package chat

// CompletionResult holds the final state of a streamed chat completion.
type CompletionResult struct {
	FinishReason     string
	Usage            ChatUsage
	ToolCalls        []ChatToolCall
	ReasoningContent string
}

// StreamCallbacks groups the callbacks the streamer invokes during SSE parsing.
type StreamCallbacks struct {
	// OnDelta is called for each text content delta.
	OnDelta func(string) error
	// OnReasoningDelta is called for each provider-supplied reasoning delta.
	// It is never called for assistant answer text.
	OnReasoningDelta func(string) error
	// OnToolCallDelta is called for each incremental tool-call chunk.
	// May be nil if the caller does not need tool-call streaming.
	OnToolCallDelta func(ChatToolCallDelta) error
}

// CloneChatSessionState creates a deep copy of the session state.
func CloneChatSessionState(state *ChatSessionState) ChatSessionState {
	clone := *state
	if state.Messages != nil {
		clone.Messages = make([]ChatMessage, len(state.Messages))
		for i, msg := range state.Messages {
			clone.Messages[i] = msg
			if msg.ToolCalls != nil {
				clone.Messages[i].ToolCalls = append([]ChatToolCall(nil), msg.ToolCalls...)
			}
		}
	}
	if state.Tools != nil {
		clone.Tools = append([]ChatToolDef(nil), state.Tools...)
	}
	return clone
}
