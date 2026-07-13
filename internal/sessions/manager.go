// Package sessions provides session persistence CRUD with runtime-config
// merging. It wraps store.SessionStore and handles type conversion between
// store-level and chat-level types, keeping persistence details out of the
// agent coordinator.
package sessions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

// RuntimeSessionConfig carries the runtime-only fields that must be spliced
// into a loaded session state after a store.Load(). These fields are not
// persisted by the store — they are live runtime configuration only.
type RuntimeSessionConfig struct {
	Provider    config.ProviderConfig
	ModelID     string
	ModelURL    string
	ModelConfig config.ModelConfig
	Parameters  chat.ChatParameters
}

// Manager wraps a store.SessionStore and provides CRUD operations that
// return chat-level types. It handles the runtime-config merge that the
// raw store does not provide.
type Manager struct {
	store store.SessionStore
}

// NewManager creates a Manager backed by the given SessionStore.
func NewManager(s store.SessionStore) *Manager {
	return &Manager{store: s}
}

// Store returns the underlying SessionStore, for callers (e.g. the agent
// tool) that need direct store access rather than the chat-level CRUD
// wrapper this Manager provides.
func (m *Manager) Store() store.SessionStore {
	return m.store
}

// OpenStore creates the default SQLite session store using the standard
// config paths. It creates the sessions directory if needed.
// Returns an error when the store cannot be opened — callers should warn
// and proceed without persistence.
func OpenStore() (store.SessionStore, error) {
	sessionsDir := config.SessionsDir()
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating sessions directory: %w", err)
	}
	dbPath := config.SessionsDBPath()
	return store.NewSQLiteStore(context.Background(), dbPath, sessionsDir)
}

// List returns paginated session summaries as chat types, newest first.
func (m *Manager) List(ctx context.Context, limit int, cursor string) ([]chat.SessionSummary, string, error) {
	summaries, nextCursor, err := m.store.List(ctx, limit, cursor)
	if err != nil {
		return nil, "", err
	}
	result := make([]chat.SessionSummary, len(summaries))
	for i, s := range summaries {
		result[i] = chat.SessionSummary{
			ID:              s.ID,
			ModelID:         s.ModelID,
			Provider:        s.Provider,
			CreatedAt:       s.CreatedAt,
			UpdatedAt:       s.UpdatedAt,
			Status:          s.Status,
			MessageCount:    s.MessageCount,
			InputTokens:     s.InputTokens,
			OutputTokens:    s.OutputTokens,
			TotalTokens:     s.TotalTokens,
			Cost:            s.Cost,
			DurationMs:      s.DurationMs,
			ToolCalls:       s.ToolCalls,
			ToolErrors:      s.ToolErrors,
			SystemPrompt:    s.SystemPrompt,
			ParentSessionID: s.ParentSessionID,
			AgentInstanceID: s.AgentInstanceID,
		}
	}
	return result, nextCursor, nil
}

// Load loads a session from the store and optionally merges runtime config.
// In-flight fields (Status, ActiveRequestID, PendingAssistant, LastError)
// are always reset to idle. If runtimeCfg is non-nil, the Provider, Model
// URL/Config, and Parameters are merged on top of the stored state.
func (m *Manager) Load(ctx context.Context, id string, runtimeCfg *RuntimeSessionConfig) (chat.ChatSessionState, error) {
	loaded, err := m.store.Load(ctx, id)
	if err != nil {
		return loaded, err
	}

	// A loaded session is idle by definition — reset in-flight fields.
	loaded.Status = chat.ChatSessionIdle
	loaded.ActiveRequestID = ""
	loaded.PendingAssistant = ""
	loaded.LastError = ""

	if runtimeCfg != nil {
		loaded.Provider = runtimeCfg.Provider
		loaded.ProviderName = runtimeCfg.Provider.Name
		loaded.Parameters = runtimeCfg.Parameters
		// Only replace Model.URL/Config when the stored model ID matches
		// the runtime model, or when the stored URL is empty (migration
		// edge case for sessions saved before model URL was tracked).
		if loaded.Model.ID == runtimeCfg.ModelID || loaded.Model.URL == "" {
			loaded.Model.URL = runtimeCfg.ModelURL
			loaded.Model.Config = runtimeCfg.ModelConfig
		}
		if loaded.Model.URL == "" {
			loaded.Model.URL = strings.TrimRight(loaded.Provider.BaseURL, "/")
		}
	}

	return loaded, nil
}

// Delete removes a saved session and its messages.
func (m *Manager) Delete(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

// Save persists session state to the store.
func (m *Manager) Save(ctx context.Context, state chat.ChatSessionState, duration time.Duration) error {
	return m.store.Save(ctx, state, duration)
}

// ExportMessages streams a session's messages as JSON-encoded lines.
func (m *Manager) ExportMessages(ctx context.Context, id string) (<-chan []byte, <-chan error) {
	return m.store.ExportMessages(ctx, id)
}

// ExportToJSONL writes a session's messages to a JSONL file at outputPath.
func (m *Manager) ExportToJSONL(ctx context.Context, id, outputPath string) error {
	return store.ExportSessionAsJSONL(ctx, m.store, id, outputPath)
}

// Count returns the number of saved sessions.
func (m *Manager) Count(ctx context.Context) (int, error) {
	return m.store.Count(ctx)
}

// Close closes the underlying store.
func (m *Manager) Close() error {
	return m.store.Close()
}

// SessionJSONLPath returns the canonical JSONL export file path for a session.
func (m *Manager) SessionJSONLPath(sessionID string, createdAt time.Time) string {
	return m.store.SessionJSONLPath(sessionID, createdAt)
}
