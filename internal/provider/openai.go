// Package streaming provides HTTP-based streaming implementations for
// LLM chat completions. It is infrastructure — the agent coordinator
// depends on the Streamer interface and this package satisfies it.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

const defaultStreamTimeout = 120 * time.Second

// OpenAIStreamer makes streaming chat completion requests to an
// OpenAI-compatible endpoint.
type OpenAIStreamer struct {
	Insecure bool
	Timeout  time.Duration
}

// StreamChatCompletionFull streams a chat completion with full callbacks
// including tool-call deltas. Satisfies the agent.Streamer interface.
func (s OpenAIStreamer) StreamChatCompletionFull(
	ctx context.Context,
	session chat.ChatSessionState,
	bearerToken string,
	extraHeaders map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	requestBody, err := buildOpenAIRequestBody(session)
	if err != nil {
		return chat.CompletionResult{}, fmt.Errorf("encoding chat request: %w", err)
	}

	url := strings.TrimRight(session.Model.URL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return chat.CompletionResult{}, fmt.Errorf("creating chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := NewHTTPClientWithTimeout(s.Insecure, s.timeout())
	resp, err := client.Do(req)
	if err != nil {
		return chat.CompletionResult{}, fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return chat.CompletionResult{}, fmt.Errorf("chat endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return s.readStream(ctx, resp.Body, cb)
}

func buildOpenAIRequestBody(session chat.ChatSessionState) ([]byte, error) {
	request := session.CompletionRequest()
	compat := session.Model.Config.Compat
	if !hasRequestCompat(compat) && !messagesHaveReasoningContent(request.Messages) {
		return json.Marshal(request)
	}

	body := map[string]any{
		"model":       request.Model,
		"messages":    encodeMessages(request.Messages, compat),
		"temperature": request.Temperature,
		"stream":      request.Stream,
	}
	if request.MaxTokens > 0 {
		body["max_tokens"] = request.MaxTokens
	}
	if len(request.Tools) > 0 {
		body["tools"] = request.Tools
	}
	maps.Copy(body, compat.ExtraBody)
	if field := strings.TrimSpace(compat.MaxTokensField); field != "" && field != "max_tokens" {
		if _, ok := body["max_tokens"]; ok {
			body[field] = body["max_tokens"]
			delete(body, "max_tokens")
		}
	}
	return json.Marshal(body)
}

func hasRequestCompat(compat config.CompatConfig) bool {
	return len(compat.ExtraBody) > 0 ||
		strings.TrimSpace(compat.MaxTokensField) != "" ||
		compat.RequiresAssistantContentForToolCalls ||
		compat.RequiresReasoningContentForToolCalls ||
		compat.RequiresReasoningContentOnAssistantMessages
}

func messagesHaveReasoningContent(messages []chat.ChatMessage) bool {
	for _, message := range messages {
		if message.ReasoningContent != "" {
			return true
		}
	}
	return false
}

func encodeMessages(messages []chat.ChatMessage, compat config.CompatConfig) []map[string]any {
	encoded := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": string(message.Role)}

		// Determine if we need to explicitly include the 'content' field.
		// The API generally requires 'content' or 'tool_calls' to be set.
		includeContent := message.Content != "" ||
			(message.Role == chat.ChatRoleAssistant && len(message.ToolCalls) == 0) ||
			(compat.RequiresAssistantContentForToolCalls &&
				message.Role == chat.ChatRoleAssistant &&
				len(message.ToolCalls) > 0)

		if includeContent {
			item["content"] = message.Content
		}

		if len(message.ToolCalls) > 0 {
			item["tool_calls"] = message.ToolCalls
		}
		if shouldReplayReasoningContent(message, compat) {
			item["reasoning_content"] = message.ReasoningContent
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		encoded = append(encoded, item)
	}
	return encoded
}

func shouldReplayReasoningContent(message chat.ChatMessage, compat config.CompatConfig) bool {
	if message.Role != chat.ChatRoleAssistant || message.ReasoningContent == "" {
		return false
	}
	return compat.RequiresReasoningContentOnAssistantMessages ||
		(compat.RequiresReasoningContentForToolCalls && len(message.ToolCalls) > 0)
}

func (s OpenAIStreamer) timeout() time.Duration {
	if s.Timeout <= 0 {
		return defaultStreamTimeout
	}
	return s.Timeout
}

func (s OpenAIStreamer) readStream(
	ctx context.Context,
	body io.Reader,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	result := chat.CompletionResult{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var toolCalls []chat.ChatToolCall

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			result.ToolCalls = toolCalls
			return result, nil
		}

		var chunk chat.ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return result, fmt.Errorf("invalid chat stream chunk: %w", err)
		}

		if chunk.Usage != nil {
			result.Usage = *chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				result.FinishReason = *choice.FinishReason
			}

			if choice.Delta.Content != "" && cb.OnDelta != nil {
				if err := cb.OnDelta(choice.Delta.Content); err != nil {
					return result, err
				}
			}
			if choice.Delta.ReasoningContent != "" {
				result.ReasoningContent += choice.Delta.ReasoningContent
				if cb.OnReasoningDelta != nil {
					if err := cb.OnReasoningDelta(choice.Delta.ReasoningContent); err != nil {
						return result, err
					}
				}
			}

			for _, tcd := range choice.Delta.ToolCalls {
				toolCalls = mergeToolCallDelta(toolCalls, tcd)
				if cb.OnToolCallDelta != nil {
					if err := cb.OnToolCallDelta(tcd); err != nil {
						return result, err
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("reading chat stream: %w", err)
	}

	result.ToolCalls = toolCalls
	return result, nil
}

func mergeToolCallDelta(calls []chat.ChatToolCall, delta chat.ChatToolCallDelta) []chat.ChatToolCall {
	for len(calls) <= delta.Index {
		calls = append(calls, chat.ChatToolCall{Type: "function"})
	}

	tc := &calls[delta.Index]
	if delta.ID != "" {
		tc.ID = delta.ID
	}
	if delta.Type != "" {
		tc.Type = delta.Type
	}
	if delta.Function.Name != "" {
		tc.Function.Name += delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		tc.Function.Arguments += delta.Function.Arguments
	}
	return calls
}
