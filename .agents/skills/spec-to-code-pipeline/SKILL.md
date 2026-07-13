---
name: spec-to-code-pipeline
description: End-to-end pipeline for turning spec documents into actionable, sequenced implementation tickets — Spec Review → Gap Analysis → Story Creation → Dependency Analysis → Execution Process Design → Implementation Coordination. Use when working through a new spec, landing a multi-ticket project, doing architecture-to-backlog conversion, or when the user asks to "analyze dependencies", "sequence these tickets", "map the critical path", or "turn this spec into stories".
source: project
scope: project
enabled: true
user_invocable: true
priority: 14
---

# Spec → Code Pipeline

A six-phase pipeline for converting spec documents into sequenced, parallelizable, hazard-free implementation tickets. Reference example: the **tau Agent Processes v1** project — 18 tickets (CAT-89–106) derived from one gap-review document.

## Phase 1: Spec Review

Read every document in the spec directory. Don't skim — note every decision, invariant, interface contract, and open question.

**Required actions:**

1. List all spec files in the target directory with `ls docs/specs/<domain>/`.
2. Read each file end-to-end. Do not trust earlier summaries.
3. Note which documents are authoritative vs. design-decision records (ADRs) vs. implementation plans — they carry different weight.
4. Flag any document that says "undecided," "TBD," or contradicts another document.

**Reference artifact:** `docs/specs/agents/` — 7 documents (overview, spec format, lifecycle, wire protocol, storage, UI, implementation plan).

**Concrete output:** A list of every document read, its role (authoritative/ADR/plan), and the key decisions it contains.

---

## Phase 2: Gap Analysis

Produce a gap-review document that categorizes every missing contract, inconsistency, underspecification, and missing behavior.

**Required structure:**

- **Header:** Date, scope (which documents were reviewed), a statement that this is a review artifact, not an amendment.
- **Overall assessment:** One-paragraph verdict on coherence and the largest category of gap.
- **Tiered gaps:**
  - **Critical** — correctness/integrity/security hazards. Must resolve before any implementation.
  - **High-priority operational** — completeness gaps that would break real usage but not the happy path.
  - **Consistency/completeness** — hardening items that can be resolved during or after initial implementation.

**Each gap entry must contain:**

- **Gap number** (G1, G2, …) for cross-referencing.
- **Title** summarizing the gap.
- **What's missing** — cite the specific doc and line(s) where the underspecification lives.
- **What's at stake** — the concrete harm if left unresolved.
- **Required contract/decision** — what must be specified.
- **Acceptance criteria** — what "resolved" looks like.

**Concrete output:** A file like `docs/specs/<domain>/review-gaps-<date>.md` with 15-20 gaps, prioritized and cross-referenced to source documents.

**Key principle:** Every gap must be traceable back to a specific line in the source spec. Never handwave "we should also think about X" — if X isn't in the spec, it's either a scope gap (note it separately) or a new feature (different pipeline).

---

## Phase 3: Story Creation

Convert each gap into a Linear ticket with structured metadata. One gap → one ticket (rarely, one gap → 2 tickets if it spans unrelated layers).

**Ticket conventions:**

| Field | Rule |
|-------|------|
| **Title** | Conventional-commit prefix (`fix(agent):`, `feat(agent-storage):`, `docs(agent-wire):`, `test(agent):`). Scope matches the package/domain. |
| **Description** | Three sections: **Gap** (what's missing, cite the gap number + source), **Scope** (concrete list of what to build/change), **Acceptance criteria** (testable, specific). |
| **Priority** | Critical gaps → Urgent. High-priority operational → High. Consistency/completeness → Medium. |
| **Labels** | Package/domain labels (`Agents`, `Coordinator`, `Storage`, `TUI`, `WebUI`, `Config`, `Tools`) plus stage markers if needed (`Investigation`). |
| **Project** | All tickets go in the same Linear project so they sort together. |

**Tooling:** Use the `linear_create_issue` MCP tool. After creation, fetch each with `linear_get_issue` to verify the description rendered correctly.

**Concrete output:** N Linear tickets linked to a project, each with a `## Gap` section referencing the gap number, source, and acceptance criteria.

---

## Phase 4: Dependency Analysis

Map every ticket onto a layered dependency graph. This is the core analytical phase — getting this wrong means blocked work later.

### Method

**Step 1 — Layer identification.** Group tickets by architectural layer. Every gap review naturally clusters into layers (e.g., spec → storage → wire → process → UI). Read each ticket's `## Scope` to confirm which layer it touches.

**Step 2 — Edge enumeration.** For each pair of tickets (A, B), ask: "Can B be implemented without A existing?" If no, A → B. Write a one-sentence rationale for every edge.

**Step 3 — Graph construction.** Draw the graph. Use `└→`, `├→`, `▶`, and `∥` (parallel) symbols so it renders in monospace. Include the edge rationale as a table.

**Step 4 — Independence check.** Identify tickets with **zero incoming edges** — these are parallel candidates. Identify tickets with **4+ incoming edges** — these are bottlenecks.

### Cross-cutting tickets

Some tickets are not layers but **cross-cutting concerns** (testing, observability, documentation audit):

- **Test-gate tickets** (e.g., "add acceptance tests to each story"): Start alongside the first implementation phase and run in parallel with every ticket, not after everything else.
- **Observability tickets**: Depend only on the layers they instrument (e.g., observability needs the state machine and budget model to exist before it can emit those transitions/metrics).
- **Documentation-audit tickets**: Usually have zero or minimal code dependencies — can run anytime.

### Output format

Deliver a section for each layer with:

1. The dependency arrows (ASCII art).
2. An edge-rationale table.
3. A note on parallelism within the layer.

Then deliver the **complete graph** and the **critical path** (the longest chain of strictly sequential tickets).

**Reference:** The CAT-89–106 analysis above is a worked example — 18 tickets layered into 7 architectural tiers with a 12-ticket critical path. Use it as a template.

---

## Phase 5: Execution Process Design

Convert the dependency graph into a phased execution plan with explicit parallelism.

### Phase structure

Each phase groups tickets that can run concurrently. Name phases by theme, not by number:

```
P0: Design Foundation (resolve the contracts)
P1: Storage (build the persistence layer)
P2: Wire/Protocol (define communication)
P3: Process/Coordinator (wire runtime behavior)
P4: Security/Trust (cross-cutting invariants)
P5: UI (presentation — depends on everything)
Cross-cut: Testing + Observability (run alongside)
```

### Phase table format

| Phase | Tickets | Rationale |
|-------|---------|-----------|
| P0: Design | CAT-89 ∥ CAT-105 | Foundation. 2 tickets, parallel. |
| P1: Storage | CAT-93 ∥ CAT-92 → CAT-101 → CAT-97 ∥ CAT-96 | Build persistence. … |

Use `∥` for parallel tickets, `→` for sequential ones within a phase.

### Executing the plan

When it's time to implement, use the `linear_update_issue` MCP tool to move tickets from Backlog → In Progress as each phase starts. Start with the independent tickets in each phase, then move to the sequential ones as dependencies resolve.

---

## Phase 6: Implementation Coordination

### Before starting a phase

1. **Verify prerequisites.** For each ticket in the phase, check that all upstream tickets are `state: Done` (not just In Progress). A dependency that's "mostly done" is not done — the contract may still shift.
2. **Check for drift.** If any upstream ticket's implementation diverged from its original spec, re-read the merged code and update downstream tickets if the contract changed.
3. **Claim and move.** Use `linear_update_issue` to set each ticket to `In Progress` and assign it as work begins.

### During implementation

- **Cross-cut tests alongside code.** Don't defer test gates (CAT-104 pattern). Each implementation story should have its acceptance criteria exercised in the same phase, not in a later testing phase.
- **One writer per layer.** Don't implement CAT-92 and CAT-101 simultaneously if they touch the same storage schema — the second writer will rebase on the first's changes. Within a phase, parallelize across *different files/packages*, not the same one.
- **Address contract changes.** If implementing CAT-89 reveals that the capability equation needs a new field that CAT-101's schema must store, open a note on CAT-101 immediately — don't let the downstream ticket assume a stale contract.

### After completing a phase

1. **Re-validate the graph.** Check that the next phase's dependencies are genuinely resolved. Sometimes a ticket's scope shifts during implementation.
2. **Update Linear status.** Move completed tickets to `Done`, add a note summarizing what was built and any contract changes.
3. **Re-read the next phase's tickets.** After a week of implementation, re-reading tickets often reveals new connections or stale assumptions that weren't visible during the dependency analysis.

---

## Concrete example: Agent Processes v1 (CAT-89–106)

The full worked pipeline for this project is documented above in the Dependency Analysis. Quick reference:

- **Source:** `docs/specs/agents/review-gaps-2026-07-13.md` (G1–G18)
- **18 tickets:** CAT-89 through CAT-106, all in the `tau: Agent Processes v1` Linear project
- **Layers:** Design (CAT-89, CAT-105) → Storage (CAT-92, CAT-93, CAT-101, CAT-97, CAT-96) → Wire (CAT-94, CAT-98, CAT-99) → Process (CAT-90, CAT-91, CAT-95) → Security (CAT-100, CAT-102) → UI (CAT-103) + cross-cutting (CAT-104, CAT-106)
- **Critical path:** CAT-89 → CAT-92 → CAT-101 → CAT-97 → CAT-90 → CAT-95 → CAT-103 (12 tickets)
- **Biggest bottleneck:** CAT-90 (resume authorization) with 5 upstream dependencies
- **Parallel slots:** CAT-105, CAT-93, CAT-96, CAT-100, CAT-102, CAT-104, CAT-106

---

## Tools used throughout

| Phase | Tool | Purpose |
|-------|------|---------|
| 1 | `read` | Read spec documents |
| 1 | `bash` (`ls`, `find`) | List spec files |
| 2 | `write` | Create gap-review document |
| 3 | `linear_create_issue` | Create tickets from gaps |
| 3 | `linear_get_issue` | Verify ticket content |
| 4 | `linear_get_issue` | Fetch all tickets for analysis |
| 4 | `ctx_batch_execute` | Batch-fetch tickets in parallel |
| 5-6 | `linear_update_issue` | Move tickets through states |
| 5-6 | `linear_list_issues` | Check project status |

## Principles

1. **Every gap traces to a source line.** No handwaving.
2. **Every edge has a one-sentence rationale.** "A → B because…"
3. **Layers are real.** Storage doesn't depend on UI. Wire doesn't depend on Process. The graph is a DAG; if it has cycles, the spec has a circular definition.
4. **Cross-cutting tickets run alongside, not after.**
5. **Contract changes propagate.** If an upstream ticket's implementation changes a contract, downstream tickets must be re-examined before they start.
6. **The gap document is a review artifact, not an amendment.** It captures findings at a point in time. The authoritative spec is the spec documents — the gap review points to them, not replaces them.

## Gotchas

### Shell escaping in `git commit -m`

When the commit message contains backticks with double-quoted strings inside them (e.g. `` `"skill"` ``), `bash` interprets the backticks as command substitution and the `"` pairs as shell-quoting. The commit still succeeds (git receives the message via stdin, not the shell), but stderr is noisy with "command not found" errors, and the message may be garbled.

**Fix:** Use single quotes for the `-m` argument when the message contains backticks or double-quoted strings:

```shell
# BROKEN — shell interprets backticks as command substitution
git commit -m "fix(agent): remove `m[\"skill\"] = true`"

# CORRECT — single quotes prevent shell interpretation
git commit -m 'fix(agent): remove m["skill"] = true injection'

# ALSO CORRECT — write the message to a file first
git commit -F /tmp/commit-msg.txt
```

Reproduced 2026-07-13 during CAT-89 commit (commit `c4757a0`). Shell noise on stderr but the commit applied correctly.
