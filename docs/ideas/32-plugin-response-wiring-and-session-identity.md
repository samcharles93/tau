# 32. Plugin response wiring should follow Pi's split between lifecycle and mutation hooks

## Status

Proposed refinement of the Tau plugin implementation plan.

## Summary

Tau's current plugin work should not focus on adding more hook types first. The immediate problems are:

1. **Session identity is not propagated reliably**, which breaks resume-oriented behavior and leaves many events effectively anonymous to plugins.
2. **Merged plugin responses are mostly ignored**, so the contract exists on paper but not in coordinator behavior.

The implementation should follow the shape proven in Pi:

- keep **lifecycle events** distinct from **mutation/blocking hooks**
- perform **permission gating at the tool boundary**, not by stripping tools from an LLM request
- when a plugin blocks a tool, produce an **immediate tool error result** using the plugin-provided reason
- make **session start/resume/shutdown** explicit enough that stateful extensions can rehydrate safely

## Decisions

### 1. Session identity is Phase 0

Before wiring more `EventResponse` fields, Tau should pass an explicit session ID through every plugin dispatch path. The current `DispatchEventRequest.session_id` field exists, but host code does not carry it through consistently.

This must be fixed before expecting plugins to behave correctly on resume or across multiple live sessions.

### 2. Keep both lifecycle and mutation events

Tau should **not** delete `tool_execution_start` / `tool_execution_end` just because `before_tool_exec` / `after_tool_exec` exist.

These are different layers:

- `tool_execution_*` = observability / lifecycle
- `before_*/after_*` = mutation and control

### 3. Permission gates belong on `before_tool_exec`

The correct model for permission gating is:

1. plugin inspects the pending tool call
2. plugin optionally asks the user for approval
3. plugin blocks execution if denied
4. Tau emits a synthetic error tool result using the plugin's reason

This preserves the default app behavior while enabling optional safety plugins.

### 4. Message mutations must affect the current request

If Tau emits a `context` event and accepts injected/removed messages, those mutations must apply to the local request state for the imminent LLM call, not merely to persisted session state.

### 5. Plugin merge order must be deterministic

Override fields such as `modified_tool_arguments`, `modified_tool_result`, `modified_model_id`, and `add_headers` should merge in explicit plugin order, not Go map iteration order.

## Recommended implementation order

1. Fix session identity propagation and resume correctness
2. Make plugin dispatch/merge order deterministic
3. Populate `before_llm_call` fully and emit `context`
4. Wire `inject_system_prompt` and `modified_model_id`
5. Implement tool-boundary blocking and modified arguments/result handling
6. Clean up duplicate shims and callback indirection

## Out of scope for this pass

- host-service reverse RPC design
- per-token `message_delta` dispatch
- compaction events before compaction exists
- plugin installer/discovery CLI
- proto cleanup/removal of unused event types
- Log RPC without a real host-facing reverse channel
