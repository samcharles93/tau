package store

import (
	"context"
	"time"

	"github.com/samcharles93/tau/internal/chat"
)

// SessionSummary is a metadata-only view of a saved session. It is the wire
// type used for session listing — no full message content is included.
type SessionSummary struct {
	ID              string    `json:"id"`
	ModelID         string    `json:"model_id"`
	Provider        string    `json:"provider"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Status          string    `json:"status"`
	MessageCount    int       `json:"message_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	TotalTokens     int       `json:"total_tokens"`
	Cost            float64   `json:"cost"`
	DurationMs      int64     `json:"duration_ms"`
	ToolCalls       int       `json:"tool_calls"`
	ToolErrors      int       `json:"tool_errors"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
}

// AgentInstance is the wire type for a persisted agent instance row.
type AgentInstance struct {
	ID               string    `json:"id"`
	SpecName         string    `json:"spec_name"`
	SpecScope        string    `json:"spec_scope"`
	SpecSourcePath   string    `json:"spec_source_path,omitempty"`
	SpecHash         string    `json:"spec_hash"`
	SpecSnapshot     string    `json:"spec_snapshot"`
	ResolvedProvider string    `json:"resolved_provider"`
	ResolvedModel    string    `json:"resolved_model"`
	EffectiveTools   string    `json:"effective_tools,omitempty"`
	Depth            int       `json:"depth"`
	ParentInstanceID string    `json:"parent_instance_id,omitempty"`
	PID              int       `json:"pid,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
	ExitStatus       string    `json:"exit_status,omitempty"`
	UsageJSON        string    `json:"usage_json,omitempty"`
}

// SessionStore persists and retrieves chat sessions. SQLite is the single
// source of truth. JSONL export files are a derived artifact — the app never
// reads them for its own operations.
type SessionStore interface {
	// Save writes session metadata and messages to SQLite. It is an upsert —
	// existing sessions are updated, new sessions are inserted.
	Save(ctx context.Context, state chat.ChatSessionState, duration time.Duration) error

	// Load reconstructs a full ChatSessionState from SQLite.
	Load(ctx context.Context, id string) (chat.ChatSessionState, error)

	// List returns paginated session summaries, newest first. Cursor is the
	// created_at value to resume from (empty for first page). The returned
	// nextCursor is empty when there are no more pages.
	List(ctx context.Context, limit int, cursor string) ([]SessionSummary, string, error)

	// Delete removes a session and its messages (via CASCADE).
	Delete(ctx context.Context, id string) error

	// ExportMessages streams all messages for a session as JSON-encoded lines.
	// The caller reads from the returned channel and handles any errors from
	// the error channel. Both channels are closed when export completes.
	ExportMessages(ctx context.Context, id string) (<-chan []byte, <-chan error)

	// Count returns the total number of stored sessions.
	Count(ctx context.Context) (int, error)

	// Close cleans up the database connection.
	Close() error

	// SessionJSONLPath returns the canonical JSONL export file path for a session.
	SessionJSONLPath(sessionID string, createdAt time.Time) string

	// SaveAgentInstance persists a new agent instance row.
	SaveAgentInstance(ctx context.Context, inst AgentInstance) error

	// CloseAgentInstance marks an instance as ended with the given status
	// and usage totals.
	CloseAgentInstance(ctx context.Context, id, exitStatus, usageJSON string) error

	// GetAgentInstance returns a single instance by id.
	GetAgentInstance(ctx context.Context, id string) (AgentInstance, error)

	// ListAgentInstances returns instances with the given parent, ordered
	// by started_at desc. Empty parentID lists root instances.
	ListAgentInstances(ctx context.Context, parentID string) ([]AgentInstance, error)

	// ListChildren returns sessions that have the given parent_session_id,
	// ordered by created_at desc.
	ListChildren(ctx context.Context, parentSessionID string) ([]SessionSummary, error)
}
