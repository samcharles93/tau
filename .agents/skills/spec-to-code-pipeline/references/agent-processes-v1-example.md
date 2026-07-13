# Worked Example: Agent Processes v1 Specification

An illustration of all 6 phases applied to a real project. Source: a gap-review document covering 7 spec files across storage, wire protocol, lifecycle, and UI domains. 18 gaps → 18 tickets → completed in 15 commits.

## Phase 1: Spec Review

Read 11 documents: overview, spec format, lifecycle, wire protocol, storage, UI, implementation plan, plus 3 design-decision records (ADRs) and the gap-review document itself.

## Phase 2: Gap Analysis

Output: a gap-review document with 18 gaps (G1–G18) organized into three tiers:
- **Critical (4):** correctness/integrity/security hazards
- **High-priority operational (5):** completeness gaps that would break real usage
- **Consistency/completeness (9):** hardening items

## Phase 3: Story Creation

18 tickets in a single project, conventional-commit titled, each with Gap/Scope/Acceptance sections. Examples of scope prefixes: `fix(storage):`, `feat(wire):`, `docs(spec):`, `test(integration):`.

## Phase 4: Dependency Analysis

### Layered architecture

```
Layer 0: Design Foundation  → 2 tickets (spec contracts)
Layer 1: Storage            → 5 tickets (persistence schema, atomic creation, versioning, identity, constraints)
Layer 2: Wire/Protocol      → 3 tickets (authority binding, framing, delivery guarantees)
Layer 3: Process/Runtime    → 3 tickets (resume auth, concurrency, cancellation)
Layer 4: Security/Trust     → 2 tickets (context framing, root override)
Layer 5: UI                 → 1 ticket (recovery, accessibility, fan-out bounds)
Layer 6: Cross-cutting      → 2 tickets (test gates, observability)
```

### Critical path (12 tickets)

```
T-design-1 → T-storage-1 → T-storage-4 → T-storage-3 → T-process-1 → T-process-3 → T-ui-1
                                                   ↘ T-wire-1 → T-wire-2 → T-wire-3 ↗
```

### Biggest bottleneck

The resume-authorization ticket (T-process-1) had 5 upstream dependencies across design, storage, and wire layers. All had to complete before its contract could be specified.

## Phase 5: Execution Process Design

| Phase | Pattern | Rationale |
|-------|---------|-----------|
| P0: Design | 2 ∥ 2 | Foundation. Resolve the contracts first. |
| P1: Storage | A ∥ B → C → D ∥ E | Build persistence. Two independent starts, then sequential, then parallel again. |
| P2: Wire | A → B → C | Protocol follows storage. Strictly sequential. |
| P3: Process | A → B ∥ C | Most-dependent ticket first, then two that can overlap. |
| P4: Security | A ∥ B | Both independent of each other. |
| P5: UI | A | Heavy downstream — depends on 4 prior tickets. |
| Cross-cut | A (alongside all), B (after P2) | Test gates run parallel to implementation; observability needs state machine + budget model. |

## Phase 6: Implementation Coordination

All 18 tickets completed as spec updates across 7 documents. Key principles that held up:

1. Each gap traced to a specific source line
2. Each edge had a one-sentence rationale
3. Cross-cutting tickets ran alongside, not after
4. Contract changes propagated to downstream tickets before they started

## Key decisions made

| Domain | Decision |
|--------|----------|
| Tool attenuation | A specific tool is an ordinary attenuated capability — no auto-injection outside the declared intersection |
| Resume authorization | Requires ancestor relationship; capabilities recomputed from snapshot ceiling, not trusted from persisted state |
| Atomic creation | Instance row + session create/fork in one transaction; ID collision retry up to 3 times |
| Budget semantics | Canonical usage model with input/output/cached/reasoning tokens; timeout (relative) vs deadline (absolute) distinction |
| Authority bindings | Pipe-to-instance binding validates all identity fields before dispatch; 3 violations close the pipe |
| Snapshot versioning | Historical snapshot for resume identity; canonical serialization for stable hashes |
| Context trust | Delegated context framed with trust markers and XML-escaping; no implicit inheritance |
| Root override | Trust-on-first-use with content-hash binding; trust store outside the repository |
