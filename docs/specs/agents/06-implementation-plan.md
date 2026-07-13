# Implementation Plan

Phased work breakdown for the agent-process architecture. Each item maps one-to-one to a Linear story under the CatlowTech team (project: Agent Processes v1); this page is the durable mirror so the plan survives independently of any tracker. Phases are dependency-ordered; items within a phase are mostly parallelisable.

**Status legend:** 🟢 decided+implemented  |  🔵 decided, implementation pending  |  ⚪ proposed, not yet decided  |  ⬜ superseded/retired

**Last updated:** 2026-07-13 — reconciled P0 status with gap review (CAT-105).

## Gap review and backlog

The 2026-07-13 gap review (`review-gaps-2026-07-13.md`) identified 18 gaps (G1–G18) against the Phase 0–5 plan. Each gap has a corresponding Linear ticket under the `tau: Agent Processes v1` project:

- **Critical (G1–G4):** CAT-89 (skill attenuation), CAT-90 (resume auth), CAT-92 (atomic creation), CAT-94 (authority binding)
- **High-priority operational (G5–G9):** CAT-91 (concurrency ceilings), CAT-93 (budget semantics), CAT-95 (process-tree cancel), CAT-98 (JSONL state machine), CAT-99 (delivery guarantees)
- **Consistency/hardening (G10–G18):** CAT-96 (identity/orphan), CAT-97 (snapshot versioning), CAT-100 (delegated context trust), CAT-101 (storage semantics), CAT-102 (root-spec trust), CAT-103 (UI recovery/a11y), CAT-104 (per-story test gates), CAT-105 (P0 status — this ticket), CAT-106 (observability)

See `review-gaps-2026-07-13.md` for gap details and `.agents/skills/spec-to-code-pipeline/SKILL.md` for the dependency analysis methodology used to sequence them.

## Phase 0: Investigations

| ID | Item | Design | Implementation | Notes |
|----|------|--------|---------------|-------|
| P0.1 | Per-run tool filtering design | 🔵 decided (ADR 2026-07-12) | 🟢 implemented (`effectiveTools` + `allowedTools` two-tier filter, CAT-89 resolved) | The registry is global; filtered-view mechanism per coordinator run (instance effective set ∩ mode restriction), including plugin registration changes mid-flight. ADR: `p0.1-per-run-tool-filtering-adr.md`. **Updated 2026-07-13:** `skill` is now an ordinary attenuated tool (not auto-injected). |
| P0.2 | Coordinator scoping audit | 🔵 decided (ADR 2026-07-12) | 🔵 pending (P2.1) | Confirmed one-shot mode is the right base for headless child runs; coordinator requires zero changes. ADR: `p0.2-coordinator-scoping-audit.md`. Blocks CAT-53 (P2.1). |
| P0.3 | Root prompt relocation audit | 🔵 decided (ADR 2026-07-12) | 🔵 pending (P1.7) | Static character extracted to `tau.agent.md`; dynamic scaffolding stays in template; two-pass rendering. ADR: `p0.3-prompt-relocation-proposal.md`. Blocks CAT-52 (P1.7). |
| P0.4 | task.agent.md rationalisation | 🟢 decided + implemented | 🟢 done (`task.agent.md` excluded from `Builtins()` list; file kept for reference) | tau is spawnable as a general-purpose child worker, filling task's original role. Format spec (`01-agent-spec-format.md`) correctly recorded this as retired; this plan now matches. |

## Phase 1: Foundations

| ID | Item | Notes |
|----|------|-------|
| P1.1 | Config: `model_modes` + `agents` block | Tier map, `default_max_depth`, `depth_ceiling`, `default_max_turns`, `default_timeout`; validation; docs in configuration.md. Tier-or-concrete resolution helper with precedence chain (01) |
| P1.2 | Spec fields: `provider`, `max-turns`, `timeout` | Frontmatter parsing + validation + tests; field reference doc update |
| P1.3 | Enforce `disable-model-invocation` | As spawn-target rejection (02 targeting rules) |
| P1.4 | Migration: `agent_instances` + `sessions.agent_instance_id` | DDL per 04; migration tests both directions |
| P1.5 | Store API | Save/Close/Get/List instance methods, `ListChildren(parentSessionID)`, session round-trip of `agent_instance_id` |
| P1.6 | Instantiation function | Shared resolve → model-resolve → attenuate → mint id → write row → create/fork session path (02); unit tests incl. bare-name `tau` full-discovery special case |
| P1.7 | `tau.agent.md` built-in + root identity at startup | Embed spec, root startup instantiates it, `--agent` flag swaps; orphan sweep hook at startup (04) |

## Phase 2: Protocol and child entry

| ID | Item | Notes |
|----|------|-------|
| P2.1 | Headless child entry point | Hidden CLI mode: read `agent.assign` on stdin, load instance + session, run coordinator, stream envelopes on stdout, exit after `agent.result`. Reuses one-shot seams from P0.2 |
| P2.2 | Envelope extension + message types | `from`/`to` fields; `agent.ready/assign/event/usage/cancel/result` in the bridge registry; specgen/AsyncAPI regeneration |
| P2.3 | stdio JSONL framing | Reader/writer with 8 MiB cap, EOF semantics, protocol-version handshake check |
| P2.4 | Budget/limit enforcement in coordinator | max-turns, max_tokens, deadline, timeout; graceful abort with partial output; `budget_exhausted`/`timed_out` statuses |
| P2.5 | Cancellation and orphan behaviour | `agent.cancel` handling, grace period, stdin-EOF persist-and-exit, always-persist-before-exit guarantee (02 lifecycle table) |
| P2.6 | Per-run tool filtering implementation | Build P0.1's design; enforce effective set in child runs and mode intersection in-session |

## Phase 3: The agent tool

| ID | Item | Notes |
|----|------|-------|
| P3.1 | `agent` tool + executor | Schema per 02; targeting checks; attenuation; depth stamping; spawn via instantiation function; process management (lift exec.Cmd patterns from spawn project); synchronous result |
| P3.2 | Fresh/fork/resume context seeding | Fresh: spec body + task + `<parent_context>`; fork: `CloneChatSessionState`; resume: existing child session |
| P3.3 | Event forwarding + usage accumulation | Re-publish `agent.event` scoped to tool call; accumulate `agent.usage` into tool record and parent session totals; close instance rows on exit |
| P3.4 | Parallel fan-out verification | N agent calls in one turn run concurrently through the existing WaitGroup path; interleaved event streams stay correctly scoped |
| P3.5 | Completion contract | Tool result assembly: final text primary, structured envelope attached, abnormal-end status line (02) |

## Phase 4: UI

| ID | Item | Notes |
|----|------|-------|
| P4.1 | TUI child state block | Live element per 05: activity verb, shimmer, turns, tokens (no unit suffix), elapsed; terminal collapse; spec colour |
| P4.2 | TUI drill-down | Expand to child transcript from forwarded events; open child session proper |
| P4.3 | WebUI parity | Same data via bridge agent-scoped events; SPA components; no drift |
| P4.4 | Session tree | Lineage in session lists (both UIs), agent attribution per row, resume from list |

## Acceptance gates (per-story test requirements)

Every implementation story carries its own executable acceptance criteria. `task check` gates compilation and unit tests; the tests below gate correctness. P5.1 is reserved for composed end-to-end validation (multi-phase scenarios).

### Phase 1 test gates

| Story | Test category | Specific scenarios |
|---|---|---|
| P1.1 (config) | Validation | Invalid tier names, missing provider, negative depth/timeout values, empty `model_modes` map |
| P1.2 (spec fields) | Parsing | All new fields round-trip through Parse → serialize → Parse; unknown fields tolerated; empty `provider` with concrete `model`; `timeout` parsing of `"5m"`/`"30s"`/`"1h"` |
| P1.3 (disable-model-invocation) | Target rejection | Spawn targeting a `disable-model-invocation: true` spec returns failed result; CLI invocation of such a spec still works |
| P1.4 (DDL migration) | Schema | Migration up creates tables/indexes; migration down drops them cleanly; migration is idempotent (run twice) |
| P1.5 (store API) | CRUD | Save/Get/List/Close round-trip; `ListChildren` returns correct lineage; session carries `agent_instance_id` through save/load |
| P1.6 (instantiation) | Integration | Root startup creates instance + session atomically; bare-name `tau` resolves through full discovery; post-commit spawn failure closes instance as `failed`; ID collision retries up to 3 times |
| P1.7 (tau.agent.md) | Prompt equivalence | Assembled prompt identical before/after relocation; user/project `tau.agent.md` overrides built-in; snapshot test covers all sections |

### Phase 2 test gates

| Story | Test category | Specific scenarios |
|---|---|---|
| P2.1 (headless child) | End-to-end | Child reads `agent.assign`, runs one turn, writes `agent.result`, exits 0; child with invalid `instance_id` exits 1; child with missing session exits 1 |
| P2.2 (envelope types) | Serialization | All 6 agent envelope types round-trip through JSON; `from`/`to` fields survive bridge re-serialization; AsyncAPI regenerates without errors |
| P2.3 (JSONL framing) | Conformance | Valid exchange completes; oversized line (9 MiB) discarded; invalid UTF-8 discarded; malformed JSON discarded; duplicate result discarded; post-result message discarded; empty line skipped; concurrent writes never interleave bytes |
| P2.4 (budget enforcement) | Boundary | Token budget exhausted at exact limit; timeout expires mid-call; deadline in the past rejected at spawn; negative max_tokens rejected; missing provider usage treated as unknown; partial output returned on breach |
| P2.5 (cancellation) | Lifecycle | Cancel during LLM call → child aborts; cancel during tool execution → child finishes tool then exits; stdin EOF → child persists and exits; child always-persists-before-exit |
| P2.6 (tool filtering) | Correctness | `effectiveTools` immutable after construction; `SetAllowedTools` narrows below ceiling; empty list reverts to ceiling; plugin tool not in ceiling is invisible; `skill` not auto-injected; ceiling intersection works at construction |

### Phase 3 test gates

| Story | Test category | Specific scenarios |
|---|---|---|
| P3.1 (agent tool) | Integration | Valid spawn completes; depth cap rejects; `disable-model-invocation` target rejected; missing spec returns failed result; spawn within concurrency limits; spawn rejected when limits exceeded; ID collision retry works |
| P3.2 (context seeding) | Correctness | Fresh child receives `<parent_context>` with trust markers; fork child receives cloned history with provenance tags; resume child loads historical snapshot; `inherit` block defaults all to false |
| P3.3 (event forwarding) | Integration | Child events arrive at parent scoped to tool call; usage accumulates into parent session totals; no double counting across tree levels; instance row closed on exit |
| P3.4 (fan-out) | Saturation | 4 concurrent children complete independently; interleaved events stay correctly scoped; concurrency limits enforced (active/queue/total); queue time counts against timeout |
| P3.5 (completion contract) | Correctness | `completed` status with full text and usage; `failed` status with exit detail; `cancelled` status with partial output; `budget_exhausted` status with partial output; `timed_out` status with partial output |

### Phase 4 test gates

| Story | Test category | Specific scenarios |
|---|---|---|
| P4.1 (state block) | Rendering | State block shows agent name, activity verb, turns, tokens, elapsed; terminal state collapses to summary; abnormal end color-coded with symbol+label |
| P4.2 (drill-down) | Interaction | Expand shows child transcript; collapse returns to state block; open child session from drill-down; Esc collapses → Esc cancels parent |
| P4.3 (WebUI parity) | Cross-surface | Same child state block data on TUI and WebUI; same drill-down transcript; same cancellation flow |
| P4.4 (session tree) | Display | Child sessions indented under parent; agent identity shown per session; resume available from session list |

### Cross-cutting test gates

| Category | Specific scenarios |
|---|---|
| **Authority/spoofing** | Envelope with spoofed `from` rejected; spoofed `instance_id` rejected; spoofed `session_id` rejected; nested event with non-forwardable type dropped; 3 violations close pipe |
| **Protocol conformance** | All 16 JSONL conformance tests (03); all invalid state transitions produce deterministic results; protocol error kinds are correctly classified |
| **Fault injection** | SQLite `SQLITE_BUSY` after busy_timeout → store retries and eventually returns `ErrStoreBusy`; spawn fails after instance row committed → instance closed as `failed`; process-start fails → compensation writes `failure_reason`; child crashes before first session write → empty session treated as fresh |
| **Concurrent writers** | Two concurrent spawn transactions serialize correctly; two concurrent resume attempts yield exactly one owner; concurrent `SetAllowedTools` calls do not race |
| **Saturation** | 16 concurrent children → memory bounded (max 256 events/child); 20 concurrent spawns → 17th rejected; queue timeout removes stale spawns |
| **Registry mutation** | Plugin tool registered mid-flight in child → filtered by ceiling; plugin tool unregistered → removed from LLM-visible set next turn; ceiling never widens after construction |
| **Unknown usage** | Provider returns no usage → treated as unknown, not zero; provider returns partial usage (output only) → missing fields treated as zero; cost not accumulated for unknown usage |
| **Restart recovery** | TUI restart reconstructs child state from store; WebUI page refresh replays snapshot + re-subscribes; parent restart detects orphaned children via sweep |
| **Redaction** | Prompt text absent from all log levels; API key in stderr redacted; Bearer token in stderr redacted; absolute paths in tool args redacted to basename |

### P5.1 scope (composed end-to-end only)

P5.1 validates multi-phase scenarios that require the full stack. It does NOT duplicate the per-story gates above:

- Full lifecycle: spawn → assign → multi-turn work → result → parent accumulates usage
- Multi-level tree: root spawns A → A spawns B → B spawns C → cancel root → all 4 processes terminated
- Resume after parent crash: root spawns A → A crashes → root restarts → orphan sweep closes A → root resumes A's session
- Budget cascade: root has 100k token budget → spawns 3 children with 30k each → root's subtree accounting reflects all 3
- Provider failure injection: provider returns 500 → child retries → eventual success; provider returns malformed response → child handles gracefully

## Phase 5: Hardening and documentation

| ID | Item | Notes |
|----|------|-------|
| P5.1 | End-to-end integration tests | Composed scenarios only (see P5.1 scope above). Per-story gates cover individual correctness; P5.1 covers multi-phase workflows. |
| P5.2 | Version-skew handling | Protocol handshake mismatch path + test |
| P5.3 | Docs pass | Update docs/agent.md, architecture.md, sessions.md, configuration.md, tools.md; regenerate AsyncAPI; cross-link this spec |
| P5.4 | Dogfood + tune built-ins | Run real delegation, set tiers on research/task, adjust default caps from experience |

## Deferred backlog (explicitly out of v1)

| ID | Item |
|----|------|
| D1 | Unix socket per instance + attach to running child |
| D2 | Background spawns (non-blocking flag, completion notification) |
| D3 | Resident children (keep-alive, idle reaping) |
| D4 | Lateral child-to-child messaging |
| D5 | Cross-machine transport: TCP/WebSocket + mDNS discovery (p2pchat/nell-engine patterns) |
| D6 | Per-spec spawn allowlists (`agents:` field) |
| D7 | Interactive approval escalation for toolset widening |
| D8 | Per-child cancel affordance in UI (if not landed with P4.1) |
| D9 | Model modes managed from the TUI |

## Risks and watch-items

- **Drift between mode and process paths**: both must route through the same coordinator turn machinery; P0.2/P2.6 are the guards.
- **SQLite contention**: fine at v1 scale (04); revisit if trees grow.
- **Prompt relocation regressions** (P0.3): moving tau's base prompt into a spec body must not change interactive behaviour; snapshot-test the assembled prompt before/after.
- **Version skew**: handled by handshake, but the failure UX should be clear, not mysterious.