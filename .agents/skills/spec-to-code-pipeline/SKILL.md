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

A six-phase pipeline for converting spec documents into sequenced, parallelizable, hazard-free implementation tickets.

## Phase 1: Spec Review

Read every document in the spec directory. Don't skim — note every decision, invariant, interface contract, and open question.

**Required actions:**

1. List all spec files in the target directory (e.g., `ls docs/specs/<domain>/`).
2. Read each file end-to-end. Do not trust earlier summaries.
3. Note which documents are authoritative vs. design-decision records (ADRs) vs. implementation plans — they carry different weight.
4. Flag any document that says "undecided," "TBD," or contradicts another document.

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

Convert each gap into an issue-tracker ticket with structured metadata. One gap → one ticket (rarely, one gap → 2 tickets if it spans unrelated layers).

**Ticket conventions:**

| Field | Rule |
|-------|------|
| **Title** | Conventional-commit prefix matching the domain (e.g., `fix(storage):`, `feat(wire):`, `docs(spec):`, `test(integration):`). |
| **Description** | Three sections: **Gap** (what's missing, cite the gap number + source), **Scope** (concrete list of what to build/change), **Acceptance criteria** (testable, specific). |
| **Priority** | Critical gaps → Urgent/Critical. High-priority operational → High. Consistency/completeness → Medium. |
| **Labels** | Package/domain labels matching the project's architecture, plus stage markers if needed. |
| **Project** | All tickets go in the same project/board so they sort together. |

**Concrete output:** N tickets in the project's issue tracker, each with a `## Gap` section referencing the gap number, source, and acceptance criteria.

---

## Phase 4: Dependency Analysis

Map every ticket onto a layered dependency graph. This is the core analytical phase — getting this wrong means blocked work later.

### Method

**Step 1 — Layer identification.** Group tickets by architectural layer. Every gap review naturally clusters into layers (e.g., spec → storage → wire → process → UI). Read each ticket's scope to confirm which layer it touches.

**Step 2 — Edge enumeration.** For each pair of tickets (A, B), ask: "Can B be implemented without A existing?" If no, A → B. Write a one-sentence rationale for every edge.

**Step 3 — Graph construction.** Draw the graph. Use `└→`, `├→`, and `∥` (parallel) symbols so it renders in monospace. Include the edge rationale as a table.

**Step 4 — Independence check.** Identify tickets with **zero incoming edges** — these are parallel candidates. Identify tickets with **4+ incoming edges** — these are bottlenecks.

### Cross-cutting tickets

Some tickets are not layers but **cross-cutting concerns** (testing, observability, documentation audit):

- **Test-gate tickets**: Start alongside the first implementation phase and run in parallel with every ticket, not after everything else.
- **Observability tickets**: Depend only on the layers they instrument.
- **Documentation-audit tickets**: Usually have zero or minimal code dependencies — can run anytime.

### Output format

Deliver a section for each layer with:

1. The dependency arrows (ASCII art).
2. An edge-rationale table.
3. A note on parallelism within the layer.

Then deliver the **complete graph** and the **critical path** (the longest chain of strictly sequential tickets).

---

## Phase 5: Execution Process Design

Convert the dependency graph into a phased execution plan with explicit parallelism.

### Phase structure

Each phase groups tickets that can run concurrently. Name phases by theme, not by number:

```
P0: Design Foundation (resolve the contracts)
P1: Storage (build the persistence layer)
P2: Wire/Protocol (define communication)
P3: Process/Runtime (wire behavior)
P4: Security/Trust (cross-cutting invariants)
P5: UI (presentation — depends on everything)
Cross-cut: Testing + Observability (run alongside)
```

### Phase table format

| Phase | Tickets | Rationale |
|-------|---------|-----------|
| P0: Design | T-1 ∥ T-2 | Foundation. 2 tickets, parallel. |
| P1: Storage | T-3 ∥ T-4 → T-5 → T-6 ∥ T-7 | Build persistence. … |

Use `∥` for parallel tickets, `→` for sequential ones within a phase.

### Executing the plan

Start with the independent tickets in each phase, then move to the sequential ones as dependencies resolve. Move tickets through statuses in the issue tracker as phases progress.

---

## Phase 6: Implementation Coordination

### Before starting a phase

1. **Verify prerequisites.** For each ticket in the phase, check that all upstream tickets are done (not just started). A dependency that's "mostly done" is not done — the contract may still shift.
2. **Check for drift.** If any upstream ticket's implementation diverged from its original spec, re-read the merged code and update downstream tickets if the contract changed.
3. **Claim the work.** Set each ticket to In Progress as you begin.

### During implementation

- **Cross-cut tests alongside code.** Each implementation story should have its acceptance criteria exercised in the same phase, not in a later testing phase.
- **One writer per layer.** Don't implement two tickets simultaneously if they touch the same schema or module — the second writer will rebase on the first's changes. Within a phase, parallelize across *different files/packages*, not the same one.
- **Address contract changes.** If implementing a ticket reveals a new field or contract that a downstream ticket's scope depends on, open a note on the downstream ticket immediately — don't let it assume a stale contract.

### After completing a phase

1. **Re-validate the graph.** Check that the next phase's dependencies are genuinely resolved. Sometimes a ticket's scope shifts during implementation.
2. **Update tracker status.** Move completed tickets to Done, add a note summarizing what was built and any contract changes.
3. **Re-read the next phase's tickets.** After a week of implementation, re-reading tickets often reveals new connections or stale assumptions that weren't visible during the dependency analysis.

---

## Validation checklist

After completing Phase 4 (dependency analysis), validate before committing:

### Gap traceability
- [ ] Every gap (G1–GN) has a corresponding ticket reference in the gap-review document
- [ ] Every ticket's description cites the gap number and source document
- [ ] No gap is referenced by two tickets (one gap → one ticket, rarely two)

### Dependency graph
- [ ] Every edge has a one-sentence rationale (no "A → B" without "because…")
- [ ] The graph is a DAG (no cycles; cycles mean circular spec definitions)
- [ ] No ticket depends on a lower-priority ticket without justification
- [ ] Cross-cutting tickets (test gates, observability) are not on the critical path

### Layer integrity
- [ ] Storage-layer tickets don't depend on UI-layer tickets
- [ ] Wire-layer tickets don't depend on Process-layer tickets
- [ ] Every ticket's scope confirms it touches only its declared layer

### Execution plan
- [ ] The critical path is identified and explicitly stated
- [ ] Parallel tickets (`∥`) are genuinely independent (no shared code paths)
- [ ] Tickets with 4+ dependencies are flagged as bottlenecks
- [ ] The phase table shows `→` for sequential and `∥` for parallel

### Repository state
- [ ] `git log --oneline -20` confirms what's already built vs spec-only
- [ ] Implementation packages named in the spec exist and their key functions resolve
- [ ] No claim about implementation status is made without checking the working tree

---

## Principles

1. **Every gap traces to a source line.** No handwaving.
2. **Every edge has a one-sentence rationale.** "A → B because…"
3. **Layers are real.** Storage doesn't depend on UI. Wire doesn't depend on Process. The graph is a DAG; if it has cycles, the spec has a circular definition.
4. **Cross-cutting tickets run alongside, not after.**
5. **Contract changes propagate.** If an upstream ticket's implementation changes a contract, downstream tickets must be re-examined before they start.
6. **The gap document is a review artifact, not an amendment.** It captures findings at a point in time. The authoritative spec is the spec documents — the gap review points to them, not replaces them.
7. **Verify, don't assume.** Before making any claim about repository state — "this doesn't exist", "this isn't implemented", "nothing has been built" — check `git log`, `ls`, and actual file contents. Spec work and implementation work can happen in parallel on the same branch. A session spent editing spec documents doesn't mean the code is absent; it means you haven't looked at the code. Fabricating a status report from assumptions rather than evidence is the single most expensive mistake in this pipeline.
8. **Read code before writing code.** When implementing a spec gap, read the existing implementation first. Compare against the spec. Fix only what's missing. Do not re-implement what already works.

## Gotchas

### Shell escaping in `git commit -m`

When the commit message contains backticks with double-quoted strings inside them (e.g. `` `"value"` ``), `bash` interprets the backticks as command substitution. The commit still succeeds (git receives the message via stdin, not the shell), but stderr is noisy with "command not found" errors.

**Fix:** Use single quotes for the `-m` argument when the message contains backticks or double-quoted strings, or write the message to a file and use `git commit -F`.

### Spec work ≠ greenfield

The gap review → story creation → dependency analysis pipeline produces spec updates. These updates land in spec documents. They do NOT mean the implementation is starting from zero. Implementation code may already exist in the working tree while the spec documents are being gap-filled. Claiming "the implementation hasn't started" without running `ls` or `git log` is a fabrication.

**Rule:** Before reporting on implementation status, always check:
- `git log --oneline -20` to see recent commits
- `ls` on the implementation packages named in the spec
- `grep` for key functions/types to confirm they exist

### Parallelism markers

Use these ASCII-art conventions consistently:
- `A ∥ B` — A and B are parallel (no dependency between them)
- `A → B` — B depends on A (sequential)
- `├→` and `└→` — branching in the graph
- `▶` — points to a dependency target
