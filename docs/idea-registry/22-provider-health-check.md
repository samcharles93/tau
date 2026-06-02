# 22. Provider Health / Connectivity Check

## Status: Not yet planned

### Motivation

When a provider is unreachable (bad URL, expired token, network issue), the error surfaces as a generic "chat request failed" at submit time. A proactive health check at startup would catch config errors early.

### Design

- On session start, send a lightweight request (models list or a minimal completion) to verify connectivity
- If connectivity fails, show a clear error message with troubleshooting guidance
- Non-blocking: if health check fails, still open TUI but show warning banner
- `/health` command to manually re-check

### Implementation

```go
// internal/platform/health.go
type HealthResult struct {
    Reachable bool
    Latency   time.Duration
    Error     string
}

func CheckHealth(ctx context.Context, provider config.ProviderConfig, bearerToken string) HealthResult
```
