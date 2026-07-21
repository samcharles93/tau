---
description: Source module internal/chat/stream.go (39 lines).
resource: internal/chat/stream.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stream.go
type: Module
---

# Module stream.go

**Path**: `internal/chat/stream.go`  
**Lines**: 39

## Snippet Preview

```
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
```
