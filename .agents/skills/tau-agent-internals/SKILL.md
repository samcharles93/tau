---
name: tau-agent-internals
description: This skill should be used when debugging tau's own agent coordinator, tui2 rendering, or live model/provider discovery — for example "a tool call looks stuck at pending", "the input box layout is wrong", "reasoning effort has no options for a model", "a config default isn't applying", "a model is stuck repeating the same tool call", "a config field I set isn't taking effect", "tool boxes render out of order", "manual scroll snaps back to the bottom", "markdown reverts to plain text", "cost calculation looks off", or when a fix needs to target the correct one of tau's two TUI implementations.
source: project
scope: project
enabled: true
user_invocable: true
priority: 15
---

# Tau Agent Internals

Non-obvious facts about tau's own architecture, found by debugging real symptoms rather than derived from reading docs. File names below are anchors, not guarantees — grep for the named function or type if line numbers have drifted.

## Tool execution event ordering (internal/agent/coordinator.go)

Build `executeTool`'s `run()` closure and emit `ChatToolExecutionStartedEvent` before calling `tool.Execute()`, not after. This event is the only signal that flips a tool call's UI status from `"pending"` to `"running"`. Emitting it after `Execute()` returns leaves every tool call displayed as `"pending"` for its entire real duration, making a legitimately slow call indistinguishable from a hang. This was live-reproduced: a two-tool-call turn sat at `"pending (1:39)"` with `"No output"` and no visible reasoning to explain the delay, reading as a hang when it was actually a slow call.

When touching this function, preserve the shape: resolve which `tool.Execute` call applies — including plugin `before_tool_exec` argument mutation and loop-breaker blocking — without running it, emit the started event, then invoke it.

## Tool-loop breaker with override (internal/agent/coordinator.go)

A model can get stuck emitting the same tool call over and over (live-reproduced: 103 identical calls in a row). The guard counts consecutive calls whose normalized key (name + arguments) matches the previous one — `lastToolStreak` against `toolLoopSoftThreshold`. It is deliberately **not** a hard block, because legitimately repeating a call is sometimes correct (re-running the same test after edits, polling for a state change, sub-agents doing similar work). The escape hatch is a `repeat_justification` argument: the model adds a short reason and the call is let through (`justified: true`). The comparison key **excludes** `repeat_justification` (it is deleted before normalization), so adding a justification doesn't change the streak identity. A backstop `toolLoopHardBlockLimit` (currently 3) ends the turn outright if a call keeps being justified past the limit. When editing this, keep the override path — a plain hard block was explicitly rejected.

Observability was a first-class requirement here, not an afterthought: when adding behaviour like the loop breaker, wire it into structured logging/metrics at the same time, because "have those changes been logged or added to metrics tracking?" is a check that will be applied. Emit a structured record when a streak is blocked or justified rather than silently swallowing it.

## tui2 rendering pipeline invariants (internal/tui2/model.go)

Three distinct drift bugs all came from the same root shape — a second render path that didn't match the finalized one. Preserve these invariants:

- **Commit history on boundaries, not on a timer.** Committed (finalized) history is always rendered before the live streaming buffer. If commits are flushed on a timer, a tool-call box can be committed and jump *ahead* of text that chronologically preceded it but is still in the live buffer. Commit at content boundaries so on-screen order matches chronological order.
- **`autoFollow` gates bottom-pinning.** The viewport must not be forced to the bottom on every render while a response streams — that stomps any manual scroll-up. Keep the `autoFollow` flag (model.go ~189): pin to bottom only while it is true, and clear it when the user scrolls away so their scroll position survives incoming stream chunks.
- **`ChatSessionSnapshotEvent` must re-render through the same renderer as finalized messages.** This event fires routinely (compaction, tool turns, session load — see the many emit sites in `coordinator.go` and `compact.go`), not just on initial load. If its handler rebuilds the viewport from raw text, already-rendered markdown reverts to plain text every time it fires. Route snapshot rebuilds through the same markdown/glamour renderer used for finalized messages.

## Layout is the single source of truth for render and mouse (internal/tui2/model.go)

`computeLayout()` returns the geometry of every non-viewport interactive region, and it is the single source of truth for **both** `View()`'s rendering and `handleMousePress`/`handleMouseDrag`'s hit-testing — so the two can never disagree about where a clickable region actually is. When adding a new interactive region (click-to-focus, click-to-expand), add it to `computeLayout` and read from that structure in both the render and the hit-test path; never compute a region's position independently in the mouse handler. The same pattern is used deliberately elsewhere (`completions.go` for the completion popup, `clampContextMenuPosition` for the context menu's draw-vs-click-away bounds). Mouse-drag text selection across the viewport, input box, and status bar is built on one shared `selectionState` primitive with a single `finalizeSelection` method (each region supplies a position-mapper and a text-extractor), not per-region hand-rolled state machines — see the general `single-source-of-truth` skill for the design rule.

## Config default application (internal/config/config.go)

Boolean UI config fields that need a non-false default use a tri-state pattern: the field itself (`ShowReasoning bool`) plus a private `showReasoningSet bool` companion, set only when the YAML key was actually present (via a `*bool` in the raw-unmarshal struct). `loadConfigFrom` then applies `cfg.UI.ShowReasoning = true` only when `!cfg.UI.showReasoningSet` — only when neither the global nor local config file mentioned the key at all. A plain zero-value bool cannot distinguish "explicitly set to false" from "never mentioned"; use this pattern whenever a bool's desired default is `true` rather than the Go zero value.

## Config round-trip pitfall: hand-written UnmarshalYAML (internal/config/config.go)

Several `Config` types have hand-written `UnmarshalYAML` methods that decode into a private mirror struct and then copy fields across. If a field exists on the outer struct but is **missing from the internal decode struct**, that field silently never round-trips from YAML — the user can set it and it is dropped with no error. This bug surfaced only via an unrelated test failure, not by direct inspection. Whenever you add a field to a `Config` type that has a custom `UnmarshalYAML`, add it to the internal decode struct in the same edit, and prefer a round-trip test (marshal a value, unmarshal, assert equality) over trusting the struct tags.

## Config self-healing (internal/config/config.go)

`syncConfigSchema` backfills missing top-level config blocks into the on-disk file on load, for forward-compatible upgrades. Its general rule is that it **never touches a key that is already present**, even if set to a zero value. There is one deliberate, narrow exception (`backfillMetricsDir`): an empty/missing `metrics.dir` is treated as "needs a real default" and backfilled to `MetricsDir()`, because a blank dir there almost never means "intentionally disabled." When extending this function, keep exceptions narrow and comment the justification — do not loosen the general "never overwrite existing values" contract.

## Cost accounting (internal/store/sqlite_store.go)

`calculateCost` prices `PromptTokens` as already including `CachedTokens`/`CacheCreationTokens` as subsets, matching `chat.Usage`'s cross-provider convention (see the `llm-provider-quirks` skill for why Anthropic's raw usage fields don't naturally have this shape). It subtracts both out before applying the base input rate, then prices `CachedTokens` at `cost.CacheRead` and `CacheCreationTokens` at `cost.CacheWrite` separately. A new usage field that changes what `PromptTokens` includes requires a matching update here, or costs will silently double-count or under-count.

## Live model discovery gap (internal/app/live_models.go)

Models listed via a provider's generic OpenAI-compatible `/v1/models` endpoint (`liveModelRefs`, used for the zero-config local-Ollama catalog entry) get an essentially empty `ModelConfig{ID: id}`, since the generic endpoint carries no capability data. This leaves `/effort` with nothing to offer (`effortLevels` returns just `["auto"]`), so reasoning cannot be turned on for those models through any UI path even when the underlying provider fully supports it.

Ollama exposes real capability data (`"capabilities":["completion","tools","thinking","vision"]`) via its native `/api/tags` endpoint, a sibling of the OpenAI-compat `/v1` surface at the same host. `ollamaThinkingModels` cross-checks that endpoint (best-effort — a probe failure must not fail model listing) to synthesize `Reasoning: true` plus `["low","medium","high"]` effort levels for models advertising `"thinking"`, matching what snapshot-sourced (models.dev) reasoning models already get via `modelInfoToModelConfig`. When adding another live-discovered provider, check for an equivalent native capability endpoint worth cross-referencing the same way.

## Two separate TUIs

`internal/tui` (legacy) and `internal/tui2` (current) both exist and both handle chat events — for example `notifyDurationFromChat` in `internal/tui/inline_events.go` and the tool-status/notification logic in `internal/tui2/model.go` are separate implementations of overlapping concerns. Confirm which one is actually active for the behavior under investigation (check whether `tui2.Run(...)` or the legacy path is invoked) before assuming a fix in one applies to both.

Tau also has a separate WebUI (Vue) frontend, and the project principle is that the TUI and WebUI must not drift. Before building a display/interaction feature in tui2, check whether the WebUI already implements it and mirror its logic — for example tool-call grouping (consecutive tool calls with no intervening text collapse into one summary) was ported from the WebUI's `ToolGroup.vue`/`ChatMessage.vue` rather than re-invented. The same applies to plugin-facing views: check the plugin/WebUI surfaces before assuming a behaviour is TUI-only.
