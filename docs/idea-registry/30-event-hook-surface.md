# 62. Rich Event Hook Surface — Complete Lifecycle Coverage

## Status: Design — based on extension-contract-spec + Pi comparison matrix

### Motivation

The current plugin DispatchEvent carries only session_start/session_shutdown as bare map[string]string. Pi has 24 typed events. The user's own extension contract spec defines 10 lifecycle events with typed payloads and response transforms. The gap is massive — real plugins (compliance, model router, PII redactor) need to observe and modify every phase of the agent lifecycle.

### Design: Typed Event Payloads + Response Transforms

Instead of string maps, events carry typed proto oneof payloads. Modifying events return EventResponse that the coordinator merges and applies.

### Event Map

| Event | Fires | Plugin Can |
| ----- | ----- | ---------- |
| session_start | New session | Init state |
| before_agent_start | Agent loop starting | Inject system prompt |
| turn_start | Each turn | Track stats |
| context | Before LLM context build | Inject/remove messages |
| before_llm_call | Before API request | Modify payload, headers, model |
| after_llm_call | After API response | Log/intercept response |
| message_start | Assistant message begins | Track lifecycle |
| message_delta | Each token | Real-time PII redact, translate |
| message_end | Message complete | Archive, analyze |
| tool_execution_start/update/end | Tool execution lifecycle | Log, stream progress |
| before_tool_exec | Tool call dispatched | Validate, block, modify args |
| after_tool_exec | Tool completed | Transform/annotate result |
| turn_end | Turn complete | Persist, trigger actions |
| before_compact/after_compact | Compaction | Customize/verify |
| session_end | Session closing | Flush, persist |

### Response Merging

When multiple plugins respond to the same event, the manager merges:

- InjectMessages: concatenated from all plugins
- RemoveMessageIndices: union of all
- InjectSystemPrompt: newline-joined
- BlockToolExecution: first block wins
- AddHeaders: last plugin wins per key
- Diagnostics: accumulated from all

### Coordinator Integration

The coordinator already has hook points (OnSessionStart, OnToolStarted, etc.) as func(map[string]any). These are widened to carry typed payloads and return responses. The plugin manager's DispatchEvent returns *EventResponse.

### Priority

1. Add typed event payloads to proto (EventPayload with oneof, EventResponse)
2. Update DispatchEvent to carry EventPayload and return EventResponse
3. Wire coordinator to fire events at turn/tool/LLM boundaries
4. Implement response merging in plugin manager
