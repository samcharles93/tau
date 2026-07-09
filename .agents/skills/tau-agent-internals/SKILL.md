---
name: tau-agent-internals
description: This skill should be used when debugging tau's own agent coordinator, tui2 rendering, or live model/provider discovery — for example "a tool call looks stuck at pending", "the input box layout is wrong", "reasoning effort has no options for a model", "a config default isn't applying", "cost calculation looks off", or when a fix needs to target the correct one of tau's two TUI implementations.
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

## tui2 chrome layout (internal/tui2/model.go)

- The input box's hint row (the line just under the top border) is always reserved by `renderInputBox`, even when the hint string is empty — removing hint text does not shrink the box. Relocating a hint elsewhere requires no box-resizing logic; stop passing text into that slot.
- The notification banner directly above the input box reserves a fixed height (`notifyReservedLines`) at all times, via `padOrClipLines`, specifically so the viewport doesn't visibly grow or shrink as notifications appear and clear. That reserved-but-usually-empty area is a good home for transient UI hints that would otherwise bloat another widget.
- `computeStatusBar` in `internal/tui2/statusbar.go` builds the right-aligned status segments (`session tok`, cost, `ctx N%`, `web:`) in explicit priority order and drops the lowest-priority ones first under width pressure; segments are not simply concatenated.

## Live model discovery gap (internal/app/live_models.go)

Models listed via a provider's generic OpenAI-compatible `/v1/models` endpoint (`liveModelRefs`, used for the zero-config local-Ollama catalog entry) get an essentially empty `ModelConfig{ID: id}`, since the generic endpoint carries no capability data. This leaves `/effort` with nothing to offer (`effortLevels` returns just `["auto"]`), so reasoning cannot be turned on for those models through any UI path even when the underlying provider fully supports it.

Ollama exposes real capability data (`"capabilities":["completion","tools","thinking","vision"]`) via its native `/api/tags` endpoint, a sibling of the OpenAI-compat `/v1` surface at the same host. `ollamaThinkingModels` cross-checks that endpoint (best-effort — a probe failure must not fail model listing) to synthesize `Reasoning: true` plus `["low","medium","high"]` effort levels for models advertising `"thinking"`, matching what snapshot-sourced (models.dev) reasoning models already get via `modelInfoToModelConfig`. When adding another live-discovered provider, check for an equivalent native capability endpoint worth cross-referencing the same way.

## Config default application (internal/config/config.go)

Boolean UI config fields that need a non-false default use a tri-state pattern: the field itself (`ShowReasoning bool`) plus a private `showReasoningSet bool` companion, set only when the YAML key was actually present (via a `*bool` in the raw-unmarshal struct). `loadConfigFrom` then applies `cfg.UI.ShowReasoning = true` only when `!cfg.UI.showReasoningSet` — only when neither the global nor local config file mentioned the key at all. A plain zero-value bool cannot distinguish "explicitly set to false" from "never mentioned"; use this pattern whenever a bool's desired default is `true` rather than the Go zero value.

## Cost accounting (internal/store/sqlite_store.go)

`calculateCost` prices `PromptTokens` as already including `CachedTokens`/`CacheCreationTokens` as subsets, matching `chat.Usage`'s cross-provider convention (see the `llm-provider-quirks` skill for why Anthropic's raw usage fields don't naturally have this shape). It subtracts both out before applying the base input rate, then prices `CachedTokens` at `cost.CacheRead` and `CacheCreationTokens` at `cost.CacheWrite` separately. A new usage field that changes what `PromptTokens` includes requires a matching update here, or costs will silently double-count or under-count.

## Two separate TUIs

`internal/tui` (legacy) and `internal/tui2` (current) both exist and both handle chat events — for example `notifyDurationFromChat` in `internal/tui/inline_events.go` and the tool-status/notification logic in `internal/tui2/model.go` are separate implementations of overlapping concerns. Confirm which one is actually active for the behavior under investigation (check whether `tui2.Run(...)` or the legacy path is invoked) before assuming a fix in one applies to both.
