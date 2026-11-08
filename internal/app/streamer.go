package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"

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

// drainStream reads chunks until EOF/Done and folds them into the result.
func (s *Streamer) drainStream(ctx context.Context, stream aisdkchat.Stream, cb tauchat.StreamCallbacks) (tauchat.CompletionResult, error) {
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
		done, err := s.applyChunk(chunk, &result, calls, cb)
		if err != nil {
			return result, err
		}
		if done {
			break
		}
	}
	result.ToolCalls = buildToolCalls(calls)
	for i, call := range result.ToolCalls {
		if strings.TrimSpace(call.Function.Name) == "" {
			return result, fmt.Errorf("stream tool call %d (%q) has no function name", i, call.ID)
		}
	}
	return result, nil
}

// applyChunk folds one stream chunk into result/calls. Returns done=true when
// the chunk marks the stream finished.
func (s *Streamer) applyChunk(chunk aisdkchat.Chunk, result *tauchat.CompletionResult, calls map[int]*assembledToolCall, cb tauchat.StreamCallbacks) (bool, error) {
	if chunk.Delta != "" && cb.OnDelta != nil {
		if err := cb.OnDelta(chunk.Delta); err != nil {
			return false, err
		}
	}
	if chunk.ReasoningDelta != "" && cb.OnReasoningDelta != nil {
		result.ReasoningContent += chunk.ReasoningDelta
		if err := cb.OnReasoningDelta(chunk.ReasoningDelta); err != nil {
			return false, err
		}
	}
	for _, d := range chunk.ToolCallDeltas {
		if err := s.applyToolCallDelta(calls, d, cb); err != nil {
			return false, err
		}
	}
	if chunk.Usage != nil {
		result.Usage = tauchat.ChatUsage{
			PromptTokens:        chunk.Usage.PromptTokens,
			CompletionTokens:    chunk.Usage.CompletionTokens,
			TotalTokens:         chunk.Usage.TotalTokens,
			CachedTokens:        chunk.Usage.CachedTokens,
			CacheCreationTokens: chunk.Usage.CacheCreationTokens,
		}
	}
	if chunk.FinishReason != "" {
		result.FinishReason = chunk.FinishReason
	}
	if chunk.Done {
		return true, nil
	}
	return false, nil
}

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

	req := s.buildRequest(session, modelID, provider)
	streamCtx := aisdkchat.WithContextHeaders(ctx, mergeHeaders(session.Provider.Headers, extraHeaders))
	stream, err := provider.ChatStream(streamCtx, req)
	if err != nil {
		return tauchat.CompletionResult{}, err
	}
	defer stream.Close()

	return s.drainStream(ctx, stream, cb)
}

func mergeHeaders(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	for k, v := range override {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func (s *Streamer) buildRequest(session tauchat.ChatSessionState, modelID string, provider aisdkchat.Provider) aisdkchat.Request {
	req := aisdkchat.Request{
		Model:       session.Model.ID,
		MaxTokens:   session.Parameters.MaxTokens,
		Temperature: float32(session.Parameters.Temperature),
		Stream:      true,
	}
	if session.Parameters.ReasoningEffort != "" && session.Parameters.ReasoningEffort != "auto" {
		req.ProviderOptions = map[string]any{
			provider.Name(): map[string]any{
				"reasoning_effort": session.Parameters.ReasoningEffort,
			},
		}
	}
	if req.Model == "" {
		req.Model = modelID
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
				args := tc.Function.Arguments
				// Defense in depth: sanitize malformed arguments from
				// historical messages so they don't cause API 400 errors.
				if args = strings.TrimSpace(args); args == "" || !json.Valid([]byte(args)) {
					args = "{}"
				}
				msg.ToolCalls = append(msg.ToolCalls, aisdkchat.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				})
			}
		}
		// Inline attachments (pasted images) become content parts placed
		// at the exact position of their placeholder token in the text, so
		// the model sees each image where the prompt references it.
		if parts := attachmentParts(m); len(parts) > 0 {
			if m.ReasoningContent != "" {
				parts = append(aisdkchat.Parts{aisdkchat.ReasoningPart{Text: m.ReasoningContent}}, parts...)
			}
			msg.Parts = parts
		} else if m.ReasoningContent != "" {
			// Preserve reasoning content for multi-turn replay on providers
			// that support it (DeepSeek, Anthropic, etc.).
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

// attachmentParts converts a message's inline attachments into ai-sdk content
// parts, interleaved with text parts at the exact byte position of each
// placeholder token ("[Image #1]"): the walk emits the preceding text, then
// the image part, then continues - so the provider receives text and images
// in the order the user wrote them. The placeholder token itself stays in
// the text, keeping the reference explicit for the model. Returns nil when
// the message has no attachments (the common case), letting callers keep the
// plain Content string path.
func attachmentParts(m tauchat.ChatMessage) aisdkchat.Parts {
	if len(m.Attachments) == 0 {
		return nil
	}
	byPlaceholder := make(map[string]tauchat.ChatAttachment, len(m.Attachments))
	for _, a := range m.Attachments {
		if a.Placeholder == "" || len(a.Data) == 0 || a.MediaType == "" {
			continue
		}
		if _, ok := byPlaceholder[a.Placeholder]; !ok {
			byPlaceholder[a.Placeholder] = a
		}
	}
	if len(byPlaceholder) == 0 {
		return nil
	}

	// Only placeholders that resolve to an attachment split the text - an
	// unresolved token (e.g. typed literally, or a detached image) stays in
	// the text untouched.
	var resolved [][2]int
	var atts []tauchat.ChatAttachment
	for _, loc := range tauchat.ImagePlaceholderSpans(m.Content) {
		if a, ok := byPlaceholder[m.Content[loc[0]:loc[1]]]; ok {
			resolved = append(resolved, loc)
			atts = append(atts, a)
		}
	}
	if len(resolved) == 0 {
		return nil
	}

	var parts aisdkchat.Parts
	cur := 0
	for i, sp := range resolved {
		if text := m.Content[cur:sp[0]]; text != "" {
			parts = append(parts, aisdkchat.NewTextPart(text))
		}
		a := atts[i]
		if a.IsImage() {
			parts = append(parts, aisdkchat.NewImageData(a.MediaType, a.Data))
		} else {
			parts = append(parts, aisdkchat.NewFileData(a.Name, a.MediaType, a.Data))
		}
		cur = sp[1]
	}
	if rest := m.Content[cur:]; rest != "" {
		parts = append(parts, aisdkchat.NewTextPart(rest))
	}
	return parts
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
	sort.Ints(indices)

	out := make([]tauchat.ChatToolCall, 0, len(indices))
	for _, i := range indices {
		call := calls[i]
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
