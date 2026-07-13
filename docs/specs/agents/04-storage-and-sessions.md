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

- `SaveAgentInstance`, `CloseAgentInstance(id, exitStatus, usage)`
- `GetAgentInstance(id)`, `ListAgentInstances(parentID)` (tree traversal)
- `ListChildren(parentSessionID)` on sessions (the missing tree method noted when the forking infrastructure was reviewed)
- Session save/load/list extended to carry `agent_instance_id`

## Contention posture

WAL supports many readers plus one writer. Writers here are: each process saving its own session, plus the parent touching instance rows at spawn/exit. Session saves are turn-cadence, instance writes are per-spawn; with a handful of concurrent agents this is far below contention territory. Children should not persist per-delta; per-turn persistence (today's behaviour) is the ceiling. Revisit only if trees grow beyond tens of concurrent agents, which v1's depth caps prevent anyway.

## Orphan sweep

At root startup (and available as a maintenance command later): find instance rows with `ended_at IS NULL`, check the pid, close dead ones as `failed` with a note. Lazy, daemon-free, and safe because pids are only advisory (a recycled pid at worst delays the sweep; the row's pipes are long gone so nothing else references it).