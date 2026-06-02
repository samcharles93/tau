# 10. Session Persistence with `/session` Command

## Status: Implemented

## Motivation

Session persistence is the single biggest UX gap (see gap analysis UX #1). Conversations vanish on exit with no way to resume, replay, export, or audit past sessions. Both `internal/store` (stub) and the session state machine exist — what's missing is the wiring and the TUI surface.

### Command Design

Two entry points with different affordances:

- **`/resume`** — fast path: opens an interactive session picker directly (list + fuzzy filter). No sub-commands.
- **`/session`** — full session management with sub-commands. Also opens the session list when used bare.

## Sub-commands

| Command | Description |
| ------- | ----------- |
| `/session` (bare) | Open interactive session list (paginated, sortable by date/model/tokens) |
| `/session export [id]` | Export session as JSONL to stdout or file |
| `/session exportHTML [id]` | Export as a standalone HTML page with the full TUI-rendered conversation (similar to Pi export) |
| `/session delete [id]` | Delete a session (with confirmation) |
| `/session info [id]` | Print session metadata to stdout in a formatted table |

### Picker UX

When the user runs `/session` or `/resume` in the TUI, a modal opens showing:

- Most recent 10 sessions (paginated)
- Each row: date, model, message count, token total, cost
- Up/Down to navigate, Enter to select, fuzzy filter via typing
- Selected session loads and replaces current conversation (with confirmation if unsaved changes)

### `/session info` Output Format

Inspired by Pi's session info output, adapted for Tau:

```shell
Session Info

File: /home/sam/.tau/sessions/2026-05-31T07-40-16-496Z_019e7cf9-f4f0-7270-8a63-6b1975803183.jsonl

ID: 019e7cf9-f4f0-7270-8a63-6b1975803183

Messages
User:       1
Assistant:  10
Tool Calls: 27
Tool Results: 27
Total:      38

Tokens
Input:      92,785
Output:     5,328
Cache Read: 592,256
Total:      690,369

Cost
Total:      $0.0161
```

### Implementation sketch

**Storage layer** (`internal/store/`):

- Use the pre-existing `store` package (currently a stub with only `doc.go`).
- SQLite via `sqlc` + migrations for session metadata (id, model, provider, created_at, total_tokens, cost).
- JSONL files on disk for full message history (append-only, easy to read/externalize).
- Storage path: `~/.tau/sessions/` (configurable via env).

**Chat layer changes** (`internal/chat/`):

- After a turn completes (`ChatResponseCompletedEvent`), the runtime persists the complete message list to the JSONL file.
- On session close, persist a final summary row to SQLite (token counts, cost, model, duration).
- Add a `ListSessions(limit, offset)` method and a `LoadSession(id)` method to the runtime interface.

**TUI changes** (`internal/tui/`):

- `/session` and `/resume` slash commands added to the handler.
- `SessionListPanel` — a scrollable list view with inline stats per row, similar to the existing `DebugListView` but data-driven.
- `SessionInfoPanel` — renders the formatted table block shown above in a modal.
- Loaded session replaces the current `messages` state, with a confirmation dialog if the current session has un-persisted changes.

**CLI changes** (`internal/cli/`):

- `tau sessions` — list recent sessions in a table.
- `tau sessions export <id>` — export to JSONL.
- `tau sessions resume <id>` — launch TUI with a pre-loaded session.

### Files to create/modify

| File | Change |
| ---- | ------ |
| `internal/store/schema.sql` | New: SQLite schema for sessions table |
| `internal/store/queries.sql` | New: CRUD queries for session metadata |
| `internal/store/session.go` | New: SessionStore interface + SQLite impl |
| `internal/chat/types.go` | Add `SessionSummary` type (metadata without full messages) |
| `internal/chat/runtime.go` | Add `ListSessions()` and `LoadSession()` methods |
| `internal/tui/chatui.go` | Add `/session`, `/resume` command handlers |
| `internal/tui/views/session_list.go` | New: session picker modal |
| `internal/tui/views/session_info.go` | New: session info display |
| `internal/cli/commands.go` | Add `tau sessions` sub-command |
| `internal/app/chat.go` | Wire session persistence into coordinator startup/teardown |

### Open Questions

1. Should auto-save happen after every turn (real-time) or only on graceful exit? Real-time is safer against crashes; on-exit is simpler.
2. Cost tracking requires model pricing data — should this come from config (`CostConfig`) or from an external pricing API?
