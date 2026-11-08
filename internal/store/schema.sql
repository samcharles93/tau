-- schema.sql is the reference schema for session persistence.
-- The actual migrations live in migrate.go; this file documents the latest state.
-- Migration V1 creates this full schema.

-- schema_version tracks applied migrations.
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- sessions stores per-session metadata.
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    model_id      TEXT NOT NULL,
    provider      TEXT NOT NULL,
    created_at    TEXT NOT NULL,  -- ISO 8601
    updated_at    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',  -- active | closed
    message_count INTEGER NOT NULL DEFAULT 0,
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read    INTEGER NOT NULL DEFAULT 0,
    cache_write   INTEGER NOT NULL DEFAULT 0,
    total_tokens  INTEGER NOT NULL DEFAULT 0,
    cost          REAL    NOT NULL DEFAULT 0.0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    system_prompt       TEXT    NOT NULL DEFAULT '',
    parent_session_id   TEXT    REFERENCES sessions(id) ON DELETE SET NULL  -- orphans children when parent is deleted
);

CREATE INDEX idx_sessions_created ON sessions(created_at DESC);
CREATE INDEX idx_sessions_status  ON sessions(status);
CREATE INDEX idx_sessions_parent  ON sessions(parent_session_id);

-- messages stores the full conversation history.
CREATE TABLE IF NOT EXISTS messages (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq               INTEGER NOT NULL,
    role              TEXT    NOT NULL,
    content           TEXT    NOT NULL DEFAULT '',
    reasoning_content TEXT    NOT NULL DEFAULT '',
    tool_calls        TEXT    NOT NULL DEFAULT '',  -- JSON array, empty string if none
    tool_call_id      TEXT    NOT NULL DEFAULT '',
    created_at        TEXT    NOT NULL,  -- ISO 8601
    client_id         TEXT    NOT NULL DEFAULT '',  -- stable app-assigned ChatMessage.ID (see chat.NewMessageID); '' for pre-migration rows
    tool_result       TEXT    NOT NULL DEFAULT '',  -- JSON ToolResultMetadata, empty for non-tool and legacy rows
    UNIQUE(session_id, seq)
);

CREATE INDEX idx_messages_session ON messages(session_id, seq);
CREATE INDEX idx_messages_client_id ON messages(session_id, client_id);

-- messages_fts is the full-text search index over message content (migration
-- v11). Regular (not external-content) FTS5 table: content/role are indexed
-- with the porter stemmer, session_id/seq are stored unindexed for grouping.
-- Triggers keep it in sync with messages on insert/delete/update; the FTS
-- rowid is messages.id, so search hits join back to messages for created_at.
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
