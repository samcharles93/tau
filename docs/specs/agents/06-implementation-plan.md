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

## Phase 5: Hardening and documentation

| ID | Item | Notes |
|----|------|-------|
| P5.1 | End-to-end integration tests | Fake provider: spawn round-trip, fan-out, attenuation (incl. spawn-tool removal cutting the subtree), depth cap, budget exhaustion, cancellation, parent-death orphaning, fork and resume, crash synthesis |
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