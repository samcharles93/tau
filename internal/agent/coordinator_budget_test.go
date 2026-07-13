package agent

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// fakeStreamer returns text-only responses with no tool calls.
type fakeStreamer struct{ calls int }

func (f *fakeStreamer) StreamChatCompletionFull(_ context.Context, _ chat.ChatSessionState, _ string, _ map[string]string, _ chat.StreamCallbacks) (chat.CompletionResult, error) {
	f.calls++
	return chat.CompletionResult{
		FinishReason: "stop",
		Usage:        chat.ChatUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}, nil
}

// newBudgetCoordinator subscribes to the bus BEFORE constructing the
// coordinator — fakeStreamer completes synchronously with no real I/O, so
// the coordinator's own goroutine can process a submitted turn and publish
// ChatResponseCompletedEvent fast enough to race ahead of a subscription
// created afterward (the bus has no replay buffer — see
// internal/eventbus — so a late subscriber simply never sees an event
// published before it existed). That race, not genuine slowness, was the
// actual cause of this file's intermittent CI timeouts; every other
// coordinator test in this package already subscribes before sending any
// command (e.g. coordinator_cancel_test.go), which this mirrors.
func newBudgetCoordinator(t *testing.T, maxTurns int, timeout time.Duration, maxTokens int, deadline time.Time) (*Coordinator, *fakeStreamer, *eventbus.Subscriber[chat.ChatEvent]) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	t.Cleanup(sub.Close)

	fs := &fakeStreamer{}
	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         bus,
		TokenSource: func(_ context.Context, _ config.ProviderConfig) (string, error) { return "t", nil },
		Streamer:    fs,
		Registry:    tools.NewRegistry(),
		MaxTurns:    maxTurns,
		Timeout:     timeout,
		MaxTokens:   maxTokens,
		Deadline:    deadline,
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c, fs, sub
}

func waitCompleted(t *testing.T, sub *eventbus.Subscriber[chat.ChatEvent], d time.Duration) *chat.ChatResponseCompletedEvent {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-sub.Events():
			if ce, ok := ev.(chat.ChatResponseCompletedEvent); ok {
				v := ce
				return &v
			}
		case <-deadline:
			t.Fatal("timeout")
			return nil
		}
	}
}

func startAndSubmit(t *testing.T, c *Coordinator, sid, rid string) {
	t.Helper()
	require.NoError(t, c.Send(chat.StartChatSessionCommand{
		SessionID: sid,
		Config: chat.ChatSessionConfig{
			Provider:     config.ProviderConfig{Name: "t", BaseURL: "https://t"},
			Model:        chat.ChatModelRef{ID: "m"},
			SystemPrompt: "p",
		},
	}))
	require.NoError(t, c.Send(chat.SubmitChatPromptCommand{
		SessionID: sid, RequestID: rid, Prompt: "hi", SubmittedAt: time.Now().UTC(),
	}))
}

func TestMaxTurnsTrip(t *testing.T) {
	c, fs, sub := newBudgetCoordinator(t, 1, 0, 0, time.Time{})
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce.BudgetExhausted, "turn 1 with maxTurns=1 should not trip")
	require.Equal(t, 1, fs.calls)

	startAndSubmit(t, c, "s2", "r2")
	ce2 := waitCompleted(t, sub, 15*time.Second)
	require.True(t, ce2.BudgetExhausted, "turn 2 with maxTurns=1 should trip")
	require.True(t, ce2.Partial)
	require.Equal(t, 1, fs.calls, "streamer should not be called on limit breach")
}

func TestTokenBudgetTrip(t *testing.T) {
	c, _, sub := newBudgetCoordinator(t, 0, 0, 200, time.Time{})
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce.BudgetExhausted, "150 < 200 should not trip")

	startAndSubmit(t, c, "s2", "r2")
	ce2 := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce2.BudgetExhausted, "150 < 200 at start, passes, accumulates to 300")

	startAndSubmit(t, c, "s3", "r3")
	ce3 := waitCompleted(t, sub, 15*time.Second)
	require.True(t, ce3.BudgetExhausted, "300 >= 200 should trip on turn 3")
	require.True(t, ce3.Partial)
}

func TestDeadlineTrip(t *testing.T) {
	c, _, sub := newBudgetCoordinator(t, 0, 0, 0, time.Now().Add(-time.Hour))
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.True(t, ce.TimedOut, "deadline in the past should trip immediately")
	require.True(t, ce.Partial)
}

func TestNoLimitsNormal(t *testing.T) {
	c, _, sub := newBudgetCoordinator(t, 0, 0, 0, time.Time{})
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce.BudgetExhausted)
	require.False(t, ce.TimedOut)
	require.False(t, ce.Partial)
}
