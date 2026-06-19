package ai

import (
	"context"
	"io"
	"testing"

	aisdkchat "github.com/samcharles93/ai-sdk/pkg/chat"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

type fakeProvider struct {
	chunks []aisdkchat.Chunk
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Chat(ctx context.Context, req aisdkchat.Request) (aisdkchat.Response, error) {
	return aisdkchat.Response{}, nil
}

func (p *fakeProvider) ChatStream(ctx context.Context, req aisdkchat.Request) (aisdkchat.Stream, error) {
	return &fakeStream{chunks: p.chunks}, nil
}

type fakeStream struct {
	chunks []aisdkchat.Chunk
	idx    int
}

func (s *fakeStream) Next(ctx context.Context) (aisdkchat.Chunk, error) {
	if s.idx >= len(s.chunks) {
		return aisdkchat.Chunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *fakeStream) Close() error { return nil }

func TestStreamerEmitsDeltasAndUsage(t *testing.T) {
	provider := &fakeProvider{
		chunks: []aisdkchat.Chunk{
			{Delta: "Hello"},
			{ReasoningDelta: "thinking"},
			{Usage: &aisdkchat.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}},
			{Done: true, FinishReason: "stop"},
		},
	}
	streamer := NewStreamer(provider, "fake-model")

	var deltas []string
	var reasoning []string
	session := tauchat.ChatSessionState{
		Model: tauchat.ChatModelRef{ID: "fake-model"},
	}
	result, err := streamer.StreamChatCompletionFull(context.Background(), session, "token", nil, tauchat.StreamCallbacks{
		OnDelta: func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
		OnReasoningDelta: func(delta string) error {
			reasoning = append(reasoning, delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StreamChatCompletionFull error = %v", err)
	}

	if len(deltas) != 1 || deltas[0] != "Hello" {
		t.Fatalf("deltas = %v, want [Hello]", deltas)
	}
	if len(reasoning) != 1 || reasoning[0] != "thinking" {
		t.Fatalf("reasoning = %v, want [thinking]", reasoning)
	}
	if result.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", result.FinishReason)
	}
	if result.Usage.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want 3", result.Usage.TotalTokens)
	}
	if result.ReasoningContent != "thinking" {
		t.Fatalf("reasoning content = %q, want thinking", result.ReasoningContent)
	}
}

func TestStreamerAssemblesToolCalls(t *testing.T) {
	provider := &fakeProvider{
		chunks: []aisdkchat.Chunk{
			{ToolCallDeltas: []aisdkchat.ToolCallDelta{{Index: 0, ID: "call_1", Name: "find"}}},
			{ToolCallDeltas: []aisdkchat.ToolCallDelta{{Index: 0, ArgsDelta: `{"x":`}}},
			{ToolCallDeltas: []aisdkchat.ToolCallDelta{{Index: 0, ArgsDelta: `1}`}}},
			{Done: true, FinishReason: "tool_calls"},
		},
	}
	streamer := NewStreamer(provider, "fake-model")

	session := tauchat.ChatSessionState{
		Model: tauchat.ChatModelRef{ID: "fake-model"},
	}
	result, err := streamer.StreamChatCompletionFull(context.Background(), session, "token", nil, tauchat.StreamCallbacks{})
	if err != nil {
		t.Fatalf("StreamChatCompletionFull error = %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Fatalf("call id = %q, want call_1", tc.ID)
	}
	if tc.Function.Name != "find" {
		t.Fatalf("tool name = %q, want find", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"x":1}` {
		t.Fatalf("arguments = %q, want {\"x\":1}", tc.Function.Arguments)
	}
}
