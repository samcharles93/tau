package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	aisdkchat "github.com/samcharles93/ai-sdk/pkg/chat"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// providerResolver resolves the ai-sdk provider and model ID to use for a given
// session. A static resolver (NewStreamer) always returns the same provider; a
// dynamic resolver (NewDynamicStreamer) picks the provider per call from the
// session's selected provider/model, which is what enables live cross-provider
// switching during a chat.
type providerResolver func(ctx context.Context, session tauchat.ChatSessionState) (aisdkchat.Provider, string, error)

// Streamer adapts an ai-sdk chat.Provider into tau's agent.Streamer
// interface (StreamChatCompletionFull). It preserves tau's existing
// coordinator turn loop, event bus, and tool execution while delegating the
// raw provider protocol to ai-sdk.
type Streamer struct {
	resolve providerResolver
}

// NewStreamer returns a Streamer permanently bound to the given ai-sdk
// provider and model. Used by headless one-shot runs that never switch
// providers.
func NewStreamer(provider aisdkchat.Provider, modelID string) *Streamer {
	return &Streamer{
		resolve: func(context.Context, tauchat.ChatSessionState) (aisdkchat.Provider, string, error) {
			return provider, modelID, nil
		},
	}
}

// NewDynamicStreamer returns a Streamer that resolves its provider per call.
// The resolver reads the session's selected provider/model, so changing either
// (via /model or /login) takes effect on the next turn without rebuilding the
// coordinator.
func NewDynamicStreamer(resolve providerResolver) *Streamer {
	return &Streamer{resolve: resolve}
}

// StreamChatCompletionFull implements agent.Streamer.
func (s *Streamer) StreamChatCompletionFull(
	ctx context.Context,
	session tauchat.ChatSessionState,
	bearerToken string,
	extraHeaders map[string]string,
	cb tauchat.StreamCallbacks,
) (tauchat.CompletionResult, error) {
	provider, modelID, err := s.resolve(ctx, session)
	if err != nil {
		return tauchat.CompletionResult{}, err
	}
	if provider == nil {
		return tauchat.CompletionResult{}, errors.New("ai: provider is nil")
	}

	req := s.buildRequest(session, modelID)
	streamCtx := aisdkchat.WithContextHeaders(ctx, extraHeaders)
	stream, err := provider.ChatStream(streamCtx, req)
	if err != nil {
		return tauchat.CompletionResult{}, err
	}
	defer stream.Close()

	result := tauchat.CompletionResult{}
	calls := make(map[int]*assembledToolCall)

	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}

		if chunk.Delta != "" && cb.OnDelta != nil {
			if err := cb.OnDelta(chunk.Delta); err != nil {
				return result, err
			}
		}
		if chunk.ReasoningDelta != "" && cb.OnReasoningDelta != nil {
			result.ReasoningContent += chunk.ReasoningDelta
			if err := cb.OnReasoningDelta(chunk.ReasoningDelta); err != nil {
				return result, err
			}
		}
		for _, d := range chunk.ToolCallDeltas {
			if err := s.applyToolCallDelta(calls, d, cb); err != nil {
				return result, err
			}
		}
		if chunk.Usage != nil {
			result.Usage = tauchat.ChatUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		if chunk.FinishReason != "" {
			result.FinishReason = chunk.FinishReason
		}
		if chunk.Done {
			break
		}
	}

	result.ToolCalls = buildToolCalls(calls)
	return result, nil
}

func (s *Streamer) buildRequest(session tauchat.ChatSessionState, fallbackModelID string) aisdkchat.Request {
	req := aisdkchat.Request{
		Model:       session.Model.ID,
		MaxTokens:   session.Parameters.MaxTokens,
		Temperature: float32(session.Parameters.Temperature),
		Stream:      true,
	}
	if session.Parameters.ReasoningEffort != "" && session.Parameters.ReasoningEffort != "off" {
		req.ProviderOptions = map[string]any{
			"openai": map[string]any{
				"reasoning_effort": effortToOpenAI(session.Parameters.ReasoningEffort),
			},
		}
	}
	if req.Model == "" {
		req.Model = fallbackModelID
	}

	for _, m := range session.RequestMessages() {
		msg := aisdkchat.Message{
			Role:       aisdkchat.Role(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]aisdkchat.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, aisdkchat.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
		}
		// Preserve reasoning content for multi-turn replay on providers
		// that support it (DeepSeek, Anthropic, etc.).
		if m.ReasoningContent != "" {
			msg.Parts = append(msg.Parts, aisdkchat.ReasoningPart{Text: m.ReasoningContent})
			if m.Content != "" {
				msg.Parts = append(msg.Parts, aisdkchat.TextPart{Text: m.Content})
			}
		}
		req.Messages = append(req.Messages, msg)
	}

	for _, t := range session.Tools {
		req.Tools = append(req.Tools, aisdkchat.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}

	return req
}

type assembledToolCall struct {
	id        string
	name      string
	arguments string
}

func (s *Streamer) applyToolCallDelta(
	calls map[int]*assembledToolCall,
	d aisdkchat.ToolCallDelta,
	cb tauchat.StreamCallbacks,
) error {
	call, ok := calls[d.Index]
	if !ok {
		call = &assembledToolCall{}
		calls[d.Index] = call
	}
	if d.ID != "" {
		call.id = d.ID
	}
	if d.Name != "" {
		call.name = d.Name
	}
	call.arguments += d.ArgsDelta

	if cb.OnToolCallDelta != nil {
		return cb.OnToolCallDelta(tauchat.ChatToolCallDelta{
			Index: d.Index,
			ID:    call.id,
			Type:  "function",
			Function: tauchat.ChatFunctionCallDelta{
				Name:      call.name,
				Arguments: d.ArgsDelta,
			},
		})
	}
	return nil
}

func buildToolCalls(calls map[int]*assembledToolCall) []tauchat.ChatToolCall {
	if len(calls) == 0 {
		return nil
	}
	indices := make([]int, 0, len(calls))
	for i := range calls {
		indices = append(indices, i)
	}
	for len(indices) > 0 && indices[0] > len(indices)-1 {
		// Defensive: ensure contiguous indexing.
		break
	}
	out := make([]tauchat.ChatToolCall, 0, len(indices))
	for i := 0; i < len(calls); i++ {
		call, ok := calls[i]
		if !ok {
			continue
		}
		if call.id == "" {
			call.id = fmt.Sprintf("tool_call_%d", i)
		}
		out = append(out, tauchat.ChatToolCall{
			ID:   call.id,
			Type: "function",
			Function: tauchat.ChatFunctionCall{
				Name:      call.name,
				Arguments: call.arguments,
			},
		})
	}
	return out
}

// effortToOpenAI maps tau's reasoning effort levels to OpenAI API values.
func effortToOpenAI(tauLevel string) string {
	switch tauLevel {
	case "off", "none":
		return "none"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "max":
		return "xhigh"
	default:
		return "medium"
	}
}
