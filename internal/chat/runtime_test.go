package chat

import (
	"context"
	"testing"
	"time"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

func TestRuntimeStreamsCompletion(t *testing.T) {
	runtime, err := NewRuntime(
		func(ctx context.Context, endpoint platform.Endpoint) (string, error) { return "token", nil },
		fakeStreamer{
			deltas: []string{"Hello", " world"},
			result: CompletionResult{
				FinishReason: "stop",
				Usage:        ChatUsage{CompletionTokens: 2, TotalTokens: 4},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()

	sub, err := runtime.SubscribeEvents(64)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer sub.Unsubscribe()

	if err := runtime.Send(StartChatSessionCommand{
		SessionID: "s1",
		Config: ChatSessionConfig{
			Endpoint: platform.Endpoints[1],
			Model:    ChatModelRef{ID: "nemotron", URL: "https://model.example"},
		},
	}); err != nil {
		t.Fatalf("Send(start) error = %v", err)
	}
	if err := runtime.Send(SubmitChatPromptCommand{
		SessionID:   "s1",
		RequestID:   "r1",
		Prompt:      "Hi",
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Send(submit) error = %v", err)
	}

	var started bool
	var deltas []string

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for completion event")
		case event := <-sub.Channel():
			switch event := event.(type) {
			case ChatResponseStartedEvent:
				if event.SessionID == "s1" && event.RequestID == "r1" {
					started = true
				}
			case ChatResponseDeltaEvent:
				if event.SessionID == "s1" && event.RequestID == "r1" {
					deltas = append(deltas, event.Delta)
				}
			case ChatResponseCompletedEvent:
				if event.State.SessionID != "s1" || event.RequestID != "r1" {
					continue
				}
				if !started {
					t.Fatal("completion arrived before started event")
				}
				if len(deltas) != 2 {
					t.Fatalf("delta count = %d, want 2", len(deltas))
				}
				if got := event.State.Messages[len(event.State.Messages)-1].Content; got != "Hello world" {
					t.Fatalf("assistant content = %q, want %q", got, "Hello world")
				}
				return
			}
		}
	}
}

func TestRuntimeCancelsActiveTurn(t *testing.T) {
	streamer := &blockingStreamer{started: make(chan struct{})}
	runtime, err := NewRuntime(
		func(ctx context.Context, endpoint platform.Endpoint) (string, error) { return "token", nil },
		streamer,
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()

	sub, err := runtime.SubscribeEvents(64)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer sub.Unsubscribe()

	if err := runtime.Send(StartChatSessionCommand{
		SessionID: "s1",
		Config: ChatSessionConfig{
			Endpoint: platform.Endpoints[1],
			Model:    ChatModelRef{ID: "nemotron", URL: "https://model.example"},
		},
	}); err != nil {
		t.Fatalf("Send(start) error = %v", err)
	}
	if err := runtime.Send(SubmitChatPromptCommand{
		SessionID:   "s1",
		RequestID:   "r1",
		Prompt:      "Hi",
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Send(submit) error = %v", err)
	}

	select {
	case <-streamer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streamer to start")
	}

	if err := runtime.Send(CancelChatRequestCommand{
		SessionID:   "s1",
		RequestID:   "r1",
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Send(cancel) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for cancellation event")
		case event := <-sub.Channel():
			cancelled, ok := event.(ChatResponseCancelledEvent)
			if !ok {
				continue
			}
			if cancelled.State.SessionID != "s1" || cancelled.RequestID != "r1" {
				continue
			}
			if cancelled.State.Status != ChatSessionIdle {
				t.Fatalf("status = %q, want %q", cancelled.State.Status, ChatSessionIdle)
			}
			if len(cancelled.State.Messages) != 1 {
				t.Fatalf("message count = %d, want 1", len(cancelled.State.Messages))
			}
			return
		}
	}
}

func TestRuntimeSubscribeEventsFanout(t *testing.T) {
	runtime, err := NewRuntime(
		func(ctx context.Context, endpoint platform.Endpoint) (string, error) { return "token", nil },
		fakeStreamer{},
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()

	subscription, err := runtime.SubscribeEvents(1)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer subscription.Unsubscribe()

	if err := runtime.Send(StartChatSessionCommand{
		SessionID: "s1",
		Config: ChatSessionConfig{
			Endpoint: platform.Endpoints[1],
			Model:    ChatModelRef{ID: "nemotron", URL: "https://model.example"},
		},
	}); err != nil {
		t.Fatalf("Send(start) error = %v", err)
	}

	event, ok := <-subscription.Channel()
	if !ok {
		t.Fatal("subscription channel closed unexpectedly")
	}
	if _, ok := event.(ChatSessionSnapshotEvent); !ok {
		t.Fatalf("event type = %T, want %T", event, ChatSessionSnapshotEvent{})
	}
}

type fakeStreamer struct {
	deltas []string
	result CompletionResult
	err    error
}

func (f fakeStreamer) StreamChatCompletion(
	ctx context.Context,
	session ChatSessionState,
	maasToken string,
	onDelta func(string) error,
) (CompletionResult, error) {
	for _, delta := range f.deltas {
		if err := onDelta(delta); err != nil {
			return CompletionResult{}, err
		}
	}
	if f.err != nil {
		return CompletionResult{}, f.err
	}
	return f.result, nil
}

type blockingStreamer struct {
	started chan struct{}
}

func (b *blockingStreamer) StreamChatCompletion(
	ctx context.Context,
	session ChatSessionState,
	maasToken string,
	onDelta func(string) error,
) (CompletionResult, error) {
	close(b.started)
	<-ctx.Done()
	return CompletionResult{}, ctx.Err()
}
