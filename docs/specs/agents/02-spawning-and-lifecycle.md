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
3. Compute the effective toolset (for children: attenuation, below; for root: the spec's `tools` or the full registry).
4. Mint the instance id, write the `agent_instances` row (snapshot, resolved model, effective tools, depth, parent instance, pid, started_at).
5. Create or fork the session, with `agent_instance_id` and (for children) `parent_session_id` set.

Who runs it:

- **Root**: the interactive process at startup, before the first session exists. Depth 0, no parent.
- **Children**: the *parent* resolves, attenuates and writes the row, then spawns the child process with the instance id. The child loads its row and session from the shared store. This keeps resolution and permission decisions in the already-trusted process; the child never computes its own capabilities.
- **Modes**: never. A mode runs under the current process's identity with the mode spec's `tools` applied as a further temporary restriction on the process's effective set (intersection again, so a mode can also never widen).

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
- The effective set is computed once at spawn, stored in the instance row, and enforced by the child's coordinator via a per-instance filtered view of the registry (the registry itself stays global; the filter is per coordinator run). Registered-tool changes mid-flight (plugins) re-apply the filter.
- Widening is impossible by construction. An agent that needs more capability returns to its parent; the root returns to the human.

## Budgets and limits

Two layers, both enforced by the child's own coordinator:

- **Structural (from spec)**: `max-turns` per assigned task, `timeout` default. Defaults from config when unset.
- **Task budget (from spawn call)**: `max_tokens`, `deadline`. The parent may also cancel at any time based on streamed usage.

The child emits `agent.usage` after every turn (see 03). On breach the child finishes the current tool call if it is cheap to do so, persists the session, and returns `budget_exhausted` or `timed_out` with partial output. Budgets are data, not exceptions.

Cost accumulation: the parent adds each child's usage totals to the spawning tool-call record and to its own session totals, so the root session always shows tree-wide cost. Instance rows keep per-instance usage for the audit trail.

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