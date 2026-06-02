# 15. Prompt Caching (Anthropic-style)

## Status: Not yet planned

### Motivation

Many providers (Anthropic, DeepSeek, OpenAI) support prompt caching to reduce latency and cost for repeated system prompts and tool definitions. Tau already sends large system prompts and tool definitions on every request — caching would yield immediate cost/latency wins.

### Design

- Add `cache_control` breakpoints in the request body for system prompt and tool definitions
- Configurable via model compat: `supports_prompt_caching: true`
- `ChatUsage` already has `CacheRead` and `CacheWrite` fields — surface them
- Track cache hit ratio in session metadata
- Show cache savings in status bar (`Cache: 95% hit`)
