# Worked Example: CAT-89–106 (Agent Processes v1)

Source: `docs/specs/agents/review-gaps-2026-07-13.md` → 18 Linear tickets. Completed 2026-07-13.

## Phase 1: Spec Review

Read 11 documents: `00-overview.md`, `01-agent-spec-format.md`, `02-spawning-and-lifecycle.md`, `03-wire-protocol.md`, `04-storage-and-sessions.md`, `05-ui.md`, `06-implementation-plan.md`, `p0.1-per-run-tool-filtering-adr.md`, `p0.2-coordinator-scoping-audit.md`, `p0.3-prompt-relocation-proposal.md`, `review-gaps-2026-07-13.md`.

## Phase 2: Gap Analysis

Output: `review-gaps-2026-07-13.md` — 18 gaps (G1–G18) organized into three tiers.

## Phase 3: Story Creation

18 Linear tickets, all in project `tau: Agent Processes v1`, conventional-commit titled, with Gap/Scope/Acceptance sections.

## Phase 4: Dependency Analysis

### Layered architecture

```
Layer 0: Design Foundation  → CAT-89, CAT-105
Layer 1: Storage            → CAT-92, CAT-93, CAT-101, CAT-97, CAT-96
Layer 2: Wire/Protocol      → CAT-94, CAT-98, CAT-99
Layer 3: Process/Coordinator→ CAT-90, CAT-91, CAT-95
Layer 4: Security/Trust     → CAT-100, CAT-102
Layer 5: UI                 → CAT-103
Layer 6: Cross-cutting      → CAT-104, CAT-106
```

### Critical path (12 tickets)

```
CAT-89 → CAT-92 → CAT-101 → CAT-97 → CAT-90 → CAT-95 → CAT-103
                          ↘ CAT-94 → CAT-98 → CAT-99 ↗
```

### Biggest bottleneck: CAT-90

5 upstream dependencies: CAT-89, CAT-92, CAT-94, CAT-101, CAT-97. All had to complete before resume authorization could be specified.

## Phase 5: Execution Process Design

| Phase | Tickets | Rationale |
|-------|---------|-----------|
| P0: Design | CAT-89 ∥ CAT-105 | Foundation. 2 tickets, parallel. |
| P1: Storage | CAT-93 ∥ CAT-92 → CAT-101 → CAT-97 ∥ CAT-96 | Build persistence layer. |
| P2: Wire | CAT-94 → CAT-98 → CAT-99 | Protocol follows storage. |
| P3: Process | CAT-90 → CAT-91 ∥ CAT-95 | CAT-90 has most deps. |
| P4: Security | CAT-100 ∥ CAT-102 | Both independent. |
| P5: UI | CAT-103 | Heavy downstream. |
| Cross-cut | CAT-104 (alongside), CAT-106 (after P2) | Test gates + observability. |

## Phase 6: Implementation Coordination

Completed all 18 tickets as spec updates across 7 documents in 15 commits. Key principles that held up:

1. Each gap traced to a specific source line
2. Each edge had a one-sentence rationale
3. Cross-cutting tickets ran alongside, not after
4. Contract changes propagated to downstream tickets before they started

## Key decisions made

| Ticket | Decision |
|--------|----------|
| CAT-89 | `skill` is an ordinary attenuated tool — no auto-injection |
| CAT-90 | Resume requires ancestor relationship; capabilities recomputed from snapshot ceiling |
| CAT-92 | Instance + session in one SQLite transaction; ID collision retry up to 3 times |
| CAT-93 | Canonical usage model: input/output/cached/reasoning tokens, decimal(18,8) cost |
| CAT-94 | PipeBinding struct validates from/to/instance_id/task_id/session_id before dispatch |
| CAT-97 | Historical snapshot for resume identity; canonical serialization for stable hashes |
| CAT-100 | Delegated context framed as `trust="data"` with origin provenance |
| CAT-102 | Trust-on-first-use for project root-spec overrides with content-hash binding |
