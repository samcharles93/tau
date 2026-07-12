# Implementation Plan

Phased work breakdown for the agent-process architecture. Each item maps one-to-one to a Linear story under the CatlowTech team (project: Agent Processes v1); this page is the durable mirror so the plan survives independently of any tracker. Phases are dependency-ordered; items within a phase are mostly parallelisable. `task check` gates every item.

## Phase 0: Investigations

| ID | Item | Notes |
|----|------|-------|
| P0.1 | Per-run tool filtering design | The registry is global (`registry.Replace`/`Unregister` affect all sessions). Design the filtered-view mechanism a coordinator run applies (instance effective set ∩ mode restriction), including plugin registration changes mid-flight. Output: short ADR in this directory |
| P0.2 | Coordinator scoping audit | Confirm the coordinator turn loop can run headless against an assigned session with injected model/tools/limits without TUI assumptions; identify seams (the one-shot path in `internal/cli/root.go` is the starting point). Output: notes + list of refactors feeding P2.1 |
| P0.3 | Root prompt relocation audit | What of tau's current base system prompt moves into `tau.agent.md`'s body, what stays code-assembled (env block, skills, memory). Output: split proposal |
| P0.4 | task.agent.md rationalisation | Decide fate of `task` built-in vs spawnable `tau` (thin restriction, retire, or keep) |

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