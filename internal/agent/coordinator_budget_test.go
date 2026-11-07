package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
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
// coordinator - fakeStreamer completes synchronously with no real I/O, so
// the coordinator's own goroutine can process a submitted turn and publish
// ChatResponseCompletedEvent fast enough to race ahead of a subscription
// created afterward (the bus has no replay buffer - see
// internal/eventbus - so a late subscriber simply never sees an event
// published before it existed). That race, not genuine slowness, was the
// actual cause of this file's intermittent CI timeouts; every other
// coordinator test in this package already subscribes before sending any
// command (e.g. coordinator_cancel_test.go), which this mirrors.
func newBudgetCoordinator(t *testing.T, maxTurns int, timeout time.Duration, maxTokens int, deadline time.Time) (*Coordinator, *fakeStreamer, *eventbus.Subscriber[chat.ChatEvent]) {
	t.Helper()
	fs := &fakeStreamer{}
	c, sub := newBudgetCoordinatorWith(t, fs, tools.NewRegistry(), maxTurns, timeout, maxTokens, deadline)
	return c, fs, sub
}

// newBudgetCoordinatorWith is newBudgetCoordinator with caller-supplied
// streamer/registry, for tests that need tool-calling iterations.
func newBudgetCoordinatorWith(t *testing.T, streamer Streamer, registry *tools.Registry, maxTurns int, timeout time.Duration, maxTokens int, deadline time.Time) (*Coordinator, *eventbus.Subscriber[chat.ChatEvent]) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	t.Cleanup(sub.Close)

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         bus,
		TokenSource: func(_ context.Context, _ config.ProviderConfig) (string, error) { return "t", nil },
		Streamer:    streamer,
		Registry:    registry,
		MaxTurns:    maxTurns,
		Timeout:     timeout,
		MaxTokens:   maxTokens,
		Deadline:    deadline,
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c, sub
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

// TestMaxTurnsResetsPerTurn guards tau-y4k: MaxTurns is a per-task cap, so
// the turn counter resets at the start of every new submitted turn. Two
// sequential turns with maxTurns=1 must both complete - previously the
// second turn failed immediately because turnCount accumulated coordinator-
// wide.
func TestMaxTurnsResetsPerTurn(t *testing.T) {
	c, fs, sub := newBudgetCoordinator(t, 1, 0, 0, time.Time{})
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce.BudgetExhausted, "turn 1 with maxTurns=1 should not trip")
	require.Equal(t, 1, fs.calls)

	startAndSubmit(t, c, "s2", "r2")
	ce2 := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce2.BudgetExhausted, "turn 2 gets a fresh per-task budget and must not trip")
	require.Equal(t, 2, fs.calls, "streamer must be called for turn 2")
}

// TestMaxTurnsTripsWithinTurn guards the other half: the cap still applies
// to agentic-loop iterations inside a single turn. A streamer that always
// emits a tool call must trip mid-turn once turnCount exceeds maxTurns.
func TestMaxTurnsTripsWithinTurn(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
		Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	}))
	streamer := &repeatedToolCallStreamer{tool: "search", args: `{"pattern":"x"}`, max: 50}
	c, sub := newBudgetCoordinatorWith(t, streamer, registry, 1, 0, 0, time.Time{})

	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.True(t, ce.BudgetExhausted, "tool-looping turn must trip once turnCount > maxTurns")
	require.True(t, ce.Partial)
	require.Equal(t, 1, streamer.calls, "streamer called once before the cap trips on iteration 2")
}

// TestTokenBudgetResetsPerTurn guards the token half of tau-y4k: cumulative
// tokens reset per turn, so sequential turns never trip each other's budget.
func TestTokenBudgetResetsPerTurn(t *testing.T) {
	c, fs, sub := newBudgetCoordinator(t, 0, 0, 200, time.Time{})
	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce.BudgetExhausted, "150 < 200 should not trip")

	startAndSubmit(t, c, "s2", "r2")
	ce2 := waitCompleted(t, sub, 15*time.Second)
	require.False(t, ce2.BudgetExhausted, "fresh turn resets cumulative tokens; 150 < 200 again")
	require.Equal(t, 2, fs.calls)
}

// TestTokenBudgetTripsWithinTurn guards that the token budget still applies
// across iterations of a single turn: a tool-looping turn accumulating 250
// tokens per iteration with maxTokens=200 trips on iteration 2.
func TestTokenBudgetTripsWithinTurn(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
		Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	}))
	streamer := &tokenToolStreamer{tool: "search", args: `{"pattern":"x"}`, usage: 250}
	c, sub := newBudgetCoordinatorWith(t, streamer, registry, 0, 0, 200, time.Time{})

	startAndSubmit(t, c, "s1", "r1")
	ce := waitCompleted(t, sub, 15*time.Second)
	require.True(t, ce.BudgetExhausted, "250 >= 200 after iteration 1 must trip iteration 2")
	require.True(t, ce.Partial)
	require.Equal(t, 1, streamer.calls)
}

// tokenToolStreamer always emits the same tool call with a fixed usage, so a
// single turn can accumulate token budget across iterations.
type tokenToolStreamer struct {
	calls int
	tool  string
	args  string
	usage int
}

func (s *tokenToolStreamer) StreamChatCompletionFull(
	context.Context, chat.ChatSessionState, string, map[string]string, chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.calls++
	return chat.CompletionResult{
		FinishReason: "tool_calls",
		Usage:        chat.ChatUsage{TotalTokens: s.usage},
		ToolCalls: []chat.ChatToolCall{{
			ID:       fmt.Sprintf("call_%d", s.calls),
			Type:     "function",
			Function: chat.ChatFunctionCall{Name: s.tool, Arguments: s.args},
		}},
	}, nil
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
