package metrics_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

// newHarness wires a bus, a usage tracker (which activates emission by
// subscribing), and a publisher standing in for the coordinator.
func newHarness(t *testing.T) (*metrics.UsageTracker, *eventbus.Publisher[chat.MetricEvent]) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)

	pub := eventbus.Publish[chat.MetricEvent](bus.Client("coordinator"))
	return tracker, pub
}

func llmResponse(session string, total float64, prompt, completion, provider, model string) chat.MetricEvent {
	return chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.response",
		Value:    total,
		Labels: map[string]string{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"provider":          provider,
			"model":             model,
		},
		SessionID: session,
	}
}

func TestUsageTracker_ParsesPromptAndCompletion(t *testing.T) {
	tracker, pub := newHarness(t)

	pub.Publish(llmResponse("s1", 30, "10", "20", "openai", "gpt-x"))

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.PromptTokens == 10 && s.CompletionTokens == 20
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.Equal(t, 30, s.TotalTokens)
	require.Equal(t, 10, s.LastPromptTokens)
	require.Equal(t, 1, s.TurnCount)
	require.Equal(t, "openai", s.LastProvider)
	require.Equal(t, "gpt-x", s.LastModel)
}

func TestUsageTracker_AccumulatesAndOverwritesLastPrompt(t *testing.T) {
	tracker, pub := newHarness(t)

	pub.Publish(llmResponse("s1", 30, "10", "20", "openai", "gpt-x"))
	pub.Publish(llmResponse("s1", 12, "5", "7", "openai", "gpt-x"))

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.TurnCount == 2
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.Equal(t, 15, s.PromptTokens)     // 10 + 5 accumulated
	require.Equal(t, 27, s.CompletionTokens) // 20 + 7 accumulated
	require.Equal(t, 42, s.TotalTokens)      // 30 + 12 accumulated
	require.Equal(t, 5, s.LastPromptTokens)  // overwritten to latest turn
}

func TestUsageTracker_MalformedPromptTokensIgnored(t *testing.T) {
	tracker, pub := newHarness(t)

	// A turn whose prompt_tokens label fails to parse must not corrupt totals
	// (strconv.Atoi error is ignored), but the turn still counts.
	bad := llmResponse("s1", 5, "not-a-number", "3", "openai", "gpt-x")
	pub.Publish(bad)

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.TurnCount == 1
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.Equal(t, 0, s.PromptTokens)
	require.Equal(t, 0, s.LastPromptTokens)
	require.Equal(t, 3, s.CompletionTokens)
}

func TestUsageTracker_CostAccumulates(t *testing.T) {
	tracker, pub := newHarness(t)

	cost := func(v float64) chat.MetricEvent {
		return chat.MetricEvent{
			Category:  chat.MetricCategoryLLM,
			Name:      "llm.cost",
			Value:     v,
			Labels:    map[string]string{"provider": "openai", "model": "gpt-x"},
			SessionID: "s1",
		}
	}
	pub.Publish(cost(0.01))
	pub.Publish(cost(0.02))

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.Cost > 0.029
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.InDelta(t, 0.03, s.Cost, 1e-9)
}

func TestUsageTracker_ToolCallsVsErrors(t *testing.T) {
	tracker, pub := newHarness(t)

	tool := func(name, status string, labels map[string]string) chat.MetricEvent {
		if labels == nil {
			labels = map[string]string{}
		}
		if status != "" {
			labels["status"] = status
		}
		return chat.MetricEvent{
			Category:  chat.MetricCategoryTool,
			Name:      name,
			Labels:    labels,
			SessionID: "s1",
		}
	}
	pub.Publish(tool("tool.read.duration", "success", map[string]string{"tool": "read"}))
	pub.Publish(tool("tool.write.duration", "error", map[string]string{"tool": "write"}))
	// Malformed-arguments event carries no status and must NOT count as an error.
	pub.Publish(tool("tool.arguments.malformed", "", map[string]string{"total_tool_calls": "2"}))

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.ToolCalls == 3
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.Equal(t, 1, s.ToolErrors)
}

func TestUsageTracker_SessionEventsIgnored(t *testing.T) {
	tracker, pub := newHarness(t)

	// A session lifecycle event must contribute nothing to the aggregates.
	pub.Publish(chat.MetricEvent{
		Category:  chat.MetricCategorySession,
		Name:      "session.created",
		Value:     999,
		SessionID: "s1",
	})
	pub.Publish(llmResponse("s1", 30, "10", "20", "openai", "gpt-x"))

	require.Eventually(t, func() bool {
		s := tracker.Snapshot("s1")
		return s != nil && s.TurnCount == 1
	}, time.Second, time.Millisecond)

	s := tracker.Snapshot("s1")
	require.Equal(t, 30, s.TotalTokens) // only the llm.response Value, not 999
}

func TestUsageTracker_MultiSessionIsolation(t *testing.T) {
	tracker, pub := newHarness(t)

	pub.Publish(llmResponse("s1", 30, "10", "20", "openai", "gpt-x"))
	pub.Publish(llmResponse("s2", 9, "5", "4", "anthropic", "claude"))

	require.Eventually(t, func() bool {
		a := tracker.Snapshot("s1")
		b := tracker.Snapshot("s2")
		return a != nil && b != nil && a.PromptTokens == 10 && b.PromptTokens == 5
	}, time.Second, time.Millisecond)

	require.Nil(t, tracker.Snapshot("unknown"))

	// Snapshot returns a copy: mutating it must not affect stored totals.
	s := tracker.Snapshot("s1")
	s.PromptTokens = 9999
	require.Equal(t, 10, tracker.Snapshot("s1").PromptTokens)
}
