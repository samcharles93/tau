# Storage and Sessions

The shared SQLite store is the data plane. The store already enforces WAL mode, `busy_timeout=5000` and foreign keys on open (`internal/store/sqlite_store.go`), so concurrent access from a parent and several children is supported today. Session forking primitives already exist: `parent_session_id` column with index, `ParentSessionID` through config/state/summary, and `CloneChatSessionState` for deep clones.

## Data-plane rule

Large state never travels on the wire. Context, histories, artefacts and snapshots live in the store; envelopes carry identifiers, control fields and streamed deltas only (8 MiB line cap as a backstop). A child is told *where* its session is, not *what* is in it.

## Schema changes

Indicative DDL; exact form follows the migration conventions in `internal/store/migrate.go`.

```sql
CREATE TABLE agent_instances (
  id                 TEXT PRIMARY KEY,          -- "research#k3v9qp"
  spec_name          TEXT NOT NULL,
  spec_scope         TEXT NOT NULL,             -- builtin | user | project
  spec_source_path   TEXT,                      -- empty for built-ins
  spec_hash          TEXT NOT NULL,             -- sha256 of the raw spec file content
  spec_snapshot      TEXT NOT NULL,             -- JSON: resolved definition + body (see 01, Snapshot semantics)
  resolved_provider  TEXT NOT NULL,
  resolved_model     TEXT NOT NULL,
  effective_tools    TEXT,                      -- JSON array; NULL = unrestricted
  depth              INTEGER NOT NULL DEFAULT 0,
  parent_instance_id TEXT REFERENCES agent_instances(id) ON DELETE SET NULL,
  pid                INTEGER,
  started_at         TIMESTAMP NOT NULL,
  ended_at           TIMESTAMP,
  exit_status        TEXT,                      -- completed | failed | cancelled | budget_exhausted | timed_out
  failure_reason     TEXT,                      -- structured spawn-failure detail ("spawn: exec: permission denied")
  usage_json         TEXT                       -- cumulative totals at end
);
CREATE INDEX idx_agent_instances_parent ON agent_instances(parent_instance_id);

ALTER TABLE sessions ADD COLUMN agent_instance_id TEXT
  REFERENCES agent_instances(id) ON DELETE SET NULL;
CREATE INDEX idx_sessions_agent_instance ON sessions(agent_instance_id);
```

## Write responsibilities

| Writer | What it writes |
|--------|----------------|
| Spawner (parent, or root startup for itself) | Instance row + session create/fork **in one transaction**. `ended_at`/`exit_status`/`failure_reason`/`usage_json` when the child terminates or fails to start. |
| Child | Its own session (existing persistence path, unchanged), on the child's normal cadence plus always-before-exit. |
| Modes | Nothing new; modes create no instance rows. |

The parent owns instance-row lifecycle because it also owns process lifecycle (it holds the pipes and the exit code). The child owns its session because it is the only writer of that session. No row has two writers, which keeps the WAL single-writer constraint comfortable.

### Transaction boundary

The instantiation function writes the `agent_instances` row and creates/forks the `sessions` row inside a single SQLite transaction (`BEGIN IMMEDIATE … COMMIT`). Both rows commit together or neither does. After commit:

- **Success**: spawn the child process. If spawn or handshake fails, the instance is closed as `failed` (see Compensation below).
- **Instance-ID collision**: the transaction rolls back and retries with a new ID (up to 3 times).
- **Any other error**: the transaction rolls back and the caller receives a structured error. No partial state survives.

### ID collision retry

Instance IDs are 6 characters of lowercase base32 from `crypto/rand`. The full address (`spec_name#id`) is the `agent_instances` primary key.

- INSERT is attempted inside the same transaction as the session create/fork.
- On `UNIQUE` constraint violation, the transaction is rolled back and retried with a fresh random ID.
- Retry up to 3 times total (3 fresh IDs). After 3 failures, return a structured error to the caller.
- Collision probability across 3 retries with ~30 bits of entropy is negligible at any realistic instance count (birthday bound: ~2^15 instances before 50% collision probability; retry makes even that harmless).

### Compensation on post-commit failure

If the transaction commits successfully but the child process fails to start (exec error, pipe failure, handshake timeout):

1. The parent writes `ended_at = NOW()`, `exit_status = 'failed'`, and a structured `failure_reason` (e.g. `"spawn: exec: permission denied"`).
2. No row is deleted — the instance exists as a real, failed entity for audit.
3. The parent emits a `ChatToolExecutionCompletedEvent` with `status: failed` and the failure reason.
4. The child session row (created atomically with the instance) is an empty shell. At coordinator load time, an empty session is treated as a fresh session — no messages, no history.

## Store API additions

- `SaveAgentInstance`, `CloseAgentInstance(id, exitStatus, usage, failureReason)`
- `GetAgentInstance(id)`, `ListAgentInstances(parentInstanceID)` — instances spawned by a specific parent instance
- `ListChildren(parentSessionID)` — sessions forked from or spawned under a parent session
- Session save/load/list extended to carry `agent_instance_id`

### API naming disambiguation

| Method | Parameter | Returns |
|---|---|---|
| `ListAgentInstances(parentInstanceID)` | Parent instance address (e.g. `"research#k3v9qp"`) | All instances directly spawned by that instance |
| `ListChildren(parentSessionID)` | Parent session UUID | All sessions whose `parent_session_id` matches (both forked and spawned) |
| `GetAgentInstance(id)` | Instance address | One row by primary key |
| `GetSession(id)` | Session UUID | One session row |

`ListAgentInstances` and `ListChildren` operate on different key spaces (instance addresses vs session UUIDs) and return different types. The parameter name makes the distinction explicit at the call site.

## Schema constraints and indexes

### CHECK constraints

```sql
-- Valid depth range (0 = root, capped by config depth_ceiling, typically 4)
ALTER TABLE agent_instances ADD CONSTRAINT chk_depth_range
  CHECK (depth >= 0 AND depth <= 10);

-- exit_status must be a known value or NULL (not yet ended)
ALTER TABLE agent_instances ADD CONSTRAINT chk_exit_status
  CHECK (exit_status IS NULL OR exit_status IN ('completed','failed','cancelled','budget_exhausted','timed_out'));

-- started_at must be non-NULL and before ended_at (when ended)
ALTER TABLE agent_instances ADD CONSTRAINT chk_timestamps
  CHECK (started_at IS NOT NULL AND (ended_at IS NULL OR ended_at >= started_at));

-- usage_json carries a version marker
ALTER TABLE agent_instances ADD CONSTRAINT chk_usage_version
  CHECK (usage_json IS NULL OR json_extract(usage_json, '$.version') IS NOT NULL);
```

### Indexes

```sql
-- Already defined in the DDL: parent_instance_id index for tree traversal
-- Additional indexes for common query patterns:

CREATE INDEX idx_agent_instances_ended ON agent_instances(ended_at)
  WHERE ended_at IS NULL;  -- partial index for orphan sweep

CREATE INDEX idx_agent_instances_spec ON agent_instances(spec_name, started_at);
  -- for "show me all research instances" queries
```

### usage_json versioning

The `usage_json` column uses a versioned shape:

```json
{
  "version": 1,
  "turns": 7,
  "input_tokens": 82000,
  "output_tokens": 4100,
  "cached_tokens": 15000,
  "reasoning_tokens": 0,
  "cache_creation_tokens": 0,
  "cost": "0.02310000"
}
```

- `version` is required and must be present when `usage_json` is non-NULL (enforced by CHECK constraint).
- Future versions may add fields; consumers must tolerate unknown fields (forward compatibility).
- Consumers must reject `usage_json` with an unsupported `version` higher than what they know about, treating the usage as unknown rather than silently misinterpreting fields.

## Active session ownership

A session must have at most one active owner at any time. The ownership model:

- **At spawn**: the instance row and session row are written in one transaction. The session is immediately "owned" by the new instance because no other process has the instance ID.
- **At resume**: the resuming process must atomically check that (a) the session's `agent_instance_id` points to an instance with `ended_at IS NOT NULL` (the prior owner has finished) and (b) no other process is concurrently resuming. This is enforced by:
  ```sql
  -- Pseudocode: the resume flow
  BEGIN IMMEDIATE;
  SELECT agent_instance_id FROM sessions WHERE id = ?;
  SELECT ended_at FROM agent_instances WHERE id = <agent_instance_id>;
  -- If ended_at IS NULL: the prior owner is still active. ROLLBACK, fail with "session active".
  -- If ended_at IS NOT NULL: INSERT new instance row, UPDATE session.agent_instance_id.
  COMMIT;
  ```
- **Concurrent resume**: two processes racing to resume the same session will serialise on the SQLite write lock. The first wins; the second sees the updated `agent_instance_id` and `ended_at IS NULL`, fails with "session already resumed".

No `LOCK`/`UNLOCK` or `ACTIVE` column is needed — the existing `ended_at` column on the prior instance is the ownership token. A session is owned while its `agent_instance_id` points to an instance with `ended_at IS NULL`.

## Busy-timeout retry behavior

SQLite's `busy_timeout` is set to 5000ms at open. If a write encounters a locked database:

1. SQLite retries internally for up to 5 seconds.
2. If the timeout expires, the `sqlite3` driver returns `SQLITE_BUSY`.
3. The store layer retries the entire operation (not just the statement) up to 2 more times with exponential backoff (100ms, 500ms), for a total of 3 attempts.
4. After 3 failed attempts, the store returns a structured error (`ErrStoreBusy`) to the caller.
5. The caller (coordinator or spawn executor) treats `ErrStoreBusy` as a transient infrastructure error: log at ERROR, retry at the application level after 1s, or fail the spawn/operation if retries are exhausted.

The per-operation retry in the store layer is separate from the spawn-level ID collision retry (which retries the entire spawn transaction). Both use 3-attempt bounded retry with explicit failure.

## Deletion and retention

### Instance deletion

- Deleting an instance row cascades via `ON DELETE SET NULL`: child instances' `parent_instance_id` becomes NULL, sessions' `agent_instance_id` becomes NULL.
- Deleting an instance does NOT delete its session. The session remains accessible by its own UUID; it simply no longer links to an agent instance.

### Session deletion

- Deleting a session does NOT affect the instance row. The instance row remains for audit with its `ended_at`/`exit_status`/`usage_json` intact.
- Deleting a session with child sessions (`parent_session_id` references) leaves children intact (no `ON DELETE CASCADE` on sessions — the tree can have gaps).

### Retention policy

- **Default**: no automatic deletion. All instance and session rows persist indefinitely.
- **Config option** (`agents.retention`): maximum age for ended instances and closed sessions (e.g. `"720h"` = 30 days). Rows older than this are eligible for deletion at the next orphan sweep.
- **Manual**: `tau sessions clean --older-than 720h` deletes eligible rows. Requires explicit user intent; never automatic.
- **Active sessions**: never eligible for deletion regardless of age.

## Contention posture

WAL supports many readers plus one writer. Writers here are: each process saving its own session, plus the parent touching instance rows at spawn/exit. Session saves are turn-cadence, instance writes are per-spawn; with a handful of concurrent agents this is far below contention territory. Children should not persist per-delta; per-turn persistence (today's behaviour) is the ceiling. Revisit only if trees grow beyond tens of concurrent agents, which v1's depth caps prevent anyway.

## Orphan sweep

At root startup (and available as a maintenance command later): find instance rows with `ended_at IS NULL`, check the pid, close dead ones as `failed` with a note. Lazy, daemon-free, and safe because pids are only advisory (a recycled pid at worst delays the sweep; the row's pipes are long gone so nothing else references it).