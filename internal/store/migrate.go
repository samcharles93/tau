package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// currentSchemaVersion is the latest migration number. Bump this when adding
// new migrations and append the SQL to the migrations map below.
const currentSchemaVersion = 11

// CurrentSchemaVersion returns the latest migration number the store migrates
// to on open.
func CurrentSchemaVersion() int { return currentSchemaVersion }

// Migrate runs all pending schema migrations in order. It is idempotent -
// already-applied migrations are skipped.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaVersionTable(db, ctx); err != nil {
		return err
	}

	current, err := appliedVersion(db, ctx)
	if err != nil {
		return err
	}

	migrations := map[int]string{
		1:  migrationV1,
		2:  migrationV2,
		3:  migrationV3,
		4:  migrationV4,
		5:  migrationV5,
		6:  migrationV6,
		7:  migrationV7,
		8:  migrationV8,
		9:  migrationV9,
		10: migrationV10,
		11: migrationV11,
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

var migrationV2 = strings.TrimSpace(`
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id);
`)

var migrationV3 = strings.TrimSpace(`
ALTER TABLE sessions ADD COLUMN tool_calls INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN tool_errors INTEGER NOT NULL DEFAULT 0;
`)

// migrationV4 adds a stable, client-assigned message identity. The table's
// existing `id INTEGER PRIMARY KEY AUTOINCREMENT` is not usable for this -
// Save() deletes and re-inserts every message on every save, so that
// autoincrement value is reassigned each time, not stable across saves.
// client_id is a separate, application-generated string (see
// chat.NewMessageID) that round-trips unchanged.
var migrationV4 = strings.TrimSpace(`
ALTER TABLE messages ADD COLUMN client_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_messages_client_id ON messages(session_id, client_id);
`)

// migrationV5 persists authoritative tool execution metadata rather than
// requiring consumers to infer failures and durations from result prose.
var migrationV5 = strings.TrimSpace(`
ALTER TABLE messages ADD COLUMN tool_result TEXT NOT NULL DEFAULT '';
`)

// migrationV6 adds the agent_instances table for process-identity tracking
// and extends sessions with an agent_instance_id for lineage. See
// docs/specs/agents/04-storage-and-sessions.md for the full schema design.
var migrationV6 = strings.TrimSpace(`
CREATE TABLE IF NOT EXISTS agent_instances (
    id                 TEXT PRIMARY KEY,
    spec_name          TEXT NOT NULL,
    spec_scope         TEXT NOT NULL,
    spec_source_path   TEXT,
    spec_hash          TEXT NOT NULL,
    spec_snapshot      TEXT NOT NULL,
    resolved_provider  TEXT NOT NULL,
    resolved_model     TEXT NOT NULL,
    effective_tools    TEXT,
    depth              INTEGER NOT NULL DEFAULT 0,
    parent_instance_id TEXT REFERENCES agent_instances(id) ON DELETE SET NULL,
    pid                INTEGER,
    started_at         TIMESTAMP NOT NULL,
    ended_at           TIMESTAMP,
    exit_status        TEXT,
    usage_json         TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_instances_parent ON agent_instances(parent_instance_id);

ALTER TABLE sessions ADD COLUMN agent_instance_id TEXT
    REFERENCES agent_instances(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_agent_instance ON sessions(agent_instance_id);
`)

// migrationV7 adds process-start identity and structured spawn-failure
// detail to agent_instances, per docs/specs/agents/
// 04-storage-and-sessions.md (Orphan sweep: PID check with process-start
// identity, Stale-age bound). process_start_ns disambiguates a live PID
// from a recycled one (see G10 review gap).
var migrationV7 = strings.TrimSpace(`
ALTER TABLE agent_instances ADD COLUMN process_start_ns INTEGER;
ALTER TABLE agent_instances ADD COLUMN failure_reason TEXT;
`)

// migrationV8 adds a composite (created_at, id) index so paginated List
// queries that use the composite cursor stay indexed.
var migrationV8 = strings.TrimSpace(`
CREATE INDEX IF NOT EXISTS idx_sessions_created_id ON sessions(created_at DESC, id DESC);
`)

// migrationV9 adds a human-meaningful title column to sessions (populated by
// Save from the first user message; List also derives it on the fly for rows
// saved before this column existed) and a composite (updated_at, id) index
// so List's last-activity ordering stays indexed.
var migrationV9 = strings.TrimSpace(`
ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sessions_updated_id ON sessions(updated_at DESC, id DESC);
`)

// migrationV10 adds an attachments column to messages, holding a JSON array
// of chat.ChatAttachment (inline image/file payloads referenced from the
// message content via placeholder tokens like "[Image #1]"). Empty string
// means no attachments, so pre-existing rows decode as nil.
var migrationV10 = strings.TrimSpace(`
ALTER TABLE messages ADD COLUMN attachments TEXT NOT NULL DEFAULT '';
`)

// migrationV11 adds a full-text search index over message content. The FTS5
// table duplicates content/role (porter stemming + unicode61 tokenizer) and
// keeps session_id/seq as unindexed columns for grouping; triggers keep it in
// sync with messages on insert/delete/update, and the final INSERT backfills
// rows persisted before this migration. messages.id is the rowid alias, so
// the FTS rowid joins back to messages for created_at.
var migrationV11 = strings.TrimSpace(`
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    role,
    session_id UNINDEXED,
    seq UNINDEXED,
    tokenize = 'porter unicode61'
);
CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content, role, session_id, seq)
    VALUES (new.id, new.content, new.role, new.session_id, new.seq);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
    DELETE FROM messages_fts WHERE rowid = old.id;
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
    DELETE FROM messages_fts WHERE rowid = old.id;
    INSERT INTO messages_fts(rowid, content, role, session_id, seq)
    VALUES (new.id, new.content, new.role, new.session_id, new.seq);
END;
INSERT INTO messages_fts(rowid, content, role, session_id, seq)
    SELECT id, content, role, session_id, seq FROM messages;
`)
