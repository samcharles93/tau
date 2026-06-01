package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// currentSchemaVersion is the latest migration number. Bump this when adding
// new migrations and append the SQL to the migrations map below.
const currentSchemaVersion = 1

// Migrate runs all pending schema migrations in order. It is idempotent —
// already-applied migrations are skipped.
func Migrate(db *sql.DB) error {
	ctx := context.Background()

	if err := ensureSchemaVersionTable(db, ctx); err != nil {
		return err
	}

	current, err := appliedVersion(db, ctx)
	if err != nil {
		return err
	}

	migrations := map[int]string{
		1: migrationV1,
	}

	for v := current + 1; v <= currentSchemaVersion; v++ {
		sqlText, ok := migrations[v]
		if !ok {
			return fmt.Errorf("store: missing migration %d", v)
		}
		if _, err := db.ExecContext(ctx, sqlText); err != nil {
			return fmt.Errorf("store: migration %d: %w", v, err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", v); err != nil {
			return fmt.Errorf("store: record migration %d: %w", v, err)
		}
	}

	return nil
}

func ensureSchemaVersionTable(db *sql.DB, ctx context.Context) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT    NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("store: ensure schema_version table: %w", err)
	}
	return nil
}

func appliedVersion(db *sql.DB, ctx context.Context) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}

var migrationV1 = strings.TrimSpace(`
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    model_id      TEXT NOT NULL,
    provider      TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    message_count INTEGER NOT NULL DEFAULT 0,
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read    INTEGER NOT NULL DEFAULT 0,
    cache_write   INTEGER NOT NULL DEFAULT 0,
    total_tokens  INTEGER NOT NULL DEFAULT 0,
    cost          REAL    NOT NULL DEFAULT 0.0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    system_prompt TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_status  ON sessions(status);

CREATE TABLE IF NOT EXISTS messages (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq               INTEGER NOT NULL,
    role              TEXT    NOT NULL,
    content           TEXT    NOT NULL DEFAULT '',
    reasoning_content TEXT    NOT NULL DEFAULT '',
    tool_calls        TEXT    NOT NULL DEFAULT '',
    tool_call_id      TEXT    NOT NULL DEFAULT '',
    created_at        TEXT    NOT NULL,
    UNIQUE(session_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
`)
