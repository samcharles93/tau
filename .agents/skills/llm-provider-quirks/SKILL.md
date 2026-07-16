---
name: llm-provider-quirks
description: This skill should be used when editing provider request/response building in ai-sdk, or when a live call to OpenAI or Anthropic returns an unexpected 400 or missing data - for example "tool calls aren't returning", "reasoning_effort rejected", "temperature not supported", "thinking budget error", "cached tokens not showing up", or anything involving the Responses API, adaptive thinking, or prompt caching. Maintain this file as a living catalog: add a new entry whenever a new wire-format quirk is confirmed live.
user-invocable: true
---

# LLM Provider Quirks

Consult this catalog before editing `provider/openai` or `provider/anthropic` in ai-sdk. Every entry below was confirmed against the real API, not inferred from docs alone. Add a new entry, with the model/date it was found, whenever another quirk surfaces.

## OpenAI

**Chat Completions vs. Responses API routing.** ai-sdk's `provider/openai/openai.go` auto-routes to `POST /responses` instead of `/chat/completions` whenever a request has both tools and a non-`"none"` `reasoning_effort`. The Responses API differs from Chat Completions in several ways:

- `reasoning_effort` must be nested as `{"reasoning": {"effort": "..."}}`, not the flat Chat Completions `reasoning_effort` field. The flat field 400s with "Unsupported parameter: 'reasoning_effort' ... moved to 'reasoning.effort'".
- `tools`/`tool_choice` must be flattened to `{"type":"function","name":...,"description":...,"parameters":...}` - no nested `"function"` object. The Chat Completions shape 400s with "Missing required parameter: 'tools[0].name'".
- Tool calls arrive as top-level output items (`{"type":"function_call","call_id":...,"name":...,"arguments":...}`), not nested inside a `"message"` item's content blocks. Code that only inspects `message.content` for tool calls will see zero tool calls and an empty response.
- `max_tokens` is renamed to `max_output_tokens`.
- `stop`, `temperature`, `top_p` are rejected outright (400) on Responses-API-routed requests - not renamed, genuinely unsupported. Drop them rather than translate them.

**Cached-token visibility.** OpenAI's automatic prefix caching (server-side, triggers above roughly 1024 tokens for a stable, identical prefix) happens silently with no request-side change. Surface it by parsing `prompt_tokens_details.cached_tokens` (Chat Completions) or `input_tokens_details.cached_tokens` (Responses API) from the usage block. Confirmed live: an identical >1024-token system-prompt prefix showed roughly 80% of tokens served from cache on the second call, with zero change to the request.

**Codex can deliver the function name only in the arguments-done event.** Confirmed live against the ChatGPT Codex backend (2026-07-12): a streamed function call may have an `fc_...` item ID and argument deltas while `response.output_item.added` carries no usable function name. `response.function_call_arguments.done` carries the authoritative `name`; consume it to backfill the assembled call. Its `arguments` field is a complete snapshot, not a delta, so do not append it after processing `response.function_call_arguments.delta` events or arguments will be duplicated.

**Responses reasoning summaries can be block arrays.** Confirmed live with `gpt-5.4` (2026-07-12): a non-streaming reasoning output item carries `summary` as an array such as `[{"type":"summary_text","text":"..."}]`, not only as a string. Decoders must accept both forms because older mocks and compatible backends may still emit the string form.

**Responses history text types depend on the message role.** Confirmed live with OpenAI Responses (2026-07-13): user/system history uses `input_text`, but assistant history must use `output_text`. Sending assistant content as `input_text` returns a 400 whose supported values are `output_text` and `refusal` for that content item.

**Ollama's OpenAI-compat endpoint uses a different reasoning field name.** Ollama's `/v1/chat/completions` streams thinking content under `"reasoning"`, not `"reasoning_content"` (DeepSeek's convention on the same generic "openai-compatible" wire shape). ai-sdk's `openai.go` checks both field names since Ollama is served through the shared `openai-compatible` provider class.

**tau's default local-Ollama routing bypasses ai-sdk's native Ollama provider.** `internal/providers/catalog.go`'s built-in `"ollama"` catalog entry sets no `Class`, so it resolves through `resolveProviderClass` to the generic `"openai-compatible"` runtime class - ai-sdk's `provider/openai.Provider` pointed at Ollama's `/v1` endpoint - not `provider/ollama` (which talks to the native `/api/chat`). A fix to `provider/ollama/ollama.go` does not reach default zero-config local-Ollama users in tau. Check which class a catalog entry resolves to before assuming a provider-level fix reaches them.

## Anthropic

**Two distinct, model-dependent thinking APIs.** Check the model name before building a `thinking` block:

- Legacy (Sonnet 4.5, Opus 4.5, Opus 4.1, Haiku 4.5, and earlier): `thinking: {"type": "enabled", "budget_tokens": N}`. `budget_tokens` must be strictly less than `max_tokens`.
- Adaptive-only (`claude-sonnet-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-fable-5`, `claude-mythos-5`, `claude-mythos-preview`): `thinking: {"type": "adaptive"}` plus a separate top-level `output_config: {"effort": "low"|"medium"|"high"|"xhigh"|"max"}` field. Sending the legacy shape to one of these models guarantees a 400: `"'thinking.type.enabled' is not supported for this model. Use 'thinking.type.adaptive' and 'output_config.effort'..."`.
- Deprecated-but-functional middle ground (Opus 4.6, Sonnet 4.6): both shapes still work; `budget_tokens` is being phased out. Prefer adaptive for new code targeting these.
- ai-sdk (`provider/anthropic/anthropic.go`, `isAdaptiveOnlyModel`) matches by model-ID prefix, so a dated snapshot (`claude-sonnet-5-20260315`) is still caught correctly. Add a new entry to `adaptiveOnlyModelPrefixes` when Anthropic ships a genuinely new model family.

**temperature/top_p rules differ by generation.** Legacy models reject `temperature`/`top_p` only while thinking is actively enabled (temperature is pinned to 1 server-side in that state). Adaptive-only models reject them on every single request, regardless of whether thinking is active or even mentioned - a model-wide rule, not a thinking-state-conditional one. Use separate gating logic for the two cases; do not reuse one flag for both.

**The default `max_tokens` fallback collided with the default effort budget.** ai-sdk's legacy-path default `max_tokens` (4096) is numerically identical to `"medium"` effort's `budget_tokens` (also 4096). Since the API requires `budget_tokens < max_tokens` strictly, any request that left `MaxTokens` unset and used medium effort was a guaranteed 400. Fix: when the caller left `MaxTokens` unset, grow the implicit value to fit the budget instead of erroring; treat an explicitly-set `MaxTokens` that's still too small as a genuine caller error.

**Prompt caching needs the content-block array form.** A plain string `system` field cannot carry `cache_control`. Use `system: [{"type":"text","text":"...","cache_control":{"type":"ephemeral"}}]`. A cache write costs 1.25x normal input price once; reads cost roughly 0.1x - enable by default for any caller resending an identical system prompt across turns (essentially every agent loop), rather than gating it behind an opt-in flag.

**`input_tokens` usage semantics are the opposite of OpenAI's.** Anthropic's `input_tokens` field excludes `cache_read_input_tokens`/`cache_creation_input_tokens` - they are separate, additive fields (`total_input = cache_read + cache_creation + input_tokens`). OpenAI's `prompt_tokens` is the opposite: a superset that already includes its `cached_tokens` subset. When folding these into one cross-provider `PromptTokens` field, add Anthropic's cache counts in but do not add OpenAI's - they are already counted.
