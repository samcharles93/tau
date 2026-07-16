package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// blockingStreamer blocks the streaming call until the test releases it, so
// we can race a cancel against the in-flight turn.
type blockingStreamer struct {
	release chan struct{}
	called  chan struct{}
	once    sync.Once
}

func newBlockingStreamer() *blockingStreamer {
	return &blockingStreamer{
		release: make(chan struct{}),
		called:  make(chan struct{}),
	}
}

func (s *blockingStreamer) StreamChatCompletionFull(
	ctx context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.once.Do(func() { close(s.called) })
	select {
	case <-s.release:
		return chat.CompletionResult{FinishReason: "stop"}, nil
	case <-ctx.Done():
		return chat.CompletionResult{}, ctx.Err()
	}
}

// TestCoordinatorCancelDuringInFlightTurn verifies that a cancel sent while
// the LLM call is in flight stops the request, regardless of whether the
// request_id matches.
func TestCoordinatorCancelDuringInFlightTurn(t *testing.T) {
	bus := newTestBus(t)
	streamer := newBlockingStreamer()
	released := make(chan struct{})

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer: &observeStreamer{inner: streamer, onReturn: func() { close(released) }},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	// Start a turn.
	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "web-real-id",
		Prompt:      "test prompt",
		SubmittedAt: time.Now().UTC(),
	}))

	// Wait until the streamer is mid-flight.
	select {
	case <-streamer.called:
	case <-time.After(time.Second):
		t.Fatal("streamer never invoked")
	}

	// Cancel with a *stale* id - the client lost track of which request is
	// active (e.g. Web UI re-submit race, reconnect, or a missed
	// ChatResponseStartedEvent). The coordinator must still cancel the
	// in-flight turn rather than refuse with a "not active" error.
	require.NoError(t, c.Send(chat.CancelChatRequestCommand{
		SessionID:   "session-1",
		RequestID:   "web-stale-id",
		RequestedAt: time.Now().UTC(),
	}))

	// The context should be cancelled, unblocking the streamer.
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("streamer was not released by cancel")
	}
}

// TestCoordinatorCancelWithNoActiveRequest verifies that a cancel arriving
// after a turn has finished (or before one has started) is a no-op rather
// than an error - the user's intent (stop waiting) is already satisfied.
func TestCoordinatorCancelWithNoActiveRequest(t *testing.T) {
	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	// Cancel before any turn has been submitted.
	require.NoError(t, c.Send(chat.CancelChatRequestCommand{
		SessionID:   "session-1",
		RequestID:   "anything",
		RequestedAt: time.Now().UTC(),
	}))

	// We expect only a snapshot event (no error event). Read until we see one.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-sub.Events():
			if _, ok := ev.(chat.ChatRuntimeErrorEvent); ok {
				t.Fatalf("unexpected runtime error: %+v", ev)
			}
		case <-deadline:
			return
		}
	}
}

// TestCoordinatorCancelWithEmptyRequestID verifies the Web UI's "I lost
// track of the request id" fallback still cancels the active turn.
func TestCoordinatorCancelWithEmptyRequestID(t *testing.T) {
	bus := newTestBus(t)
	streamer := newBlockingStreamer()
	released := make(chan struct{})

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer: &observeStreamer{inner: streamer, onReturn: func() { close(released) }},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)
	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "real",
		Prompt:      "p",
		SubmittedAt: time.Now().UTC(),
	}))
	<-streamer.called

	// Empty request_id from a client that lost its state.
	require.NoError(t, c.Send(chat.CancelChatRequestCommand{
		SessionID:   "session-1",
		RequestedAt: time.Now().UTC(),
	}))

	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("streamer was not released by empty-id cancel")
	}
}

// observeStreamer wraps another Streamer and signals on return so tests can
// wait for the LLM call to finish.
type observeStreamer struct {
	inner    Streamer
	onReturn func()
}

func (o *observeStreamer) StreamChatCompletionFull(
	ctx context.Context,
	state chat.ChatSessionState,
	token string,
	headers map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	res, err := o.inner.StreamChatCompletionFull(ctx, state, token, headers, cb)
	if o.onReturn != nil {
		o.onReturn()
	}
	return res, err
}
