package chat

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

func testRuntimeProvider() config.ProviderConfig {
	return config.ProviderConfig{
		Name:    "test",
		BaseURL: "https://provider.example",
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
	}
}

func TestRuntimeStreamsCompletion(t *testing.T) {
	runtime, err := NewRuntime(
		context.Background(),
		func(ctx context.Context, _ config.ProviderConfig) (string, error) { return "token", nil },
		fakeStreamer{
			deltas: []string{"Hello", " world"},
			result: CompletionResult{
				FinishReason:     "stop",
				Usage:            ChatUsage{CompletionTokens: 2, TotalTokens: 4},
				ReasoningContent: "runtime reasoning",
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
			Provider: testRuntimeProvider(),
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
				message := event.State.Messages[len(event.State.Messages)-1]
				if got := message.Content; got != "Hello world" {
					t.Fatalf("assistant content = %q, want %q", got, "Hello world")
				}
				if got := message.ReasoningContent; got != "runtime reasoning" {
					t.Fatalf("assistant reasoning_content = %q, want runtime reasoning", got)
				}
				return
			}
		}
	}
}

func TestRuntimeCancelsActiveTurn(t *testing.T) {
	streamer := &blockingStreamer{started: make(chan struct{})}
	runtime, err := NewRuntime(
		context.Background(),
		func(ctx context.Context, _ config.ProviderConfig) (string, error) { return "token", nil },
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
			Provider: testRuntimeProvider(),
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
		context.Background(),
		func(ctx context.Context, _ config.ProviderConfig) (string, error) { return "token", nil },
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
			Provider: testRuntimeProvider(),
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

func TestRuntimeReloadExtensionsWhileIdle(t *testing.T) {
	reloader := &fakeRuntimeReloader{
		result: ExtensionReloadResult{ExtensionCount: 1},
	}
	runtime, err := NewRuntime(
		context.Background(),
		func(ctx context.Context, _ config.ProviderConfig) (string, error) { return "token", nil },
		fakeStreamer{},
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()
	runtime.SetExtensionReloader(reloader)

	sub, err := runtime.SubscribeEvents(8)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer sub.Unsubscribe()

	if err := runtime.Send(StartChatSessionCommand{
		SessionID: "s1",
		Config: ChatSessionConfig{
			Provider: testRuntimeProvider(),
			Model:    ChatModelRef{ID: "nemotron", URL: "https://model.example"},
		},
	}); err != nil {
		t.Fatalf("Send(start) error = %v", err)
	}
	if err := runtime.Send(ReloadExtensionsCommand{RequestedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("Send(reload) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reload event")
		case event := <-sub.Channel():
			reloaded, ok := event.(ExtensionsReloadedEvent)
			if !ok {
				continue
			}
			if reloaded.Result.ExtensionCount != 1 {
				t.Fatalf("extension count = %d, want 1", reloaded.Result.ExtensionCount)
			}
			if reloader.calls != 1 || !reloader.lastIdle {
				t.Fatalf("reloader calls = %d idle = %v, want 1 true", reloader.calls, reloader.lastIdle)
			}
			return
		}
	}
}

func TestRuntimeReloadExtensionsWhileActiveRejects(t *testing.T) {
	reloader := &fakeRuntimeReloader{}
	streamer := &blockingStreamer{started: make(chan struct{})}
	runtime, err := NewRuntime(
		context.Background(),
		func(ctx context.Context, _ config.ProviderConfig) (string, error) { return "token", nil },
		streamer,
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	defer runtime.Close()
	runtime.SetExtensionReloader(reloader)

	sub, err := runtime.SubscribeEvents(8)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer sub.Unsubscribe()

	if err := runtime.Send(StartChatSessionCommand{
		SessionID: "s1",
		Config: ChatSessionConfig{
			Provider: testRuntimeProvider(),
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
	if err := runtime.Send(ReloadExtensionsCommand{RequestedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("Send(reload) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reload notification")
		case event := <-sub.Channel():
			notification, ok := event.(ChatNotificationEvent)
			if !ok {
				continue
			}
			if notification.Message == "Extension reload is only available while idle" {
				if reloader.calls != 0 {
					t.Fatalf("reloader calls = %d, want 0", reloader.calls)
				}
				return
			}
		}
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
	bearerToken string,
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
	bearerToken string,
	onDelta func(string) error,
) (CompletionResult, error) {
	close(b.started)
	<-ctx.Done()
	return CompletionResult{}, ctx.Err()
}

type fakeRuntimeReloader struct {
	calls    int
	lastIdle bool
	result   ExtensionReloadResult
	err      error
}

func (r *fakeRuntimeReloader) ReloadExtensions(_ context.Context, idle bool) (ExtensionReloadResult, error) {
	r.calls++
	r.lastIdle = idle
	return r.result, r.err
}

func (r *fakeRuntimeReloader) ExtensionCommands() []ExtensionCommand {
	return r.result.Commands
}

func (r *fakeRuntimeReloader) RunExtensionCommand(context.Context, string, string, any) (string, error) {
	return "", nil
}
