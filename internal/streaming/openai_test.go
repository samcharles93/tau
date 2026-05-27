package streaming

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

func TestBuildOpenAIRequestBodyUnchangedWithoutCompat(t *testing.T) {
	session := requestBodySession(config.ModelConfig{})
	got, err := buildOpenAIRequestBody(session)
	if err != nil {
		t.Fatalf("buildOpenAIRequestBody() error = %v", err)
	}
	want, err := json.Marshal(session.CompletionRequest())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("body = %s, want unchanged %s", got, want)
	}
}

func TestBuildOpenAIRequestBodyMergesExtraBody(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{
			ExtraBody: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
			},
		},
	})
	body := decodeRequestBody(t, session)
	if _, ok := body["thinking"].(map[string]any); !ok {
		t.Fatalf("body = %#v, want thinking extra body", body)
	}
	if body["max_tokens"].(float64) != 128 {
		t.Fatalf("max_tokens = %#v, want unchanged max_tokens", body["max_tokens"])
	}
}

func TestBuildOpenAIRequestBodyMaxTokensField(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{MaxTokensField: "max_completion_tokens"},
	})
	body := decodeRequestBody(t, session)
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("body = %#v, did not expect max_tokens", body)
	}
	if body["max_completion_tokens"].(float64) != 128 {
		t.Fatalf("max_completion_tokens = %#v, want 128", body["max_completion_tokens"])
	}
}

func TestBuildOpenAIRequestBodyAssistantToolCallsIncludeContentWhenRequired(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{RequiresAssistantContentForToolCalls: true},
	})
	session.Messages = []chat.ChatMessage{{
		Role: chat.ChatRoleAssistant,
		ToolCalls: []chat.ChatToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: chat.ChatFunctionCall{
				Name:      "lookup",
				Arguments: "{}",
			},
		}},
	}}

	body := decodeRequestBody(t, session)
	messages := body["messages"].([]any)
	message := messages[1].(map[string]any)
	content, ok := message["content"].(string)
	if !ok || content != "" {
		t.Fatalf("assistant tool-call content = %#v, want empty string", message["content"])
	}
}

func TestBuildOpenAIRequestBodyDoesNotInventReasoningContent(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{
			RequiresReasoningContentForToolCalls:        true,
			RequiresReasoningContentOnAssistantMessages: true,
		},
	})
	body := decodeRequestBody(t, session)
	messages := body["messages"].([]any)
	for _, rawMessage := range messages {
		message := rawMessage.(map[string]any)
		if _, ok := message["reasoning_content"]; ok {
			t.Fatalf("message = %#v, did not expect invented reasoning_content", message)
		}
	}
}

func TestBuildOpenAIRequestBodySerializesCapturedReasoningContentForDeepSeekCompat(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{
			RequiresReasoningContentForToolCalls:        true,
			RequiresReasoningContentOnAssistantMessages: true,
		},
	})
	session.Messages = append(session.Messages, chat.ChatMessage{
		Role:             chat.ChatRoleAssistant,
		ReasoningContent: "authentic reasoning",
		ToolCalls: []chat.ChatToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: chat.ChatFunctionCall{
				Name:      "lookup",
				Arguments: "{}",
			},
		}},
	})

	body := decodeRequestBody(t, session)
	messages := body["messages"].([]any)
	message := messages[2].(map[string]any)
	if got := message["reasoning_content"]; got != "authentic reasoning" {
		t.Fatalf("reasoning_content = %#v, want authentic reasoning", got)
	}
}

func TestBuildOpenAIRequestBodyOmitsReasoningContentWithoutCompat(t *testing.T) {
	session := requestBodySession(config.ModelConfig{})
	session.Messages = append(session.Messages, chat.ChatMessage{
		Role:             chat.ChatRoleAssistant,
		Content:          "answer",
		ReasoningContent: "provider reasoning",
	})

	body := decodeRequestBody(t, session)
	messages := body["messages"].([]any)
	message := messages[2].(map[string]any)
	if _, ok := message["reasoning_content"]; ok {
		t.Fatalf("message = %#v, did not expect reasoning_content without compat", message)
	}
}

func TestBuildOpenAIRequestBodySerializesFinalAssistantReasoningOnlyWhenRequired(t *testing.T) {
	session := requestBodySession(config.ModelConfig{
		Compat: config.CompatConfig{RequiresReasoningContentOnAssistantMessages: true},
	})
	session.Messages = append(session.Messages, chat.ChatMessage{
		Role:             chat.ChatRoleAssistant,
		Content:          "answer",
		ReasoningContent: "authentic final reasoning",
	})

	body := decodeRequestBody(t, session)
	messages := body["messages"].([]any)
	message := messages[2].(map[string]any)
	if got := message["reasoning_content"]; got != "authentic final reasoning" {
		t.Fatalf("reasoning_content = %#v, want authentic final reasoning", got)
	}

	session.Model.Config.Compat = config.CompatConfig{RequiresReasoningContentForToolCalls: true}
	body = decodeRequestBody(t, session)
	messages = body["messages"].([]any)
	message = messages[2].(map[string]any)
	if _, ok := message["reasoning_content"]; ok {
		t.Fatalf("message = %#v, did not expect non-tool reasoning_content with only tool-call compat", message)
	}
}

func TestReadStreamAccumulatesReasoningContentWithToolCalls(t *testing.T) {
	streamer := OpenAIStreamer{}
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"more","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var reasoningDeltas []string
	result, err := streamer.readStream(context.Background(), strings.NewReader(sse), chat.StreamCallbacks{
		OnDelta: func(delta string) error {
			t.Fatalf("OnDelta(%q) called for reasoning_content", delta)
			return nil
		},
		OnReasoningDelta: func(delta string) error {
			reasoningDeltas = append(reasoningDeltas, delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("readStream() error = %v", err)
	}
	if result.ReasoningContent != "think more" {
		t.Fatalf("ReasoningContent = %q, want think more", result.ReasoningContent)
	}
	if got := strings.Join(reasoningDeltas, "|"); got != "think |more" {
		t.Fatalf("reasoning deltas = %q, want callbacks for authentic reasoning_content", got)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(result.ToolCalls))
	}
	if got := result.ToolCalls[0].Function.Arguments; got != `{"q":"x"}` {
		t.Fatalf("arguments = %q, want JSON arguments", got)
	}
}

func requestBodySession(model config.ModelConfig) chat.ChatSessionState {
	return chat.ChatSessionState{
		Model: chat.ChatModelRef{
			ID:     "deepseek-v4-pro",
			URL:    "https://api.deepseek.com",
			Config: model,
		},
		SystemPrompt: "system",
		Parameters: chat.ChatParameters{
			MaxTokens:   128,
			Temperature: 0.5,
		},
		Messages: []chat.ChatMessage{{
			Role:    chat.ChatRoleUser,
			Content: "hello",
		}},
	}
}

func decodeRequestBody(t *testing.T, session chat.ChatSessionState) map[string]any {
	t.Helper()
	body, err := buildOpenAIRequestBody(session)
	if err != nil {
		t.Fatalf("buildOpenAIRequestBody() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", body, err)
	}
	return decoded
}
