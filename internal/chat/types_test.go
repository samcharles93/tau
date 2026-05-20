package chat

import (
	"errors"
	"testing"
	"time"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

func TestNewChatSessionStateDefaultsAndRequest(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Endpoint: platform.Endpoints[1],
		Model: ChatModelRef{
			ID:  "nemotron-nano-9b",
			URL: "https://model.example/v1",
		},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	if session.Status != ChatSessionIdle {
		t.Fatalf("status = %q, want %q", session.Status, ChatSessionIdle)
	}
	if session.Parameters.MaxTokens != defaultChatMaxTokens {
		t.Fatalf("max tokens = %d, want %d", session.Parameters.MaxTokens, defaultChatMaxTokens)
	}
	if session.Parameters.Temperature != defaultChatTemperature {
		t.Fatalf("temperature = %v, want %v", session.Parameters.Temperature, defaultChatTemperature)
	}
	if session.SystemPrompt != defaultChatSystemPrompt {
		t.Fatalf("system prompt = %q, want %q", session.SystemPrompt, defaultChatSystemPrompt)
	}

	req := session.CompletionRequest()
	if !req.Stream {
		t.Fatal("completion request should always stream for chat sessions")
	}
	if len(req.Messages) != 1 {
		t.Fatalf("request messages = %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != ChatRoleSystem {
		t.Fatalf("first request role = %q, want %q", req.Messages[0].Role, ChatRoleSystem)
	}
}

func TestChatSessionTurnLifecycle(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Endpoint: platform.Endpoints[1],
		Model: ChatModelRef{
			ID:  "nemotron-nano-9b",
			URL: "https://model.example/v1",
		},
		Parameters: ChatParameters{MaxTokens: 2048, Temperature: 0.2},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	if err := session.BeginTurn("r1", "Hello", now.Add(time.Second)); err != nil {
		t.Fatalf("BeginTurn() error = %v", err)
	}
	if err := session.AppendAssistantDelta("Hi", now.Add(2*time.Second)); err != nil {
		t.Fatalf("AppendAssistantDelta() error = %v", err)
	}
	if err := session.AppendAssistantDelta(" there", now.Add(3*time.Second)); err != nil {
		t.Fatalf("AppendAssistantDelta() error = %v", err)
	}
	if err := session.CompleteTurn("stop", ChatUsage{CompletionTokens: 2, TotalTokens: 4}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}

	if session.Status != ChatSessionIdle {
		t.Fatalf("status = %q, want %q", session.Status, ChatSessionIdle)
	}
	if session.HasActiveRequest() {
		t.Fatal("session should not have an active request after completion")
	}
	if len(session.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(session.Messages))
	}
	if session.Messages[0].Role != ChatRoleUser || session.Messages[0].Content != "Hello" {
		t.Fatalf("first message = %#v, want user Hello", session.Messages[0])
	}
	if session.Messages[1].Role != ChatRoleAssistant || session.Messages[1].Content != "Hi there" {
		t.Fatalf("second message = %#v, want assistant Hi there", session.Messages[1])
	}
	if session.LastFinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", session.LastFinishReason)
	}
}

func TestChatSessionPatchCancelAndFailure(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Endpoint: platform.Endpoints[1],
		Model: ChatModelRef{
			ID:  "nemotron-nano-9b",
			URL: "https://model.example/v1",
		},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	newPrompt := "Be concise."
	newTokens := 512
	newTemp := 0.1
	newModel := ChatModelRef{ID: "granite-8b", URL: "https://other.example/v1"}
	if err := session.ApplyPatch(ChatSessionPatch{
		Model:        &newModel,
		SystemPrompt: &newPrompt,
		MaxTokens:    &newTokens,
		Temperature:  &newTemp,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if session.Model.ID != newModel.ID || session.SystemPrompt != newPrompt {
		t.Fatalf("patch did not apply: %#v", session)
	}

	if err := session.BeginTurn("r1", "Hello", now.Add(2*time.Second)); err != nil {
		t.Fatalf("BeginTurn() error = %v", err)
	}
	if err := session.MarkCancelling(now.Add(3 * time.Second)); err != nil {
		t.Fatalf("MarkCancelling() error = %v", err)
	}
	if err := session.CancelTurn(now.Add(4 * time.Second)); err != nil {
		t.Fatalf("CancelTurn() error = %v", err)
	}
	if session.LastFinishReason != "cancelled" {
		t.Fatalf("finish reason = %q, want cancelled", session.LastFinishReason)
	}

	if err := session.BeginTurn("r2", "Try again", now.Add(5*time.Second)); err != nil {
		t.Fatalf("BeginTurn() error = %v", err)
	}
	if err := session.FailTurn(errors.New("boom"), now.Add(6*time.Second)); err != nil {
		t.Fatalf("FailTurn() error = %v", err)
	}
	if session.LastError != "boom" {
		t.Fatalf("last error = %q, want boom", session.LastError)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("message count = %d, want 2 user messages only", len(session.Messages))
	}
}
