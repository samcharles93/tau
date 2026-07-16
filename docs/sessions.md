# Session Persistence

Tau persists chat sessions to a local SQLite database (`~/.config/tau/sessions.db`). Sessions are saved automatically on
close and can be resumed, listed, exported, and deleted.

## Architecture

```
internal/sessions/manager.go   - Session lifecycle (create, update, close, branch)
internal/store/sqlite_store.go - SQLite implementation of SessionStore
internal/store/session.go      - SessionStore interface
internal/store/migrate.go      - Schema migrations
internal/store/jsonl_export.go - JSONL export
```

## SessionStore Interface

```go
// internal/store/session.go
type SessionStore interface {
    Save(ctx context.Context, state chat.ChatSessionState, duration time.Duration) error
    Load(ctx context.Context, sessionID string) (chat.ChatSessionState, error)
    List(ctx context.Context, limit int, cursor string) ([]SessionSummary, string, error)
    Delete(ctx context.Context, sessionID string) error
    ExportMessages(ctx context.Context, sessionID string) (<-chan []byte, <-chan error)
    Close() error
}
```

### Methods

| Method                      | Description                                                                                                   |
| --------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `Save(state, duration)`     | Persist a session's full state (messages, metadata, duration). Upserts - creates if new, updates if existing. |
| `Load(sessionID)`           | Load a session's full state including all messages.                                                           |
| `List(limit, cursor)`       | List session summaries with cursor-based pagination.                                                          |
| `Delete(sessionID)`         | Delete a session and all its messages.                                                                        |
| `ExportMessages(sessionID)` | Stream messages as JSONL (one JSON object per line) via channels.                                             |
| `Close()`                   | Close the database connection.                                                                                |

## SQLite Schema

The database uses a simple two-table schema with migrations for versioning:

### `sessions` table

| Column           | Type             | Description                                         |
| ---------------- | ---------------- | --------------------------------------------------- |
| `id`             | TEXT PRIMARY KEY | Session UUID                                        |
| `provider`       | TEXT             | Provider name                                       |
| `model_id`       | TEXT             | Model ID                                            |
| `model_config`   | TEXT (JSON)      | Model metadata (context window, cost, capabilities) |
| `system_prompt`  | TEXT             | Session system prompt                               |
| `parameters`     | TEXT (JSON)      | Temperature, max_tokens, reasoning_effort           |
| `status`         | TEXT             | Session status at save time                         |
| `show_reasoning` | INTEGER          | Whether reasoning was shown                         |
| `message_count`  | INTEGER          | Total message count                                 |
| `total_tokens`   | INTEGER          | Sum of prompt + completion tokens                   |
| `cost`           | REAL             | Estimated cost in USD                               |
| `duration_ms`    | INTEGER          | Session duration in milliseconds                    |
| `created_at`     | TEXT (ISO8601)   | Session creation time                               |
| `updated_at`     | TEXT (ISO8601)   | Last save time                                      |

### `messages` table

| Column              | Type                              | Description                           |
| ------------------- | --------------------------------- | ------------------------------------- |
| `id`                | INTEGER PRIMARY KEY AUTOINCREMENT | Message sequence                      |
| `session_id`        | TEXT (FK → sessions.id)           | Owning session                        |
| `role`              | TEXT                              | "system", "user", "assistant", "tool" |
| `content`           | TEXT                              | Message text content                  |
| `reasoning_content` | TEXT                              | Reasoning/chain-of-thought content    |
| `tool_calls`        | TEXT (JSON)                       | Serialized tool calls                 |
| `tool_call_id`      | TEXT                              | For "tool" role messages              |
| `name`              | TEXT                              | Optional tool/function name           |
| `created_at`        | TEXT (ISO8601)                    | Message creation time                 |

## Schema Migrations

`internal/store/migrate.go` manages schema versioning:

- Uses a `schema_version` table to track the current version.
- Migrations are applied sequentially in a transaction.
- New columns and tables are added via `ALTER TABLE` and `CREATE TABLE IF NOT EXISTS`.

## Session Manager

`internal/sessions/manager.go` wraps the store and provides lifecycle management:

```go
type Manager struct {
    store SessionStore
}

func NewManager(store SessionStore) *Manager
func (m *Manager) Save(ctx, state, duration) error
func (m *Manager) Load(ctx, sessionID, runtimeConfig) (ChatSessionState, error)
func (m *Manager) List(ctx, limit, cursor) ([]SessionSummary, string, error)
func (m *Manager) Delete(ctx, sessionID) error
func (m *Manager) ExportMessages(ctx, sessionID) (<-chan []byte, <-chan error)
func (m *Manager) Close() error
```

The manager handles:

- **Auto-save**: Sessions are saved after each completed turn by the coordinator.
- **Final save**: Sessions are saved on close with final state.
- **Resume**: Loading a session restores its messages, model, and parameters.
- **Runtime config merging**: When resuming, runtime config (model URL, provider auth) is merged with stored session
  state.

## Session Summaries

```go
type SessionSummary struct {
    ID           string    `json:"id"`
    ModelID      string    `json:"model_id"`
    Provider     string    `json:"provider"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
    Status       string    `json:"status"`
    MessageCount int       `json:"message_count"`
    TotalTokens  int       `json:"total_tokens"`
    Cost         float64   `json:"cost"`
}
```

Summaries are returned by `List()` for display in the TUI's `/sessions` command and the Web UI's SessionSwitcher.

## JSONL Export

`internal/store/jsonl_export.go` streams messages as JSONL (one JSON object per line):

```go
func ExportSessionAsJSONL(ctx context.Context, store SessionStore, sessionID string, w io.Writer) error
```

The export format:

```jsonl
{"role":"system","content":"You are a coding assistant..."}
{"role":"user","content":"Explain the event bus"}
{"role":"assistant","content":"The event bus...","tool_calls":null}
{"role":"tool","content":"...","tool_call_id":"call_abc"}
```

Tool calls are included as annotations on assistant messages. Tool results are separate messages with `tool_call_id`
linking them.

## App Integration

In `internal/app/run.go`, the session store is created at startup:

```go
rawStore, storeErr := sessions.OpenStore()
if storeErr != nil {
    slog.Warn("session store unavailable, sessions will not be persisted", "err", storeErr)
} else {
    sessionManager = sessions.NewManager(rawStore)
}
```

The store path defaults to `~/.config/tau/sessions.db`. If the store cannot be opened (e.g., permissions), tau continues
without persistence and logs a warning.

The session manager is passed to the coordinator, which calls `Save()` after each turn and on close.

## CLI Commands

### `tau sessions`

Lists saved sessions with a summary table (ID, model, message count, tokens, cost, date).

### `tau --resume <id>`

Resumes a session by ID. Use `--resume latest` for the most recent session. The session's messages, model, and
parameters are restored.

### `/export` (TUI command)

Exports the current session as JSONL. The file is saved to the sessions directory.

### `/sessions` (TUI command)

Lists saved sessions inline in the TUI with details.

## Web UI Session Management

The Web UI's `SessionSwitcher` component provides:

- List saved sessions (model, message count, date).
- Click to load a session (sends `LoadSessionCommand`).
- Delete button with confirmation.
- Export button (downloads JSONL via the bridge).
