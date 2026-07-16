package metrics

import (
	"strconv"
	"sync"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// SessionTotals holds aggregated metrics for one session.
type SessionTotals struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// LastPromptTokens is the prompt-token count of the most recent llm.response
	// turn (overwritten each turn), used to compute context-window utilisation.
	LastPromptTokens int
	Cost             float64
	TurnCount        int
	ToolCalls        int
	ToolErrors       int
	LastProvider     string
	LastModel        string
	// TurnDurationMs accumulates turn.duration metric values (ms).
	// This is wall-clock time for the entire agentic turn including
	// all LLM calls and tool executions in the loop.
	TurnDurationMs int64
	// SkillActivations counts unique skill activation events.
	SkillActivations int
	// ExtensionCommands counts extension command invocations.
	ExtensionCommands int
	// ModelSwitches counts model change events during the session.
	ModelSwitches int
	// ProviderSwitches counts provider change events during the session.
	ProviderSwitches int
	// TurnErrors counts failed or cancelled LLM turns.
	TurnErrors int
}

// UsageTracker aggregates MetricEvents per session. It subscribes to the
// bus on a dedicated client and exposes a snapshot of aggregated totals.
// Callers can query totals for a session to enrich session summaries, or
// to display live cost in the TUI.
type UsageTracker struct {
	sub *eventbus.SubscriberFunc[chat.MetricEvent]

	mu      sync.Mutex
	session map[string]*SessionTotals
}

// NewUsageTracker creates a tracker and subscribes it to MetricEvent on
// the given bus client. The client should be dedicated to the tracker so
// that its dispatch goroutine is not shared with other subscribers.
func NewUsageTracker(client *eventbus.Client) *UsageTracker {
	t := &UsageTracker{
		session: make(map[string]*SessionTotals),
	}
	t.sub = eventbus.SubscribeFunc(client, t.handle)
	return t
}

func (t *UsageTracker) handle(e chat.MetricEvent) {
	// Session lifecycle events (created, closed) carry no numeric
	// aggregates. State-change events (model.changed, provider.changed)
	// increment their respective counters below.

	t.mu.Lock()
	defer t.mu.Unlock()

	totals, ok := t.session[e.SessionID]
	if !ok {
		totals = &SessionTotals{}
		t.session[e.SessionID] = totals
	}

	switch e.Category {
	case chat.MetricCategoryLLM:
		switch e.Name {
		case "llm.response":
			totals.TotalTokens += int(e.Value)
			totals.TurnCount++
			if prompt, err := strconv.Atoi(e.Labels["prompt_tokens"]); err == nil {
				totals.PromptTokens += prompt
				totals.LastPromptTokens = prompt
			}
			if completion, err := strconv.Atoi(e.Labels["completion_tokens"]); err == nil {
				totals.CompletionTokens += completion
			}
			if provider, ok := e.Labels["provider"]; ok {
				totals.LastProvider = provider
			}
			if model, ok := e.Labels["model"]; ok {
				totals.LastModel = model
			}
		case "llm.cost":
			totals.Cost += e.Value
		case "turn.duration":
			totals.TurnDurationMs += int64(e.Value)
		case "llm.error":
			totals.TurnErrors++
		}

	case chat.MetricCategoryTool:
		totals.ToolCalls++
		if status, ok := e.Labels["status"]; ok && status == "error" {
			totals.ToolErrors++
		}

	case chat.MetricCategorySkill:
		switch e.Name {
		case "skill.activated":
			totals.SkillActivations++
		}

	case chat.MetricCategoryExtension:
		switch e.Name {
		case "extension.command":
			totals.ExtensionCommands++
		}

	case chat.MetricCategorySession:
		switch e.Name {
		case "session.model.changed":
			totals.ModelSwitches++
		case "session.provider.changed":
			totals.ProviderSwitches++
		default:
			// session.created, session.closed - no numeric aggregate.
		}
	}
}

// Snapshot returns a copy of the aggregated totals for a session.
// Returns nil if the session has no recorded metrics.
func (t *UsageTracker) Snapshot(sessionID string) *SessionTotals {
	t.mu.Lock()
	defer t.mu.Unlock()

	totals, ok := t.session[sessionID]
	if !ok {
		return nil
	}
	clone := *totals
	return &clone
}

// Close unsubscribes from the bus.
func (t *UsageTracker) Close() {
	t.sub.Close()
}
