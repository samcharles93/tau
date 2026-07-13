# Agent Process Specification Gap Review

Date: 2026-07-13

Scope: all documents in `docs/specs/agents/` present at review time. This is a review artifact, not an amendment to the authoritative design.

**Status:** 18 gaps identified → 18 Linear tickets created (CAT-89–CAT-106) under `tau: Agent Processes v1`. P0 design phase (CAT-89 + CAT-105) completed 2026-07-13. G1 resolved.

## Overall assessment

The specification is unusually coherent at the architectural level: identity, process isolation, capability attenuation, persistence, protocol, UI projection, and phased delivery form one implementable design. It is not yet implementation-complete, however. The largest gaps are at authority and failure boundaries rather than in the happy path.

Before Phase 1 implementation, resolve G1-G4. Before enabling real parallel delegation, resolve G5-G9. The remaining items can be completed during hardening, but should be added to the implementation plan now so they are not lost.

## Critical gaps

### G1. `skill` tool injection contradicts strict attenuation

**Status: RESOLVED (2026-07-13, CAT-89).**

The lifecycle spec defines the effective toolset as the intersection of the child spec, parent effective tools, and spawn restriction, and says widening is impossible by construction (`02-spawning-and-lifecycle.md`, lines 57-66). The P0.1 ADR instead says every non-empty filter receives `skill`, and gives `{read, grep, skill}` for a spec declaring only `[read, grep]` (`p0.1-per-run-tool-filtering-adr.md`, lines 135-146).

This is a capability grant outside the declared intersection. It also makes mode switching an implicit privilege, even though a mode may alter prompts and behavior.

Required decision: either make `skill` an explicitly declared capability that obeys normal attenuation, or document it as a mandatory ambient capability in the main spec and threat model. The first option preserves the stated security invariant.

Acceptance criteria:

- One normative toolset equation covers root, child, mode, and plugin registration.
- Tests prove no undeclared tool appears at construction or after registry mutation.
- Empty, omitted, and explicit-empty tool lists have distinct, documented serialization semantics.

### G2. Resume authorization and capability recomputation are unspecified

**Status: RESOLVED (2026-07-13, CAT-90).**

**Ticket: CAT-90**

`resume` starts a new process on an existing child session (`02-spawning-and-lifecycle.md`, line 96), but the spec does not state who may resume which session, whether the target must be a descendant of the caller, whether its original spec must match the requested target, or whether effective tools are inherited from the old instance or recomputed against the current parent.

Without a rule, a caller could potentially name an unrelated session or revive a session with capabilities that no longer fit the current tree.

Required contract:

- The session must exist, be ended/not actively owned, and be attributable to an allowed prior agent instance.
- Define whether resume is restricted to direct children, any descendant, or any locally owned child session.
- The new instance must recompute capabilities as `old snapshot/spec ceiling ∩ current parent effective set ∩ spawn restriction`; persisted effective tools must never be trusted as sufficient authority.
- Define model/spec precedence on resume and reject conflicting `agent`/`model` inputs rather than silently changing identity.
- Acquire session ownership atomically so two resumptions cannot run the same session concurrently.

### G3. Instance/session creation is not specified as an atomic operation

**Status: RESOLVED (2026-07-13, CAT-92).**

**Ticket: CAT-92**

Instantiation writes an instance row and then creates or forks a session (`02-spawning-and-lifecycle.md`, lines 15-26), while storage assigns instance lifecycle to the parent and session writes to the child (`04-storage-and-sessions.md`, lines 39-47). The failure behavior between those steps is undefined.

Specify one transaction for instance-row creation plus session creation/fork metadata, followed by process spawn. If process creation fails, close the instance deterministically as `failed` with a structured reason. Also define collision retry for the six-character instance ID and a database uniqueness retry bound.

Acceptance tests should inject failure after every step: ID allocation, instance insert, session fork, pipe creation, process start, ready timeout, and assign write.

### G4. Protocol authority validation is incomplete

**Status: RESOLVED (2026-07-13, CAT-94).**

**Ticket: CAT-94**

The child validates that assigned instance/session IDs exist and match (`03-wire-protocol.md`, line 60), but the parent-side validation rules are absent. `from`/`to`, nested `agent.event` contents, task IDs, session IDs, and result IDs must be treated as untrusted bytes even over a child pipe: child tools and plugins execute code in a separate process and can be buggy or compromised.

Specify that the parent binds a pipe to the instance it spawned and rejects/quarantines envelopes whose `from`, `to`, `task_id`, `instance_id`, or `session_id` do not match that binding. Nested events must be allowlisted and re-attributed by the parent rather than trusted to self-identify. The child must likewise accept commands only for its assigned instance/task.

## High-priority operational gaps

### G5. There is no concurrency or resource ceiling

**Status: RESOLVED (2026-07-13, CAT-91).**

**Ticket: CAT-91**

The design intentionally allows N agent calls in one turn to spawn N children concurrently (`02-spawning-and-lifecycle.md`, line 49), while storage assumes only “a handful” and says depth caps prevent trees beyond tens (`04-storage-and-sessions.md`, line 58). Depth does not bound breadth: one turn can request arbitrarily many siblings, recursively.

Add configurable per-parent and process-wide concurrent-child limits, plus a queued/rejected behavior. Consider total active processes, file descriptors, pending spawn count, and provider concurrency. Define fair cancellation and whether queued children consume timeout/deadline budget.

### G6. Budget semantics are underspecified

**Status: RESOLVED (2026-07-13, CAT-93).**

**Ticket: CAT-93**

The spec names `max_tokens`, deadline, timeout, and cost accumulation (`02-spawning-and-lifecycle.md`, lines 68-77), but does not define:

- whether tokens mean input + output, output only, cached tokens, reasoning tokens, or provider-reported totals;
- whether a child subtree counts against the child's budget as well as the root aggregate;
- behavior when providers omit/delay usage or report it only at completion;
- pre-call admission versus post-call breach handling;
- deadline format: the schema example uses a duration (`"5m"`) while the term “deadline” normally means an absolute timestamp;
- overflow, negative/zero values, maximum values, and cost currency/precision.

Define a canonical usage model and conservative behavior when usage is unknown. Rename relative `deadline` to `timeout`/`max_duration`, or accept an RFC 3339 absolute deadline explicitly.

### G7. Cancellation does not define process-tree termination

**Status: RESOLVED (2026-07-13, CAT-95).**

**Ticket: CAT-95**

The lifecycle table defines cancel, stdin close, grace, and SIGKILL (`02-spawning-and-lifecycle.md`, lines 98-108), but not whether signals target only the direct child or its whole process group. A child may itself have running descendants, provider subprocesses, or shell-tool processes.

Specify process-group creation and tree-wide cancellation propagation, ordering, grace timing, platform behavior, and cleanup when a descendant ignores cancellation. Clarify whether “finish the current tool call if cheap” is permitted after user cancellation; user cancellation should normally stop new work immediately.

### G8. JSONL framing lacks implementation-level failure rules

**Status: RESOLVED (2026-07-13, CAT-98).**

**Ticket: CAT-98**

The wire defines UTF-8 JSONL, an 8 MiB line cap, first-message ordering, and EOF behavior (`03-wire-protocol.md`, lines 18-24), but omits:

- read/write deadlines for `ready`, `assign`, and shutdown;
- malformed JSON, unknown message type, duplicate terminal result, message after result, oversized line, invalid UTF-8, and partial final line behavior;
- atomic serialization of concurrent event writers to prevent interleaved stdout;
- stdout contamination policy for libraries or accidental prints;
- stderr size/rate limiting and secret redaction;
- bounded buffering/backpressure when the parent/UI consumes slowly.

Define a small protocol state machine and a single writer goroutine per endpoint. Add conformance tests for every invalid transition.

### G9. Delivery guarantees are overstated

**Status: RESOLVED (2026-07-13, CAT-99).**

**Ticket: CAT-99**

The protocol calls a pipe “ordered, lossless” (`03-wire-protocol.md`, lines 104-108). Ordering is true per byte stream, but end-to-end losslessness is not guaranteed across process death, forced kill, decoder failure, or parent event dropping. This wording could cause implementation code to omit recovery logic.

Change the guarantee to: ordered and reliable while both endpoints and the pipe remain healthy; no replay or acknowledgement in v1. State which messages are durable in SQLite and how UIs recover after reconnect/restart. Usage/result reconciliation should prefer durable session/instance state over having observed every streamed message.

## Consistency and completeness gaps

### G10. Instance ID entropy and PID orphan detection need stronger rules

**Status: RESOLVED (2026-07-13, CAT-96).** ID collision retry spec'd in CAT-92/CAT-101.

**Ticket: CAT-96**

Six lowercase base32 characters provide roughly 30 bits of entropy (`02-spawning-and-lifecycle.md`, lines 5-11). That may be adequate for a display suffix, but the full address is also the database primary key and protocol identity. Specify collision retry and consider a durable random/UUID primary key with a short display suffix.

The orphan sweep treats a live recycled PID as a reason to delay closure (`04-storage-and-sessions.md`, lines 60-62). Add process start identity where the OS supports it, plus a stale-age bound, so a recycled long-lived PID cannot leave an instance running forever in the UI.

### G11. Prompt/context trust framing is absent

**Status: RESOLVED (2026-07-13, CAT-100).**

**Ticket: CAT-100**

The parent-selected `context` becomes a `<parent_context>` block (`02-spawning-and-lifecycle.md`, lines 35-39), but the spec does not say it is data rather than higher-priority instruction, how delimiters are escaped, or how externally sourced content is labeled. Forked histories may also contain tool output and instructions from different trust origins.

Define prompt assembly precedence and escaping. Persist origin/provenance for delegated context, especially before agents can be triggered through plugins. Child sessions should default to no unrelated persisted/search context; opt-in inheritance must be explicit.

### G12. Snapshot schema and compatibility are not versioned

**Status: RESOLVED (2026-07-13, CAT-97).**

**Ticket: CAT-97**

The resolved spec snapshot is serialized into JSON (`01-agent-spec-format.md`, lines 95-97), but no snapshot schema version or forward/backward compatibility rule is given. Add `snapshot_version`, canonical serialization rules for `spec_hash`, and behavior when a newer binary cannot decode an old snapshot. Clarify whether resume uses the historical body snapshot (identity continuity) or the latest spec (upgrade behavior).

### G13. Storage constraints and indexes are incomplete

**Status: RESOLVED (2026-07-13, CAT-101).**

**Ticket: CAT-101**

The proposed schema should specify:

- foreign key/index for `parent_instance_id`;
- checks for valid depth, timestamps, and exit status;
- whether `usage_json` is nullable and its versioned shape;
- uniqueness/ownership constraint preventing simultaneous active instances for one session;
- transaction/retry policy after SQLite `busy_timeout` expires;
- retention/deletion behavior for an instance whose session is deleted, and vice versa.

Tree traversal should state whether `ListAgentInstances(parentID)` means parent instance or parent session; the current name is ambiguous beside `ListChildren(parentSessionID)` (`04-storage-and-sessions.md`, lines 49-54).

### G14. Root identity override has an unresolved trust boundary

**Status: RESOLVED (2026-07-13, CAT-102).**

**Ticket: CAT-102**

The root `tau` spec can be overridden through normal discovery, with project precedence (`00-overview.md`, decision 1; `02-spawning-and-lifecycle.md`, line 17). Entering an untrusted repository could therefore replace the root agent's prompt while it retains the full registry.

Specify whether project overrides of the root require explicit approval, a trust-on-first-use record, or a config opt-in. At minimum, the UI must show the resolved scope/source/hash before privileged execution. This is separate from child attenuation because the root has no parent ceiling.

### G15. UI recovery, accessibility, and high-volume behavior are missing

**Ticket: CAT-103**

The UI receives the full child event stream and keeps live drill-down state in memory (`05-ui.md`, lines 3-44). Define:

- reconstruction after UI reconnect/restart and what is loaded from durable storage;
- bounded retention/coalescing for high-volume deltas and many concurrent children;
- stable ordering when child events interleave;
- terminal-width/narrow-layout behavior, keyboard navigation, screen-reader/plain-text representation, reduced-motion handling for shimmer, and color-independent status;
- whether parent-turn cancellation remains reachable while a child transcript is expanded.

### G16. Observability and redaction are not requirements

**Status: RESOLVED (2026-07-13, CAT-106).**

**Ticket: CAT-106**

Add structured spawn/ready/assign/cancel/result transition logs with instance/task/session correlation, durations, exit classification, protocol error kind, queue time, and resource-limit rejection. Never log prompts, parent context, tool arguments, environment, or raw stderr by default. Metrics should separate direct usage from subtree aggregate usage to avoid double counting.

### G17. The implementation plan lacks per-item acceptance gates

**Ticket: CAT-104**

`task check` is the only universal gate (`06-implementation-plan.md`, line 3), and P5.1 bundles most end-to-end behavior late (`06-implementation-plan.md`, lines 56-63). Security and lifecycle invariants should be tested in the phase that introduces them, not deferred to Phase 5.

Add acceptance criteria to every P1-P4 story and protocol conformance/property tests to P2. Required cases include malformed envelopes, spoofed identity, duplicate result, concurrent writer serialization, queue saturation, SQLite busy failure, spawn rollback, resume race, plugin tool registration during a run, unknown usage, cancellation of descendant process groups, and restart reconstruction.

### G18. P0 status is inconsistent across documents

**Ticket: CAT-105 — RESOLVED (2026-07-13).**

The implementation plan still lists P0.4 as undecided (`06-implementation-plan.md`, line 12), while the format spec says `task.agent.md` is retired by P0.4 (`01-agent-spec-format.md`, lines 86-92). P0.1 also reports “4 of 7 design steps implemented” while serving as an ADR for pre-implementation work.

Mark each investigation as proposed/decided/implemented with links to its output and update the phase plan. Separate design completion from code completion so readers know what remains.

## Recommended plan changes

1. Add a Phase 0 authority ADR covering `skill`, resume authorization, root override trust, and prompt/context provenance.
2. Add a Phase 0 protocol-state ADR covering validation, framing failures, backpressure, and process groups.
3. Extend P1.4/P1.5/P1.6 with atomic creation, session ownership, schema versioning, collision retry, and deletion/retention semantics.
4. Extend P2.3/P2.5 with the state machine, single writer, timeouts, process groups, and invalid-frame conformance tests.
5. Extend P2.4/P3.3 with canonical usage/budget semantics and subtree accounting.
6. Add a concurrency-control story before P3.4.
7. Add UI recovery/accessibility/high-volume acceptance criteria to P4.
8. Move invariant-specific tests into their implementation stories; keep P5.1 for composed end-to-end validation.

## Definition of spec-complete

The agent-process spec is ready for implementation when:

- every authority transition has an explicit validator and owner;
- every persisted multi-row state transition is atomic or has defined compensation;
- every protocol message has valid states, invalid-state behavior, size/time limits, and correlation rules;
- concurrency, token, time, storage, and process limits have deterministic semantics;
- resume cannot widen authority or produce two writers for one session;
- cancellation demonstrably reaches the whole descendant process tree;
- the UI can recover from durable state and remains usable under bounded high-volume fan-out;
- each implementation story carries its own executable acceptance criteria.
