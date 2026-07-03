package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"

	_ "modernc.org/sqlite" // database/sql driver
)

// SQLiteStore implements SessionStore backed by a local SQLite database.
// Messages are stored in the messages table. JSONL export files are written
// as a convenience artifact but the app never reads them.
type SQLiteStore struct {
	db          *sql.DB
	sessionsDir string // directory for JSONL export files
}

// NewSQLiteStore opens or creates the SQLite database at dbPath and runs
// pending migrations. sessionsDir is where JSONL export files are written.
func NewSQLiteStore(dbPath, sessionsDir string) (*SQLiteStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}

	// Belt and suspenders: enforce WAL mode and foreign keys after open.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enable WAL mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enable foreign keys: %w", err)
	}

	// Serialise all database operations through a single connection.
	// This is the canonical pattern for Go + SQLite: multiple connections
	// to the same file cause SQLITE_BUSY even under WAL mode because each
	// connection holds independent read/write locks at the pager level.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, sessionsDir: sessionsDir}, nil
}

// Save writes session metadata and messages to SQLite in a single transaction.
// Existing sessions are upserted; messages are deleted and re-inserted.
func (s *SQLiteStore) Save(ctx context.Context, state chat.ChatSessionState, duration time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	cost := calculateCost(state.Model.Config, state.LastUsage)

	_, err = tx.ExecContext(
		ctx, `
		INSERT INTO sessions (
			id, model_id, provider, created_at, updated_at, status,
			message_count, input_tokens, output_tokens, cache_read,
			cache_write, total_tokens, cost, duration_ms, tool_calls,
			tool_errors, system_prompt, parent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			updated_at        = excluded.updated_at,
			status            = excluded.status,
			message_count     = excluded.message_count,
			input_tokens      = excluded.input_tokens,
			output_tokens     = excluded.output_tokens,
			cache_read        = excluded.cache_read,
			cache_write       = excluded.cache_write,
			total_tokens      = excluded.total_tokens,
			cost              = excluded.cost,
			duration_ms       = excluded.duration_ms,
			tool_calls        = excluded.tool_calls,
			tool_errors       = excluded.tool_errors,
			system_prompt     = excluded.system_prompt,
			parent_session_id = excluded.parent_session_id
	`,
		state.SessionID,
		state.Model.ID,
		state.Provider.Name,
		formatTime(state.CreatedAt),
		formatTime(state.UpdatedAt),
		string(state.Status),
		len(state.Messages),
		state.LastUsage.PromptTokens,
		state.LastUsage.CompletionTokens,
		0, // cache_read — TODO(cache-tokens): ChatUsage needs cache token fields
		0, // cache_write
		state.LastUsage.TotalTokens,
		cost,
		duration.Milliseconds(),
		0, // tool_calls — populated from tracker snapshot on close
		0, // tool_errors
		state.SystemPrompt,
		nullString(state.ParentSessionID),
	)
	if err != nil {
		return fmt.Errorf("store: upsert session: %w", err)
	}

	// Delete existing messages and re-insert. Simple and correct for
	// the message counts we expect (<10K per session).
	if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE session_id = ?", state.SessionID); err != nil {
		return fmt.Errorf("store: delete messages: %w", err)
	}

	ins, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (session_id, seq, role, content, reasoning_content, tool_calls, tool_call_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("store: prepare message insert: %w", err)
	}
	defer ins.Close()

	for i, msg := range state.Messages {
		tcJSON := encodeToolCalls(msg.ToolCalls)
		msgTime := msg.CreatedAt
		if msgTime.IsZero() {
			// Fall back to synthesising from the session start for messages
			// persisted before per-message timestamps were tracked.
			msgTime = state.CreatedAt.Add(time.Duration(i) * time.Second)
		}

		if _, err := ins.ExecContext(
			ctx,
			state.SessionID, i,
			string(msg.Role),
			msg.Content,
			msg.ReasoningContent,
			tcJSON,
			msg.ToolCallID,
			formatTime(msgTime),
		); err != nil {
			return fmt.Errorf("store: insert message %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// Load reconstructs a ChatSessionState from SQLite. Returns an error if the
// session does not exist.
func (s *SQLiteStore) Load(ctx context.Context, id string) (chat.ChatSessionState, error) {
	var row struct {
		modelID, provider, createdStr, updatedStr, status, systemPrompt string
		parentID                                                        sql.NullString
		messageCount                                                    int
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT model_id, provider, created_at, updated_at, status, system_prompt,
		       message_count, parent_session_id
		FROM sessions WHERE id = ?
	`, id).Scan(
		&row.modelID, &row.provider, &row.createdStr, &row.updatedStr,
		&row.status, &row.systemPrompt, &row.messageCount, &row.parentID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return chat.ChatSessionState{}, fmt.Errorf("store: session %q not found", id)
		}
		return chat.ChatSessionState{}, fmt.Errorf("store: load session: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339, row.createdStr)
	updatedAt, _ := time.Parse(time.RFC3339, row.updatedStr)

	msgRows, err := s.db.QueryContext(ctx, `
		SELECT seq, role, content, reasoning_content, tool_calls, tool_call_id, created_at
		FROM messages WHERE session_id = ? ORDER BY seq
	`, id)
	if err != nil {
		return chat.ChatSessionState{}, fmt.Errorf("store: load messages: %w", err)
	}
	defer msgRows.Close()

	var messages []chat.ChatMessage
	for msgRows.Next() {
		var (
			seq                                  int
			role, content, reasoning             string
			toolCallsJSON, toolCallID, createdAt string
		)
		if err := msgRows.Scan(&seq, &role, &content, &reasoning, &toolCallsJSON, &toolCallID, &createdAt); err != nil {
			return chat.ChatSessionState{}, fmt.Errorf("store: scan message: %w", err)
		}
		msgCreatedAt, _ := time.Parse(time.RFC3339, createdAt)
		msg := chat.ChatMessage{
			Role:             chat.ChatRole(role),
			Content:          content,
			ReasoningContent: reasoning,
			ToolCalls:        decodeToolCalls(toolCallsJSON),
			ToolCallID:       toolCallID,
			CreatedAt:        msgCreatedAt,
		}
		messages = append(messages, msg)
	}
	if err := msgRows.Err(); err != nil {
		return chat.ChatSessionState{}, err
	}

	return chat.ChatSessionState{
		SessionID:       id,
		Model:           chat.ChatModelRef{ID: row.modelID},
		Provider:        config.ProviderConfig{Name: row.provider},
		ProviderName:    row.provider,
		SystemPrompt:    row.systemPrompt,
		ParentSessionID: row.parentID.String,
		Status:          chat.ChatSessionStatus(row.status),
		Messages:        messages,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// List returns paginated session summaries, newest first. The cursor is a
// created_at ISO 8601 string. Returns up to limit summaries plus a nextCursor
// (empty when there are no more pages).
func (s *SQLiteStore) List(ctx context.Context, limit int, cursor string) ([]SessionSummary, string, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT id, model_id, provider, created_at, updated_at, status,
		       message_count, input_tokens, output_tokens, total_tokens,
		       cost, duration_ms, tool_calls, tool_errors,
		       system_prompt, parent_session_id
		FROM sessions
	`
	var args []any

	if cursor != "" {
		query += " WHERE created_at < ?"
		args = append(args, cursor)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit+1) // extra row to detect next page

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var summaries []SessionSummary
	for rows.Next() {
		var createdStr, updatedStr string
		var parentID sql.NullString
		var sum SessionSummary
		if err := rows.Scan(
			&sum.ID, &sum.ModelID, &sum.Provider,
			&createdStr, &updatedStr, &sum.Status,
			&sum.MessageCount, &sum.InputTokens, &sum.OutputTokens,
			&sum.TotalTokens, &sum.Cost, &sum.DurationMs,
			&sum.ToolCalls, &sum.ToolErrors,
			&sum.SystemPrompt, &parentID,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan session row: %w", err)
		}
		sum.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		sum.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		sum.ParentSessionID = parentID.String
		summaries = append(summaries, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(summaries) > limit {
		// The extra row at index `limit` tells us there is a next page.
		// Use the last *visible* row's timestamp as the cursor so the
		// extra row is included in the following page.
		// NOTE: when multiple sessions share the same created_at, a
		// composite (created_at, id) cursor would be more robust; the
		// current single-column cursor may skip rows in that edge case.
		nextCursor = summaries[limit-1].CreatedAt.UTC().Format(time.RFC3339)
		summaries = summaries[:limit]
	}

	return summaries, nextCursor, nil
}

// Delete removes a session and its messages.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: session %q not found", id)
	}
	return nil
}

// ExportMessages streams all messages for a session as JSON-encoded lines.
func (s *SQLiteStore) ExportMessages(ctx context.Context, id string) (<-chan []byte, <-chan error) {
	out := make(chan []byte, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		rows, err := s.db.QueryContext(ctx, `
			SELECT role, content, reasoning_content, tool_calls, tool_call_id
			FROM messages WHERE session_id = ? ORDER BY seq
		`, id)
		if err != nil {
			errs <- err
			return
		}
		defer rows.Close()

		for rows.Next() {
			line, err := s.buildJSONLLine(rows)
			if err != nil {
				errs <- err
				return
			}
			select {
			case out <- line:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		if err := rows.Err(); err != nil {
			errs <- err
		}
	}()

	return out, errs
}

func (s *SQLiteStore) buildJSONLLine(scanner interface {
	Scan(dest ...any) error
},
) ([]byte, error) {
	var role, content, reasoning, toolCallsJSON, toolCallID string
	if err := scanner.Scan(&role, &content, &reasoning, &toolCallsJSON, &toolCallID); err != nil {
		return nil, err
	}

	line := map[string]any{
		"role":    role,
		"content": content,
	}
	if reasoning != "" {
		line["reasoning_content"] = reasoning
	}
	if toolCallsJSON != "" {
		var tc []json.RawMessage
		if err := json.Unmarshal([]byte(toolCallsJSON), &tc); err == nil && len(tc) > 0 {
			line["tool_calls"] = tc
		}
	}
	if toolCallID != "" {
		line["tool_call_id"] = toolCallID
	}

	data, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Count returns the total number of stored sessions.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count)
	return count, err
}

// SessionJSONLPath returns the canonical JSONL export file path for a session.
func (s *SQLiteStore) SessionJSONLPath(sessionID string, createdAt time.Time) string {
	filename := fmt.Sprintf(
		"%s_%s.jsonl",
		createdAt.UTC().Format("2006-01-02T15-04-05"),
		sessionID,
	)
	return filepath.Join(s.sessionsDir, filename)
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Helpers

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func encodeToolCalls(calls []chat.ChatToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	data, _ := json.Marshal(calls)
	return string(data)
}

func decodeToolCalls(jsonStr string) []chat.ChatToolCall {
	if jsonStr == "" {
		return nil
	}
	var calls []chat.ChatToolCall
	if err := json.Unmarshal([]byte(jsonStr), &calls); err != nil {
		return nil
	}
	return calls
}

// calculateCost estimates session cost from usage and model pricing.
// Returns 0 if cost config is unavailable.
//
// TODO(cache-tokens): ChatUsage does not currently track cache_read or
// cache_write token counts. When those fields are added, include them here
// using cfg.CacheRead and cfg.CacheWrite.
func calculateCost(cfg config.ModelConfig, usage chat.ChatUsage) float64 {
	cost := cfg.Cost
	if cost.Input == 0 && cost.Output == 0 {
		return 0
	}
	// Prices are per 1M tokens.
	inputCost := float64(usage.PromptTokens) * cost.Input / 1_000_000
	outputCost := float64(usage.CompletionTokens) * cost.Output / 1_000_000
	return inputCost + outputCost
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
