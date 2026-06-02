# 11. Context Window Management and Token Counting

## Status: Not yet planned

### Motivation

The agent coordinator currently has no awareness of context window limits. As conversations grow, the LLM receives ever-larger message lists, eventually hitting the model's context limit (e.g., 128K tokens). This causes silent truncations or API errors. Tau needs smart context management: accurate token counting, proactive truncation strategies, and visibility for the user.

### Problem

- `ChatSessionState` tracks messages but never counts tokens
- No limit enforcement before sending requests
- `max_tokens` controls output only, not total context
- The `CostConfig` in config exists but is unused at runtime
- No `tiktoken` or equivalent tokenizer integration

### Design

**Token counting layer** (`internal/chat/tokens.go`):

- `TokenCounter` interface with `CountTokens(messages []ChatMessage) int`
- `TiktokenCounter` implementation wrapping `github.com/pkoukk/tiktoken-go`
- `SimpleCounter` fallback using character/word estimation (~4 chars/token)
- Selectable via config: `token_counter: tiktoken|simple`

**Context window awareness** (`ChatSessionState`):

- Add `ContextLimit int` field loaded from `ModelConfig.ContextWindow`
- Add `TokenCount int` field tracking current message token total (lazily recomputed)
- `EstimateRequestTokens() int` for the full request with system prompt + tools
- `RemainingTokens() int` for user visibility

**Truncation strategies** (`internal/chat/truncation.go`):

- `TruncationStrategy` enum: `keep_last_n`, `summarize_early`, `sliding_window`
- `KeepLastN(n int)` — keep the most recent N messages, drop older
- `SummarizeEarly` — summarize early conversation into a single system message, keep recent
- `SlidingWindow` — keep N tokens worth of messages, dropping oldest first
- Strategy selectable via config or `/context` slash command

**Status bar visibility**:

- Show `Tokens: 4,200 / 128,000` in the status bar
- Color-code: green (<50%), yellow (50-80%), red (>80%)
- `/context` command to view detailed breakdown and change strategy

### Files to create/modify

| File | Change |
| ---- | ------ |
| `internal/chat/tokens.go` | New: TokenCounter interface + tiktoken/simple impls |
| `internal/chat/truncation.go` | New: TruncationStrategy + implementations |
| `internal/chat/types.go` | Add ContextLimit, TokenCount to ChatSessionState |
| `internal/tui/chatui.go` | Show token count in status bar; add `/context` command |
| `internal/config/config.go` | Add token_counter and truncation_strategy config fields |
| `internal/agent/coordinator.go` | Apply truncation before building request messages |
