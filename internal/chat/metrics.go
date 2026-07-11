// Package chat provides the domain types for Tau's chat system, including
// the MetricEvent type used for observability across the event bus.
package chat

import "time"

// MetricCategory groups metrics by subsystem for subscriber filtering.
type MetricCategory string

const (
	MetricCategoryLLM       MetricCategory = "llm"       // token counts, cost, latency
	MetricCategoryTool      MetricCategory = "tool"      // tool invocations, durations, errors
	MetricCategorySkill     MetricCategory = "skill"     // skill activations
	MetricCategoryExtension MetricCategory = "extension" // extension commands
	MetricCategorySession   MetricCategory = "session"   // session lifecycle
)

// MetricEvent is published on the event bus by the coordinator. A single type
// with a Category discriminator keeps the bus API simple: one Publisher, any
// number of Subscribers, each filtering by category.
//
// MetricEvent is NOT a ChatEvent — it routes on its own bus topic, separate
// from ChatSessionSnapshotEvent and friends.
type MetricEvent struct {
	Category  MetricCategory    `json:"category"`
	Name      string            `json:"name"`   // e.g. "tool.read.duration", "llm.response"
	Value     float64           `json:"value"`  // numeric value (tokens, ms, count, usd)
	Unit      string            `json:"unit"`   // "tokens", "ms", "count", "usd"
	Labels    map[string]string `json:"labels"` // provider, model, tool, status, error_kind
	SessionID string            `json:"session_id"`
	RequestID string            `json:"request_id,omitempty"`
	CallID    string            `json:"call_id,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// CostPerMillionTokens is the standard pricing unit for LLM APIs.
const CostPerMillionTokens = 1_000_000
