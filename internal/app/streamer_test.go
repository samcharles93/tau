package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/runtime"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

type channelTestProvider struct {
	chunks []aisdkchat.Chunk
}

func (p *channelTestProvider) Name() string { return "channel-test" }

func (p *channelTestProvider) Chat(context.Context, aisdkchat.Request) (aisdkchat.Response, error) {
	return aisdkchat.Response{}, nil
}

func (p *channelTestProvider) ChatStream(context.Context, aisdkchat.Request) (aisdkchat.Stream, error) {
	return &channelTestStream{chunks: p.chunks}, nil
}

type channelTestStream struct {
	chunks []aisdkchat.Chunk
	next   int
}

func (s *channelTestStream) Next(context.Context) (aisdkchat.Chunk, error) {
	if s.next >= len(s.chunks) {
		return aisdkchat.Chunk{}, io.EOF
	}
	chunk := s.chunks[s.next]
	s.next++
	return chunk, nil
}

func (*channelTestStream) Close() error { return nil }

func TestStreamerKeepsReasoningAndFinalContentSeparate(t *testing.T) {
	provider := &channelTestProvider{chunks: []aisdkchat.Chunk{
		{Delta: "Useful ", ReasoningDelta: "Need to inspect. "},
		{ReasoningDelta: "Next action."},
		{Delta: "answer.", FinishReason: "stop", Done: true},
	}}
	streamer := NewStreamer(provider, "test-model")

	var finalContent strings.Builder
	var visibleReasoning strings.Builder
	result, err := streamer.StreamChatCompletionFull(
		context.Background(),
		tauchat.ChatSessionState{},
		"",
		nil,
		tauchat.StreamCallbacks{
			OnDelta: func(delta string) error {
				finalContent.WriteString(delta)
				return nil
			},
			OnReasoningDelta: func(delta string) error {
				visibleReasoning.WriteString(delta)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := finalContent.String(), "Useful answer."; got != want {
		t.Fatalf("final content = %q, want %q", got, want)
	}
	if got, want := visibleReasoning.String(), "Need to inspect. Next action."; got != want {
		t.Fatalf("visible reasoning = %q, want %q", got, want)
	}
	if got, want := result.ReasoningContent, visibleReasoning.String(); got != want {
		t.Fatalf("persisted reasoning = %q, want %q", got, want)
	}
}

// TestDynamicStreamerNoSelection verifies the dynamic streamer returns a
// friendly, actionable error (rather than a nil-provider panic) when the
// session has no provider or model selected.
func TestDynamicStreamerNoSelection(t *testing.T) {
	pr := newProviderRuntime(newRuntimeForProviders(nil, false), nil, false)
	streamer := buildDynamicStreamer(pr)

	_, err := streamer.StreamChatCompletionFull(
		context.Background(),
		tauchat.ChatSessionState{}, // no provider, no model
		"",
		nil,
		tauchat.StreamCallbacks{},
	)
	if err == nil {
		t.Fatal("expected error when no model is selected")
	}
	if !strings.Contains(err.Error(), "/provider") || !strings.Contains(err.Error(), "/model") {
		t.Fatalf("expected guidance toward /provider and /model, got: %v", err)
	}
}

// TestAggregateModelRefsTagsProvider verifies that models discovered across
// multiple providers are each tagged with the provider they came from, which is
// what lets /model switch providers live.
func TestAggregateModelRefsTagsProvider(t *testing.T) {
	providers := []tauconfig.ProviderConfig{
		{
			Name:    "alpha",
			BaseURL: "https://alpha.example/v1",
			Models:  []tauconfig.ModelConfig{{ID: "alpha-1"}},
		},
		{
			Name:    "beta",
			BaseURL: "https://beta.example/v1",
			Models:  []tauconfig.ModelConfig{{ID: "beta-1"}, {ID: "beta-2"}},
		},
	}
	rt := newRuntimeForProviders(providers, false)

	refs := aggregateModelRefs(context.Background(), rt, false, providers)

	byID := make(map[string]string, len(refs))
	for _, r := range refs {
		byID[r.ID] = r.Provider
	}
	for id, wantProvider := range map[string]string{
		"alpha-1": "alpha",
		"beta-1":  "beta",
		"beta-2":  "beta",
	} {
		if got := byID[id]; got != wantProvider {
			t.Errorf("model %q: provider tag = %q, want %q", id, got, wantProvider)
		}
	}
}

func TestBuildSessionConfigClampsMaxTokensToModelLimit(t *testing.T) {
	cfg := buildSessionConfig(ChatOptions{
		Provider:    tauconfig.ProviderConfig{Name: "test"},
		MaxTokens:   128000,
		Temperature: 0.7,
	}, tauchat.ChatModelRef{
		ID: "smaller-output",
		Config: tauconfig.ModelConfig{
			MaxTokens:        65536,
			DefaultMaxTokens: 65536,
		},
	}, "system")

	if cfg.Parameters.MaxTokens != 65536 {
		t.Fatalf("max tokens = %d, want 65536", cfg.Parameters.MaxTokens)
	}
}

func TestResolveProviderClassInfersDedicatedProviderByName(t *testing.T) {
	runtime.RegisterBuiltinClasses()
	got := resolveProviderClass(tauconfig.ProviderConfig{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com/v1",
	})
	if got != "deepseek" {
		t.Fatalf("class = %q, want deepseek", got)
	}
}

func TestResolveProviderClassDoesNotInferOllamaNativeClass(t *testing.T) {
	runtime.RegisterBuiltinClasses()
	got := resolveProviderClass(tauconfig.ProviderConfig{
		Name:    "ollama",
		BaseURL: "http://localhost:11434/v1",
	})
	if got != "openai-compatible" {
		t.Fatalf("class = %q, want openai-compatible", got)
	}
}

func TestDeepSeekProviderUsesNativeChatCompletionsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer srv.Close()

	rt := newRuntimeForProviders([]tauconfig.ProviderConfig{{
		Name:    "deepseek",
		BaseURL: srv.URL + "/v1",
		Auth: tauconfig.AuthConfig{
			Type:   tauconfig.AuthTypeAPIKey,
			APIKey: "test-key",
		},
		Models: []tauconfig.ModelConfig{{ID: "deepseek-v4-pro"}},
	}}, false)

	provider, modelID, err := rt.ChatProvider(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("ChatProvider() error = %v", err)
	}
	stream, err := provider.ChatStream(context.Background(), aisdkchat.Request{
		Model:    modelID,
		Messages: []aisdkchat.Message{{Role: aisdkchat.RoleUser, Content: "hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	_ = stream.Close()

	if gotPath != "/chat/completions" {
		t.Fatalf("DeepSeek request path = %q, want /chat/completions", gotPath)
	}
}

func TestOllamaProviderUsesOpenAICompatiblePath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer srv.Close()

	rt := newRuntimeForProviders([]tauconfig.ProviderConfig{{
		Name:    "ollama",
		BaseURL: srv.URL + "/v1",
		Auth: tauconfig.AuthConfig{
			Type: tauconfig.AuthTypeNone,
		},
		Models: []tauconfig.ModelConfig{{ID: "kimi-k2.7-code:cloud"}},
	}}, false)

	provider, modelID, err := rt.ChatProvider(context.Background(), "ollama/kimi-k2.7-code:cloud")
	if err != nil {
		t.Fatalf("ChatProvider() error = %v", err)
	}
	stream, err := provider.ChatStream(context.Background(), aisdkchat.Request{
		Model:    modelID,
		Messages: []aisdkchat.Message{{Role: aisdkchat.RoleUser, Content: "hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	_ = stream.Close()

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("Ollama request path = %q, want /v1/chat/completions", gotPath)
	}
}

func TestBuildToolCallsUsesPresentSparseIndexes(t *testing.T) {
	calls := buildToolCalls(map[int]*assembledToolCall{
		1: {
			id:        "toolu_1",
			name:      "find",
			arguments: `{"pattern":"*.go"}`,
		},
	})

	if len(calls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(calls))
	}
	if calls[0].ID != "toolu_1" {
		t.Fatalf("tool call ID = %q, want toolu_1", calls[0].ID)
	}
	if calls[0].Function.Name != "find" {
		t.Fatalf("tool call name = %q, want find", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"pattern":"*.go"}` {
		t.Fatalf("tool call arguments = %q", calls[0].Function.Arguments)
	}
}

func TestBuildRequestInterleavesImagePartsAtPlaceholderPositions(t *testing.T) {
	s := NewStreamer(&channelTestProvider{}, "test-model")
	session := tauchat.ChatSessionState{
		Model: tauchat.ChatModelRef{ID: "test-model"},
		Messages: []tauchat.ChatMessage{
			{
				Role:    tauchat.ChatRoleUser,
				Content: "edit [Image #1] to match [Image #2] please",
				Attachments: []tauchat.ChatAttachment{
					{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{1, 2, 3}},
					{Placeholder: "[Image #2]", MediaType: "image/jpeg", Data: []byte{4, 5}},
				},
			},
		},
	}

	req := s.buildRequest(session, "test-model", &channelTestProvider{})
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(req.Messages))
	}
	msg := req.Messages[0]
	// Text is split at each placeholder and the image part is interleaved at
	// that exact position: text, image1, text, image2, text.
	if len(msg.Parts) != 5 {
		t.Fatalf("parts = %d, want 5: %+v", len(msg.Parts), msg.Parts)
	}
	wantText := []string{"edit ", " to match ", " please"}
	textIdx := []int{0, 2, 4}
	for i, want := range wantText {
		p, ok := msg.Parts[textIdx[i]].(aisdkchat.TextPart)
		if !ok || p.Text != want {
			t.Fatalf("part %d = %#v, want TextPart %q", textIdx[i], msg.Parts[textIdx[i]], want)
		}
	}
	img1, ok := msg.Parts[1].(aisdkchat.ImagePart)
	if !ok || img1.MediaType != "image/png" || string(img1.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("part 1 = %#v, want image/png with data [1 2 3]", msg.Parts[1])
	}
	img2, ok := msg.Parts[3].(aisdkchat.ImagePart)
	if !ok || img2.MediaType != "image/jpeg" || string(img2.Data) != string([]byte{4, 5}) {
		t.Fatalf("part 3 = %#v, want image/jpeg with data [4 5]", msg.Parts[3])
	}
}

func TestBuildRequestTextOnlyMessageKeepsPlainContent(t *testing.T) {
	s := NewStreamer(&channelTestProvider{}, "test-model")
	session := tauchat.ChatSessionState{
		Model:    tauchat.ChatModelRef{ID: "test-model"},
		Messages: []tauchat.ChatMessage{{Role: tauchat.ChatRoleUser, Content: "hello"}},
	}
	req := s.buildRequest(session, "test-model", &channelTestProvider{})
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d", len(req.Messages))
	}
	msg := req.Messages[0]
	if msg.Parts != nil {
		t.Fatalf("Parts = %+v, want nil for text-only message", msg.Parts)
	}
	if msg.Content != "hello" {
		t.Fatalf("Content = %q", msg.Content)
	}
}

func TestBuildRequestUnreferencedPlaceholderStaysText(t *testing.T) {
	s := NewStreamer(&channelTestProvider{}, "test-model")
	session := tauchat.ChatSessionState{
		Model: tauchat.ChatModelRef{ID: "test-model"},
		Messages: []tauchat.ChatMessage{
			{
				Role:    tauchat.ChatRoleUser,
				Content: "see [Image #1] and [Image #9]",
				Attachments: []tauchat.ChatAttachment{
					{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{1}},
				},
			},
		},
	}
	req := s.buildRequest(session, "test-model", &channelTestProvider{})
	msg := req.Messages[0]
	// [Image #9] has no attachment: it stays inside the text, and only one
	// image part is emitted, at the position of [Image #1].
	if len(msg.Parts) != 3 {
		t.Fatalf("parts = %d, want 3: %+v", len(msg.Parts), msg.Parts)
	}
	if _, ok := msg.Parts[1].(aisdkchat.ImagePart); !ok {
		t.Fatalf("part 1 = %#v, want ImagePart", msg.Parts[1])
	}
	last, ok := msg.Parts[2].(aisdkchat.TextPart)
	if !ok || last.Text != " and [Image #9]" {
		t.Fatalf("last part = %#v", msg.Parts[2])
	}
}
