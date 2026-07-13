---
name: dogfooding-methods
description: Helps evaluate Tau through disciplined self-use, turning daily friction into better product decisions and implementation priorities.
user-invocable: true
---

# Dogfooding Methods

Use this skill when the user is working on Tau itself, especially when they ask what to improve next, how to interpret their own usage, how to prioritise bugs, or how to turn daily friction into product direction.

Dogfooding means using Tau as the primary tool for building Tau, then treating the friction from that work as product evidence. Do not optimise for hypothetical users too early. First optimise for the real operator using the tool today.

## Core stance

When helping with dogfooding, prioritise:

1. Real friction observed during actual use
2. Fast fixes that improve the daily development loop
3. Features that make Tau better at improving Tau
4. Instrumentation that reveals where Tau wastes attention
5. Reliability and trust before ecosystem polish
6. External-user compatibility only when sharing or onboarding is imminent

Be direct. Separate product reality from imagined future market needs.

## Vocabulary

Use these terms consistently:

* **Dogfood loop**: Use Tau, notice friction, capture it, fix or defer it, then use Tau again.
* **Papercut**: A small annoyance that repeats often enough to matter.
* **Golden path**: The most common successful workflow Tau should make effortless.
* **Friction log**: A short record of moments where Tau slowed the user down.
* **Trust break**: Any behaviour that makes the user question whether Tau is safe, correct, or worth using.
* **Loop closer**: A change that makes Tau better at diagnosing, fixing, testing, or documenting itself.
* **Product smell**: A signal that the shape of the product is fighting the user.
* **Backlog rot**: Issues that no longer reflect reality, causing attention waste.
* **Polish trap**: Work that feels productive but does not improve the daily loop.
* **Compatibility theatre**: Work done for imagined third-party users before there are real third-party users.

## Decision rule

When ranking work, use this order:

1. Does it stop Tau from working?
2. Does it break trust?
3. Does it interrupt the user's daily flow?
4. Does it make future Tau work easier?
5. Does it improve observability, diagnosis, or testability?
6. Does it help future external users?

If two items look equal, choose the one that will be felt sooner in the next dogfood session.

## Evidence levels

Classify each finding by evidence strength:

* **Level 0, idea**: Sounds useful, not yet observed.
* **Level 1, noticed once**: Happened once during real use.
* **Level 2, repeated papercut**: Happened multiple times or affected focus.
* **Level 3, workflow blocker**: Stopped or seriously slowed real work.
* **Level 4, trust break**: Caused data loss, incorrect output, stale state, unsafe action, or misleading feedback.

Prioritise Level 3 and Level 4 issues before speculative features.

## Dogfood loop

When reviewing Tau, follow this loop:

1. **Observe**
   Capture what the user was trying to do, what Tau did, and what felt wrong.

2. **Name the friction**
   Use a concrete label like "draft loss", "stale backlog", "slow verification", "unclear command state", or "mode confusion".

3. **Find the smallest fix**
   Prefer a narrow fix that improves the next session over a broad redesign.

4. **Add a regression guard**
   Add a test, script, check, or documented repro so the issue does not return silently.

5. **Update the system**
   Keep Linear, GitHub issues, docs, and AGENTS.md aligned when behaviour or architecture changes.

6. **Dogfood again**
   Use Tau for the next Tau task and watch whether the fix actually reduced friction.

## What to look for

When analysing Tau, actively search for:

* Places where the user has to restart Tau to pick up changes
* Input handling that loses text, surprises muscle memory, or hides state
* UI elements that overflow, jitter, flicker, or steal attention
* Commands that exist but are hard to discover
* Session resume behaviour that drops context or renders late
* Backlog items that are already fixed or no longer relevant
* Quality checks that are noisy, flaky, slow, or not trusted
* Places where docs, Linear, GitHub, and code disagree
* Features that should be plugins but are being hardcoded into core
* Plugin or skill APIs that make simple workflows awkward
* Anything that makes Tau worse at building Tau

## Prioritisation heuristics

For a solo pre-release project, use this priority order:

1. Daily-use bugs
2. Trust and data-loss issues
3. TUI feel and input ergonomics
4. Fast local verification
5. Session continuity
6. Skill and command discoverability
7. Internal architecture simplification
8. Plugin ergonomics for the user's own workflows
9. External compatibility
10. Enterprise or multi-user features

Do not over-prioritise migration paths, backwards compatibility, or colleague onboarding until the user is actually preparing to share Tau.

## Recommended response shape

When the user asks what to improve next, respond with:

1. **Top recommendation**
   State the single next thing to do.

2. **Why this matters now**
   Tie it to real dogfood friction.

3. **Smallest useful implementation**
   Suggest the narrowest change that proves value.

4. **Regression guard**
   Suggest the test, check, fixture, script, or manual repro.

5. **What to defer**
   Name the tempting but lower-value work to avoid.

Example:

> Fix Ctrl+C input clearing next. It is a daily muscle-memory papercut, not a theoretical bug. Add an idle non-empty-input branch before quit confirmation, reset history navigation, and cover it with a small model test. Defer plugin backwards compatibility until Tau has external plugin users.

## When reviewing backlog

Treat stale backlog as a product bug.

For each issue, classify it as:

* **Do now**
* **Do after current dogfood friction**
* **Before sharing externally**
* **Later commercial direction**
* **Already done, close it**
* **No longer relevant, close it**

Prefer closing stale work over carrying it forward.

## When proposing Linear issues

Write issues that include:

* Symptom
* Repro
* Expected behaviour
* Suspected file or subsystem
* Smallest acceptable fix
* Regression guard
* Dogfood impact

Avoid vague issue titles like "Improve UX". Use titles like:

* `fix(tui2): Ctrl+C clears idle draft input before quit confirmation`
* `test(tui2): add deterministic parity run with fake provider`
* `chore(linear): close stale Tau issues already fixed on main`
* `feat(skills): expose skill catalogue from /skills`

## Anti-patterns

Avoid these:

* Building for imaginary users before the solo dogfood loop is strong
* Treating every annoyance as a feature request
* Filing issues without a repro or decision point
* Doing compatibility work before there is something to be compatible with
* Keeping fixed issues open because closing them feels administrative
* Adding a new subsystem when a small patch to an existing one would work
* Calling something "polish" when it actually affects trust or flow
* Treating docs as separate from the product experience

## Strong opinions

If Tau is still mostly private, the best product strategy is:

* Make it excellent for the primary user first.
* Make the fastest path through common Tau-on-Tau work feel addictive.
* Prefer boring reliability over impressive roadmap items.
* Keep the backlog honest.
* Instrument what hurts.
* Share only once the golden path feels calm, fast, and hard to break.

When in doubt, ask: "Will Sam feel this improvement in the next two Tau sessions?"

If yes, prioritise it.

If no, defer it unless it prevents data loss, security trouble, or architectural lock-in.
