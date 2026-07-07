package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// --- pure-function tests ----------------------------------------------------

func TestParseToolCallKeyIgnoresJustificationField(t *testing.T) {
	plain, jPlain := parseToolCallKey("grep", `{"pattern":"x","path":"/a"}`)
	justified, jJustified := parseToolCallKey("grep", `{"pattern":"x","path":"/a","repeat_justification":"polling for a state change"}`)
	if plain != justified {
		t.Fatalf("parseToolCallKey should ignore repeat_justification in key, got %q vs %q", plain, justified)
	}
	if jPlain != "" {
		t.Fatalf("expected empty justification, got %q", jPlain)
	}
	if jJustified != "polling for a state change" {
		t.Fatalf("expected justification 'polling for a state change', got %q", jJustified)
	}

	different, _ := parseToolCallKey("grep", `{"pattern":"y","path":"/a"}`)
	if plain == different {
		t.Fatal("parseToolCallKey should distinguish genuinely different arguments")
	}
}

func TestParseToolCallKeyExtractsJustification(t *testing.T) {
	_, got := parseToolCallKey("test", `{"pattern":"x"}`)
	if got != "" {
		t.Fatalf("expected empty justification, got %q", got)
	}
	_, got = parseToolCallKey("test", `{"pattern":"x","repeat_justification":"  re-running after a fix  "}`)
	if got != "re-running after a fix" {
		t.Fatalf("justification = %q, want trimmed value", got)
	}
	_, got = parseToolCallKey("test", `not valid json`)
	if got != "" {
		t.Fatalf("expected empty justification for invalid JSON, got %q", got)
	}
	// Whitespace-only justification should be treated as absent.
	_, got = parseToolCallKey("test", `{"repeat_justification":"   "}`)
	if got != "" {
		t.Fatalf("expected empty justification for whitespace-only, got %q", got)
	}
}

// --- end-to-end coordinator tests -------------------------------------------

// repeatedToolCallStreamer always returns the same tool call (name+args)
// until it has been called max times, then finishes the turn normally —
// a safety net in case the loop breaker fails to stop it.
type repeatedToolCallStreamer struct {
	calls int
	max   int
	tool  string
	args  string
}

func (s *repeatedToolCallStreamer) StreamChatCompletionFull(
	context.Context, chat.ChatSessionState, string, map[string]string, chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.calls++
	if s.calls > s.max {
		return chat.CompletionResult{FinishReason: "stop"}, nil
	}
	return chat.CompletionResult{
		FinishReason: "tool_calls",
		ToolCalls: []chat.ChatToolCall{{
			ID:       fmt.Sprintf("call_%d", s.calls),
			Type:     "function",
			Function: chat.ChatFunctionCall{Name: s.tool, Arguments: s.args},
		}},
	}, nil
}

// scriptedArgsStreamer returns one tool call per entry in argsSeq (in
// order), then finishes the turn.
type scriptedArgsStreamer struct {
	tool    string
	argsSeq []string
	calls   int
}

func (s *scriptedArgsStreamer) StreamChatCompletionFull(
	context.Context, chat.ChatSessionState, string, map[string]string, chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	if s.calls >= len(s.argsSeq) {
		return chat.CompletionResult{FinishReason: "stop"}, nil
	}
	args := s.argsSeq[s.calls]
	s.calls++
	return chat.CompletionResult{
		FinishReason: "tool_calls",
		ToolCalls: []chat.ChatToolCall{{
			ID:       fmt.Sprintf("call_%d", s.calls),
			Type:     "function",
			Function: chat.ChatFunctionCall{Name: s.tool, Arguments: args},
		}},
	}, nil
}

// TestToolLoopBreakerHardStopsRunawayRepetition guards against the real
// incident that motivated this feature: a model called grep with byte-
// identical arguments 103 times in a row, each getting a valid, non-empty
// result, without ever producing a final answer. Past the soft threshold,
// unjustified identical calls must be blocked (not actually executed), and
// after toolLoopHardBlockLimit consecutive unjustified blocks the turn must
// end outright rather than iterate forever.
func TestToolLoopBreakerHardStopsRunawayRepetition(t *testing.T) {
	execCount := 0
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
		Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
			execCount++
			return tools.Result{Content: "same result every time"}, nil
		},
	}))

	streamer := &repeatedToolCallStreamer{tool: "search", args: `{"pattern":"x"}`, max: 20}
	parallel := false

	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()
	metricsSub := eventbus.Subscribe[chat.MetricEvent](bus.Client("test-metrics"))
	defer metricsSub.Close()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:               bus,
		TokenSource:       func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:          streamer,
		Registry:          registry,
		ParallelToolCalls: &parallel,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "search repeatedly",
		SubmittedAt: time.Now().UTC(),
	}))

	var errMsg string
	metricCounts := map[string]int{}
	deadline := time.After(2 * time.Second)
	for errMsg == "" {
		select {
		case event := <-sub.Events():
			if ev, ok := event.(chat.ChatRuntimeErrorEvent); ok {
				errMsg = ev.Message
			}
		case ev := <-metricsSub.Events():
			metricCounts[ev.Name]++
		case <-deadline:
			t.Fatal("timed out waiting for the loop breaker to end the turn")
		}
	}
	// Drain any metrics still in flight after the runtime error fired.
	drainDeadline := time.After(100 * time.Millisecond)
drain:
	for {
		select {
		case ev := <-metricsSub.Events():
			metricCounts[ev.Name]++
		case <-drainDeadline:
			break drain
		}
	}

	require.Contains(t, errMsg, "runaway loop")
	// Calls 1..soft run for real; every call past the soft threshold
	// without justification must be intercepted, not executed.
	require.Equal(t, toolLoopSoftThreshold, execCount)

	// Observability: the block(s) leading up to the hard stop, and the hard
	// stop itself, must both be visible as metrics — not just as a single
	// generic "turn failed" log line.
	require.Equal(t, toolLoopHardBlockLimit-1, metricCounts["tool.loop.blocked"])
	require.Equal(t, 1, metricCounts["tool.loop.hard_stop"])
}

// TestToolLoopBreakerAllowsJustifiedOverride verifies the override path:
// once blocked, a repeat carrying a non-empty top-level
// "repeat_justification" argument must be allowed through and actually
// executed — legitimate repeated actions (re-running the same test, polling
// for a state change) must not be permanently blocked.
func TestToolLoopBreakerAllowsJustifiedOverride(t *testing.T) {
	var executedArgs []string
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
		Execute: func(_ context.Context, params json.RawMessage, _ tools.UIBridge) (tools.Result, error) {
			executedArgs = append(executedArgs, string(params))
			return tools.Result{Content: "same result"}, nil
		},
	}))

	baseArgs := `{"pattern":"x"}`
	justifiedArgs := `{"pattern":"x","repeat_justification":"re-running the same test after a fix"}`
	streamer := &scriptedArgsStreamer{tool: "search", argsSeq: []string{baseArgs, baseArgs, baseArgs, baseArgs, justifiedArgs}}
	parallel := false

	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()
	metricsSub := eventbus.Subscribe[chat.MetricEvent](bus.Client("test-metrics"))
	defer metricsSub.Close()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:               bus,
		TokenSource:       func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:          streamer,
		Registry:          registry,
		ParallelToolCalls: &parallel,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "search repeatedly, then justify",
		SubmittedAt: time.Now().UTC(),
	}))

	var sawCompleted, sawRuntimeError bool
	metricCounts := map[string]int{}
	deadline := time.After(2 * time.Second)
	for !sawCompleted && !sawRuntimeError {
		select {
		case event := <-sub.Events():
			switch event.(type) {
			case chat.ChatResponseCompletedEvent:
				sawCompleted = true
			case chat.ChatRuntimeErrorEvent:
				sawRuntimeError = true
			}
		case ev := <-metricsSub.Events():
			metricCounts[ev.Name]++
		case <-deadline:
			t.Fatal("timed out waiting for the turn to finish")
		}
	}

	require.False(t, sawRuntimeError, "turn should complete normally, not hard-stop, when justification is provided")
	require.True(t, sawCompleted)
	// Calls 1-3 run for real (under threshold); call 4 (unjustified, past
	// threshold) is blocked; call 5 (justified) runs for real.
	require.Len(t, executedArgs, 4)
	require.Contains(t, executedArgs[3], "repeat_justification")

	// Observability: both the block and the justified override must be
	// visible as metrics, not silently invisible once the model recovers.
	require.Equal(t, 1, metricCounts["tool.loop.blocked"])
	require.Equal(t, 1, metricCounts["tool.loop.justified"])
	require.Equal(t, 0, metricCounts["tool.loop.hard_stop"])
}
