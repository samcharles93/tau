package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestIsUnsupportedMediaError(t *testing.T) {
	media400 := &errx.ProviderError{
		Provider:   "test",
		StatusCode: http.StatusBadRequest,
		Message:    `{"error":{"message":"This model does not support image input"}}`,
		Base:       aisdkchat.ErrInvalidRequest,
	}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"typed unsupported content", aisdkchat.ErrUnsupportedContent, true},
		{"wrapped typed", fmt.Errorf("stream: %w", &aisdkchat.UnsupportedContentError{Provider: "p", Model: "m", PartType: aisdkchat.PartTypeImage}), true},
		{"400 blaming image", media400, true},
		{"wrapped 400 blaming vision", fmt.Errorf("chat stream: %w", &errx.ProviderError{Provider: "p", StatusCode: 400, Message: "vision not supported by this model", Base: aisdkchat.ErrInvalidRequest}), true},
		{"422 modality", &errx.ProviderError{Provider: "p", StatusCode: 422, Message: "unsupported modality: image", Base: aisdkchat.ErrInvalidRequest}, true},
		{"400 unrelated", &errx.ProviderError{Provider: "p", StatusCode: 400, Message: "max_tokens too large", Base: aisdkchat.ErrInvalidRequest}, false},
		{"500 image wording", &errx.ProviderError{Provider: "p", StatusCode: 500, Message: "image proxy exploded", Base: aisdkchat.ErrProviderUnavailable}, false},
		{"plain error", errors.New("connection reset"), false},
		{"nil-safe", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isUnsupportedMediaError(tc.err))
		})
	}
}

// failThenSucceedStreamer fails the first turn with the given error and
// completes normally afterwards, so tests can prove the session stays usable.
type failThenSucceedStreamer struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (s *failThenSucceedStreamer) StreamChatCompletionFull(
	_ context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		return chat.CompletionResult{}, s.err
	}
	_ = cb.OnDelta("recovered")
	return chat.CompletionResult{FinishReason: "stop"}, nil
}

func (s *failThenSucceedStreamer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// eventCollector drains ChatEvents off the bus into a mutex-guarded log so
// Eventually-poll assertions can inspect what the coordinator emitted.
type eventCollector struct {
	mu     sync.Mutex
	events []chat.ChatEvent
	sub    *eventbus.Subscriber[chat.ChatEvent]
	done   chan struct{}
}

func newEventCollector(t *testing.T, bus *eventbus.Bus) *eventCollector {
	t.Helper()
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("media-test"))
	ec := &eventCollector{sub: sub, done: make(chan struct{})}
	go func() {
		defer close(ec.done)
		for {
			select {
			case ev := <-sub.Events():
				ec.mu.Lock()
				ec.events = append(ec.events, ev)
				ec.mu.Unlock()
			case <-sub.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		sub.Close()
		<-ec.done
	})
	return ec
}

func (ec *eventCollector) lastState() chat.ChatSessionState {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	for _, v := range slices.Backward(ec.events) {
		if e, ok := v.(chat.ChatSessionSnapshotEvent); ok {
			return e.State
		}
	}
	return chat.ChatSessionState{}
}

func (ec *eventCollector) hasEvent(pred func(chat.ChatEvent) bool) bool {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return slices.ContainsFunc(ec.events, pred)
}

func newMediaTestCoordinator(t *testing.T, streamer *failThenSucceedStreamer) (*Coordinator, *eventCollector) {
	t.Helper()
	bus := newTestBus(t)
	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer: streamer,
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c, newEventCollector(t, bus)
}

func startMediaSession(t *testing.T, c *Coordinator, sessionID string) {
	t.Helper()
	require.NoError(t, c.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "http://test"},
			Model:    chat.ChatModelRef{ID: "text-only-model", URL: "http://test"},
		},
	}))
}

func TestCoordinatorRollsBackAttachmentMessageOnMediaRejection(t *testing.T) {
	streamer := &failThenSucceedStreamer{
		err: &errx.ProviderError{
			Provider:   "test",
			StatusCode: http.StatusBadRequest,
			Message:    "model does not support image input",
			Base:       aisdkchat.ErrInvalidRequest,
		},
	}
	c, ec := newMediaTestCoordinator(t, streamer)
	startMediaSession(t, c, "session_media")

	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session_media",
		RequestID:   "req_media",
		Prompt:      "describe [Image #1]",
		Attachments: []chat.ChatAttachment{{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{1, 2, 3}}},
		SubmittedAt: time.Now().UTC(),
	}))

	// The turn fails with a runtime error event...
	require.Eventually(t, func() bool {
		return ec.hasEvent(func(ev chat.ChatEvent) bool {
			_, ok := ev.(chat.ChatRuntimeErrorEvent)
			return ok
		})
	}, 3*time.Second, 10*time.Millisecond, "expected a ChatRuntimeErrorEvent")

	// ...the attachment message is rolled back in the latest snapshot...
	require.Eventually(t, func() bool {
		state := ec.lastState()
		for _, m := range state.Messages {
			if len(m.Attachments) > 0 {
				return false
			}
		}
		return len(state.Messages) == 0
	}, 3*time.Second, 10*time.Millisecond, "snapshot should have the attachment message rolled back")

	// ...and a warning notification explains the rollback.
	require.True(t, ec.hasEvent(func(ev chat.ChatEvent) bool {
		e, ok := ev.(chat.ChatNotificationEvent)
		return ok && e.Level == chat.ChatNotificationWarn && len(e.Message) >= 9 && e.Message[:9] == "rolled ba"
	}), "expected a rollback notification event")

	// The session must still accept new turns (not wedged).
	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session_media",
		RequestID:   "req_after",
		Prompt:      "plain text now",
		SubmittedAt: time.Now().UTC(),
	}))
	require.Eventually(t, func() bool {
		for _, m := range ec.lastState().Messages {
			if m.Role == chat.ChatRoleUser && m.Content == "plain text now" {
				return true
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "session should accept a new turn after rollback")
	require.Equal(t, 2, streamer.callCount())
}

func TestCoordinatorKeepsMessageOnUnrelatedFailure(t *testing.T) {
	streamer := &failThenSucceedStreamer{
		err: &errx.ProviderError{
			Provider:   "test",
			StatusCode: http.StatusBadRequest,
			Message:    "max_tokens exceeds limit",
			Base:       aisdkchat.ErrInvalidRequest,
		},
	}
	c, ec := newMediaTestCoordinator(t, streamer)
	startMediaSession(t, c, "session_keep")

	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session_keep",
		RequestID:   "req_keep",
		Prompt:      "describe [Image #1]",
		Attachments: []chat.ChatAttachment{{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{1}}},
		SubmittedAt: time.Now().UTC(),
	}))

	require.Eventually(t, func() bool {
		return ec.hasEvent(func(ev chat.ChatEvent) bool {
			_, ok := ev.(chat.ChatRuntimeErrorEvent)
			return ok
		})
	}, 3*time.Second, 10*time.Millisecond)

	// A non-media 400 must NOT roll back the message - the failure is
	// unrelated to the attachment.
	require.Eventually(t, func() bool {
		msgs := ec.lastState().Messages
		if len(msgs) == 0 {
			return false
		}
		last := msgs[len(msgs)-1]
		return last.Role == chat.ChatRoleUser && len(last.Attachments) == 1
	}, 3*time.Second, 10*time.Millisecond, "attachment message should be kept on unrelated 400")

	require.False(t, ec.hasEvent(func(ev chat.ChatEvent) bool {
		e, ok := ev.(chat.ChatNotificationEvent)
		return ok && e.Level == chat.ChatNotificationWarn
	}), "no rollback notification expected for unrelated failure")
}
