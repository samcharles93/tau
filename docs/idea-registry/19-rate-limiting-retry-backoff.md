# 19. Rate Limiting and Retry with Backoff

## Status: Not yet planned

### Motivation

The streaming layer (`internal/streaming/openai.go`) makes a single HTTP call and returns the error on failure. Rate limits (HTTP 429) and transient errors (5xx) are not retried. This leads to failed turns on busy providers.

### Design

- Configurable retry policy per provider: `max_retries`, `initial_backoff`, `max_backoff`
- Exponential backoff with jitter for 429 and 5xx responses
- Respect `Retry-After` headers when present
- Show retry status in TUI notification bar
- Config: `retry: { max_retries: 3, initial_backoff: 1s, max_backoff: 30s }`
