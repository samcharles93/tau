package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

func testProvider() config.ProviderConfig {
	return config.ProviderConfig{
		Name:    "test",
		BaseURL: "https://provider.example",
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
	}
}

// TestChatSessionConfigValidateAllowsUnconfiguredProvider guards a real
// startup path: `tau --skip-setup` on a machine with no providers must be
// able to launch the TUI showing "use /provider" guidance rather than
// hard-failing before the app ever appears, the same way an empty model
// already launches unselected with a "use /model" hint.
func TestChatSessionConfigValidateAllowsUnconfiguredProvider(t *testing.T) {
	cfg := ChatSessionConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for a fully empty (unconfigured) provider+model", err)
	}
}

// TestChatSessionConfigValidateRejectsPartialProvider guards against a
// config with only one of Name/BaseURL set slipping through as if it were
// deliberately "unconfigured" - that combination can only be a real mistake
// (e.g. a bug upstream partially populating the struct), not a user
// choosing to configure a provider later.
func TestChatSessionConfigValidateRejectsPartialProvider(t *testing.T) {
	nameOnly := ChatSessionConfig{Provider: config.ProviderConfig{Name: "openai"}}
	if err := nameOnly.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want an error for Name set without BaseURL")
	}

	urlOnly := ChatSessionConfig{Provider: config.ProviderConfig{BaseURL: "https://api.openai.com/v1"}}
	if err := urlOnly.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want an error for BaseURL set without Name")
	}
}

func TestNewChatSessionStateDefaultsAndRequest(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:  "test-model",
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

func TestNewChatSessionStateAllowsUnselectedModel(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	// A session may launch with a provider but no model selected; the user
	// picks one later with /model. This must not be rejected.
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() with empty model error = %v", err)
	}
	if session.Model.ID != "" {
		t.Fatalf("model id = %q, want empty", session.Model.ID)
	}
	if session.Status != ChatSessionIdle {
		t.Fatalf("status = %q, want %q", session.Status, ChatSessionIdle)
	}
}

func TestChatSessionTurnLifecycle(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:  "test-model",
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

func TestAppendAssistantToolCallMessageWithReasoningStoresReasoningContent(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:  "test-model",
			URL: "https://model.example/v1",
		},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}
	if err := session.BeginTurn("r1", "Hello", now.Add(time.Second)); err != nil {
		t.Fatalf("BeginTurn() error = %v", err)
	}

	calls := []ChatToolCall{{
		ID:   "call_1",
		Type: "function",
		Function: ChatFunctionCall{
			Name:      "lookup",
			Arguments: "{}",
		},
	}}
	if err := session.AppendAssistantToolCallMessageWithReasoning("", "authentic reasoning", calls, now.Add(2*time.Second)); err != nil {
		t.Fatalf("AppendAssistantToolCallMessageWithReasoning() error = %v", err)
	}

	if len(session.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(session.Messages))
	}
	got := session.Messages[1]
	if got.Role != ChatRoleAssistant {
		t.Fatalf("assistant role = %q, want %q", got.Role, ChatRoleAssistant)
	}
	if got.ReasoningContent != "authentic reasoning" {
		t.Fatalf("reasoning_content = %q, want authentic reasoning", got.ReasoningContent)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls = %#v, want call_1", got.ToolCalls)
	}
}

func TestCompleteTurnWithReasoningStoresFinalAssistantReasoningContent(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:  "test-model",
			URL: "https://model.example/v1",
		},
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
	if err := session.CompleteTurnWithReasoning("stop", ChatUsage{CompletionTokens: 1}, "authentic final reasoning", now.Add(3*time.Second)); err != nil {
		t.Fatalf("CompleteTurnWithReasoning() error = %v", err)
	}

	got := session.Messages[len(session.Messages)-1]
	if got.Content != "Hi" {
		t.Fatalf("content = %q, want Hi", got.Content)
	}
	if got.ReasoningContent != "authentic final reasoning" {
		t.Fatalf("reasoning_content = %q, want authentic final reasoning", got.ReasoningContent)
	}
}

func TestChatSessionPatchCancelAndFailure(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:  "test-model",
			URL: "https://model.example/v1",
		},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	newPrompt := "Be concise."
	newTokens := 512
	newTemp := 0.1
	newModel := ChatModelRef{ID: "other-model", URL: "https://other.example/v1"}
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

func TestApplyPatchClampsMaxTokensToModelLimit(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:     "large-output",
			URL:    "https://model.example/v1",
			Config: config.ModelConfig{MaxTokens: 128000},
		},
		Parameters: ChatParameters{MaxTokens: 128000},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	smaller := ChatModelRef{
		ID:     "smaller-output",
		URL:    "https://model.example/v1",
		Config: config.ModelConfig{MaxTokens: 65536},
	}
	if err := session.ApplyPatch(ChatSessionPatch{Model: &smaller}, now.Add(time.Second)); err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if session.Parameters.MaxTokens != 65536 {
		t.Fatalf("max tokens after model switch = %d, want 65536", session.Parameters.MaxTokens)
	}

	tooHigh := 100000
	if err := session.ApplyPatch(ChatSessionPatch{MaxTokens: &tooHigh}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if session.Parameters.MaxTokens != 65536 {
		t.Fatalf("max tokens after explicit high patch = %d, want 65536", session.Parameters.MaxTokens)
	}
}

// TestApplyPatchResetsMaxTokensWhenNewModelHasUnknownLimit guards against a
// regression where switching to a model with no declared MaxTokens ceiling
// (e.g. a live-fetched Ollama model, whose Config carries no per-model
// metadata) forwarded the PREVIOUS model's MaxTokens value unclamped -
// ClampMaxTokensForModel has no ceiling to clamp against, so a value tuned
// for a large-output model got sent as-is to a model with a much smaller
// real limit and the provider rejected the request with a 400.
func TestApplyPatchResetsMaxTokensWhenNewModelHasUnknownLimit(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model: ChatModelRef{
			ID:     "large-output",
			URL:    "https://model.example/v1",
			Config: config.ModelConfig{MaxTokens: 393220},
		},
		Parameters: ChatParameters{MaxTokens: 393220},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	liveModel := ChatModelRef{
		ID:  "ollama-live-model",
		URL: "http://localhost:11434/v1",
		// No Config.MaxTokens - matches internal/app.liveModelRefs, which
		// has no metadata source for a live-fetched provider's output limit.
	}
	if err := session.ApplyPatch(ChatSessionPatch{Model: &liveModel}, now.Add(time.Second)); err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if session.Parameters.MaxTokens != 0 {
		t.Fatalf("max tokens after switching to unknown-limit model = %d, want 0 (omitted from wire request)", session.Parameters.MaxTokens)
	}
}

// TestAppendStandaloneMessageRequiresNoActiveRequest guards against a
// regression where bash-mode ("!") results couldn't be appended to history
// because, unlike AppendStandaloneMessage, the other Append* helpers require
// HasActiveRequest() - but a bash command runs outside the turn loop and has
// no active request.
func TestAppendStandaloneMessageRequiresNoActiveRequest(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	session, err := NewChatSessionState("s1", ChatSessionConfig{
		Provider: testProvider(),
		Model:    ChatModelRef{ID: "test-model", URL: "https://model.example/v1"},
	}, now)
	if err != nil {
		t.Fatalf("NewChatSessionState() error = %v", err)
	}

	if session.HasActiveRequest() {
		t.Fatal("fresh session should have no active request")
	}

	content := "Ran `ls`\n\n```\nfile.go\n```"
	if err := session.AppendStandaloneMessage(ChatRoleUser, content, now.Add(time.Second)); err != nil {
		t.Fatalf("AppendStandaloneMessage() error = %v", err)
	}

	if len(session.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(session.Messages))
	}
	got := session.Messages[0]
	if got.Role != ChatRoleUser || got.Content != content {
		t.Fatalf("appended message = %#v, want role=%q content=%q", got, ChatRoleUser, content)
	}
	if !session.UpdatedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("UpdatedAt = %v, want %v", session.UpdatedAt, now.Add(time.Second))
	}
}
