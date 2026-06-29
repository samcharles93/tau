package metrics

import (
	"sync"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// SessionTotals holds aggregated metrics for one session.
type SessionTotals struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Cost             float64
	TurnCount        int
	ToolCalls        int
	ToolErrors       int
	LastProvider     string
	LastModel        string
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
	// Session lifecycle events don't carry numeric data to aggregate;
	// they signal that a session exists.
	if e.Category == chat.MetricCategorySession {
		return
	}

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
			if provider, ok := e.Labels["provider"]; ok {
				totals.LastProvider = provider
			}
			if model, ok := e.Labels["model"]; ok {
				totals.LastModel = model
			}
		case "llm.cost":
			totals.Cost += e.Value
		}

	case chat.MetricCategoryTool:
		totals.ToolCalls++
		if status, ok := e.Labels["status"]; ok && status == "error" {
			totals.ToolErrors++
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
