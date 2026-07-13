# Spawning and Lifecycle

This page covers instance identity, the instantiation path shared by root and children, the `agent` tool, capability attenuation, depth and budget enforcement, the completion contract, and every failure mode.

## Instance identity

An agent instance is addressed as `<spec-name>#<id>`, e.g. `research#k3v9qp`.

- `id` is 6 characters of lowercase base32 from `crypto/rand`, minted by whoever creates the instance row.
- The address is the `from`/`to` value in every wire envelope, the primary key of `agent_instances`, and the label the UI shows.
- Two concurrent instances of the same spec are always distinguishable; nothing enforces singletons.

## Instantiation

One function, used by every path that brings an agent identity into existence:

1. Resolve the spec (built-in, `user:`, `project:`; for the bare name `tau` the root startup path resolves through full discovery so project/user overrides win, see 01).
2. Resolve the model (precedence in 01) to a concrete provider/model pair.
3. Compute the effective toolset (for children: attenuation; for root: the spec's `tools` or the full registry).
4. Mint the instance id with bounded uniqueness retry (see below), write the `agent_instances` row (snapshot, resolved model, effective tools, depth, parent instance, pid, started_at) AND create/fork the session — **in one SQLite transaction**.
5. After commit: spawn the child process. If spawn or handshake fails, close the instance deterministically as `failed` with a structured reason (see compensation, below).

Who runs it:

- **Root**: the interactive process at startup, before the first session exists. Depth 0, no parent.
- **Children**: the *parent* resolves, attenuates and writes the row, then spawns the child process with the instance id. The child loads its row and session from the shared store. This keeps resolution and permission decisions in the already-trusted process; the child never computes its own capabilities.
- **Modes**: never. A mode runs under the current process's identity with the mode spec's `tools` applied as a further temporary restriction on the process's effective set (intersection again, so a mode can also never widen).

### ID collision retry

Instance IDs are 6 characters of lowercase base32 from `crypto/rand`. The full address (`spec-name#id`) is the primary key of `agent_instances`. Collision retry:

- After minting the ID, attempt the INSERT within the same transaction as the session create/fork.
- On `UNIQUE` constraint failure, retry with a new random ID up to 3 times.
- After 3 retries, fail with a structured error. The caller receives a failed tool result (spawn failure) or the root startup exits.
- The 6-char ID is the display suffix; the full address (`spec#id`) is the protocol identity. Collision probability across 3 retries with ~30 bits of entropy is negligible at realistic instance counts.

### Compensation on post-commit failure

If the transaction commits successfully but process spawn, pipe creation, or handshake fails:

1. The instance row exists and is visible to queries (intentional — the instance is a real, failed entity).
2. The parent writes `ended_at = now()`, `exit_status = 'failed'`, and a structured `failure_reason` (e.g. `"spawn: exec failed: permission denied"`).
3. The parent emits a `ChatToolExecutionCompletedEvent` with `status: failed` and the reason, so the UI shows the failure.
4. No compensation write touches the child session (it was never started, so there is nothing to save).

### Compensation on child crash before session creation

If the child process starts but exits before creating its first session write (crashes in coordinator init):

1. The parent detects EOF on stdout without a `ready` envelope.
2. The parent closes the instance as `failed` with exit detail.
3. The session row is an empty shell (created atomically with the instance) — it is treated as a null session at load time.

## The `agent` tool

Spawning is a normal registry tool named `agent`, subject to `tools` filtering and attenuation like any other tool. Schema:

```json
{
  "agent":        "research",           // required; spec name, user:/project: prefixes allowed
  "prompt":       "...",                // required; the task
  "context":      "...",               // optional; parent-selected context, becomes a <parent_context> block
  "context_mode": "fresh" | "fork",     // optional; default "fresh"
  "resume":       "<child session id>", // optional; follow-up to a finished child; excludes context/context_mode
  "model":        "fast",               // optional; tier or concrete; precedence spawn > spec > inherit
  "tools":        ["read", "grep"],     // optional; narrows the child below the attenuated set, never widens
  "budget": {                            // optional; all fields optional
    "max_tokens": 200000,
    "deadline":   "5m"
  }
}
```

Execution is synchronous: the tool call blocks until the child completes. Fan-out is free because the coordinator already executes a turn's tool calls concurrently (`coordinator_turn.go`, WaitGroup per call): N `agent` calls in one assistant turn run N children in parallel, each result returning as its child finishes.

Targeting rules, enforced by the executor before anything is spawned:

- Spec must resolve, else the tool returns a failed result naming the miss.
- `disable-model-invocation: true` specs are rejected as targets.
- Depth: the child's depth is parent depth + 1 and must not exceed the effective cap (spec `max-turns` is unrelated; see budgets). The cap is config `agents.default_max_depth` unless the parent's spec lowered it; no spec may exceed `agents.depth_ceiling`.

## Capability attenuation

```
child effective tools = child spec tools  ∩  parent effective tools  ∩  spawn "tools" param (if given)
```

- `nil` spec/param means "no restriction from this contributor", not "nothing".
- The `agent` tool itself participates: a parent whose effective set lacks `agent` cannot spawn at all; a child whose intersection lacks `agent` ends the tree there.
- `skill` is an ordinary attenuated tool. It participates in the intersection like any other tool — it is present only when explicitly declared in the spec's `tools` list or the spawn restriction. Omitting `skill` disables mode switching (the agent cannot enter another mode mid-session; slash commands from the user still work). No tool is injected outside the declared intersection.
- The effective set is computed once at spawn, stored in the instance row, and enforced by the child's coordinator via a per-instance filtered view of the registry (the registry itself stays global; the filter is per coordinator run). Registered-tool changes mid-flight (plugins) re-apply the filter.
- Widening is impossible by construction. An agent that needs more capability returns to its parent; the root returns to the human.

### Tool-list serialisation semantics

| Declared value | Meaning |
|---|---|
| `nil` (field omitted) | No restriction from this contributor; inherit parent effective or full registry |
| `[]` (explicit empty list) | Currently treated as nil (unrestricted); reserved for future "no tools" semantics |
| `["read", "grep"]` | Only these tools, intersected with other contributors |

### Cross-cutting rules

- **Root process**: `effectiveTools = nil` → full registry available. The root has no parent ceiling.
- **Mode entry**: the mode spec's `tools` list is intersected with the process's current `effectiveTools` ceiling, stored as the active filter. Exiting the mode reverts to the ceiling.
- **Plugin registration mid-flight**: new plugin tools are checked against the immutable `effectiveTools` ceiling on the next turn iteration. A plugin tool not in the ceiling is invisible to the LLM.

## Budgets and limits

### Canonical usage model

Every provider maps into one documented usage model. The canonical fields:

| Field | Type | Semantics |
|---|---|---|
| `input_tokens` | uint64 | Prompt tokens consumed this turn, including cached and reasoning tokens. Always ≥ `cached_tokens + reasoning_tokens`. |
| `output_tokens` | uint64 | Completion tokens produced this turn. |
| `cached_tokens` | uint64 | Subset of `input_tokens` served from provider cache (billed at the cache-read rate). Zero when the provider does not report cache breakdowns. |
| `reasoning_tokens` | uint64 | Subset of `input_tokens` consumed by internal reasoning/thinking. Zero when reasoning is not in use or the provider does not report it. |
| `cache_creation_tokens` | uint64 | Tokens written to the provider cache (billed at the cache-write rate). Zero when not applicable. |
| `cost` | decimal string (18,8) | Total cost in USD for this turn. Computed as `(input − cached − cache_creation) × input_rate + output × output_rate + cached × cache_read_rate + cache_creation × cache_write_rate`. Precision: 8 decimal places. |

### Direct vs subtree accounting

| Scope | What it counts |
|---|---|
| **Direct** | The agent's own provider calls for the current assigned task. Reported on `agent.usage` after every turn. |
| **Subtree** | Direct + the sum of all descendant instances' direct usage. Accumulated by the parent as each child completes. |
| **Session total** | Direct + subtree for all turns in the session. Stored in the session row for the UI. |
| **Instance total** | The agent's own direct usage across all turns of its lifetime. Stored in `agent_instances.usage_json`. |

Subtree aggregation happens in the parent, not the child: the child reports only its own direct usage. The parent adds each child's total to the spawning tool-call record and to its own session totals. This means the root session always shows tree-wide cost without double counting (each instance reports only its own direct calls).

### Admission and breach behavior

**Pre-call admission (token budgets):**

1. Before each LLM call, the coordinator checks whether `(session total input_tokens + session total output_tokens) ≥ max_tokens`.
2. If the budget is already exhausted, the call is skipped and the turn ends with `budget_exhausted` status.
3. If the budget allows the call, it proceeds. The coordinator tracks cumulative usage during streaming.

**Post-call breach (token budgets):**

1. After the provider returns and usage is known, the coordinator checks the updated total against `max_tokens`.
2. If the call breached the budget, the turn ends with `budget_exhausted` status and partial output.
3. The agent may finish the current tool call if it is cheap to do so (configurable grace: one tool call ≤ 5s wall time), then persists the session.

**Time-based limits (timeout and deadline):**

- **`timeout`** (Go `time.Duration`, specified as `"5m"`, `"30s"`, etc.): a relative wall-clock limit from the moment the child process starts. Enforced by the coordinator via `context.WithTimeout`. On expiry, the in-flight LLM call is cancelled, the session is persisted, and the agent returns `timed_out`.
- **`deadline`** (RFC 3339 absolute timestamp, e.g. `"2026-07-13T12:00:00Z"`): an absolute point in time after which the agent must stop. Admits if `time.Now() < deadline`; rejects at spawn if the deadline is already past. Enforced via `context.WithDeadline`. The schema distinguishes these by format: duration strings are `timeout`, RFC 3339 strings are `deadline`.

**Validation rules:**

| Field | Valid range | Behavior on invalid |
|---|---|---|
| `max_tokens` | 1 … 2^63−1 | ≤ 0: reject at spawn; no budget enforced |
| `max_turns` | 1 … 2^31−1 | ≤ 0: defer to config default |
| `timeout` | 1s … 24h | < 1s: reject at spawn; > 24h: cap to 24h |
| `deadline` | must be in the future at spawn time | In the past: reject at spawn |
| `cost` | ≥ 0, decimal(18,8) | Negative: treated as 0 (provider bug); unavailable: stored as NULL |

### Conservative handling for missing usage

Providers may delay, omit, or partially report usage:

- **No usage in streaming deltas**: the `agent.usage` envelope carries the cumulative total known so far. If a provider sends no usage deltas, the first cumulative report arrives with the completion chunk. The UI shows "…" until the first usage arrives.
- **No usage at completion**: the `agent.result` reports `input_tokens: 0, output_tokens: 0`. The parent treats this as "usage unknown" — cost is not accumulated, and the UI shows "unknown" rather than inventing a zero. Budget enforcement is skipped for the turn (cannot prove breach without data).
- **Partial usage (e.g. only output tokens)**: missing fields are treated as zero. The parent accumulates what it has. Double counting is impossible because each instance reports only its own direct usage.

### Currency and precision

- All costs are in USD.
- Stored as decimal strings (not floats) with 8 decimal places.
- Rate multiplication uses arbitrary-precision decimal arithmetic (via `shopspring/decimal` or equivalent).
- The UI displays up to 4 decimal places (e.g. `$0.0231`); the store keeps 8 for audit.

## Completion contract

The tool result the parent model sees is the child's final assistant text. Attached to the tool record (for the harness, UI and store, not injected into the prose) is the structured envelope:

```json
{
  "status":       "completed | failed | cancelled | budget_exhausted | timed_out",
  "instance_id":  "research#k3v9qp",
  "session_id":   "<child session id>",
  "usage":        { "turns": 7, "input_tokens": 0, "output_tokens": 0, "cost": 0.0 },
  "error":        "...",   // abnormal ends only
  "partial":      true      // set when final_text is best-effort partial output
}
```

On abnormal end the parent model receives the partial text (if any) plus a single compact status line appended by the harness, e.g. `[agent research#k3v9qp ended: timed_out after 5m, 7 turns; partial output above]`. Failures are data: the parent decides whether to retry, respawn, resume or report.

Follow-ups: `resume` spawns a fresh process on the child's existing session (same spec, new instance row with the same lineage). This is the only continuation mechanism; there are no resident children in v1.

## Lifecycle and failure modes

Child process states: `spawned → ready → working → (result sent) → exited`.

| Event | Detection | Behaviour |
|-------|-----------|-----------|
| Normal completion | `agent.result` then exit 0 | Parent records result, closes pipes, updates instance row (`ended_at`, `exit_status`, usage) |
| Child crash | stdout EOF without `agent.result`, nonzero exit | Parent synthesises `status: failed` with the exit detail; child session retains whatever was persisted last |
| Parent cancels (user Esc, budget cut-off, turn cancelled) | Parent sends `agent.cancel`, then closes stdin after a grace period (default 5s) | Child aborts the in-flight provider call, persists the session, sends `agent.result` with `cancelled` if it can, exits |
| Parent dies | Child sees stdin EOF outside the cancel flow | Child treats it as cancel-with-no-listener: persist, exit. Its session and instance row remain in the store for post-mortem or resume |
| Child hangs | Parent-side deadline (spawn `deadline` or spec `timeout`) fires | Cancel flow, then SIGKILL after grace; `status: timed_out` |
| Depth/target violation | Executor pre-checks | No process is spawned; tool returns `failed` immediately |

Rules of thumb encoded above: the store is always consistent because the child persists before exiting on every path it controls, and the parent never trusts the child's self-reporting for liveness (pipes and exit codes are the ground truth).

Orphaned instance rows (machine crash, SIGKILL) are detected lazily: any row with `ended_at IS NULL` whose pid is dead may be closed as `failed` by the next process that notices (root startup is a natural sweep point). No daemon is introduced for this.