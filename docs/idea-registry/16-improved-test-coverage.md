# 16. Improved Test Coverage (Quality)

## Status: Ongoing

### Current state (2026-06-02)

| Package | Coverage | Tests |
| ------- | -------- | ----- |
| `config` | 80.9% | ✅ |
| `extensions` | 79.1% | ✅ |
| `provider` | 73.5% | ✅ |
| `pubsub` | 70.9% | ✅ |
| `agent` | 68.8% | ✅ |
| `skills` | 67.8% | ✅ |
| `chat/commands` | 66.7% | ✅ |
| `streaming` | 64.8% | ✅ |
| `chat` | 57.2% | ⚠ |
| `agent/tools` | 42.3% | ⚠ |
| `tui` | 28.5% | ❌ |
| `app` | 3.9% | ❌ |
| `platform` | 6.7% | ❌ |
| `cli` | 0.0% | ❌ |
| `theme` | 0.0% | ❌ |
| `store` | N/A (stub) | ❌ |
| `tui/components` | 0.0% | ❌ |
| `tui/layouts` | 0.0% | ❌ |
| `tui/notify` | 0.0% | ❌ |
| `tui/views` | 0.0% | ❌ |
| `agent/notify` | N/A | ❌ |

### Priority improvements

1. **`app` (3.9%)** — The orchestration layer has almost no coverage. Test `RunChat` with mock runtimes, token resolution, and model selection.
2. **`tui` (28.5%)** — The chat panel is the most complex component. Add tests for slash command dispatch, event handling, autocomplete matching, and state transitions.
3. **`agent/tools` (42.3%)** — Tool execution is security-critical (shell, write, edit). Add integration tests with real temp directories.
4. **`platform` (6.7%)** — Auth flow and HTTP client need tests with mock HTTP servers.
5. **`tui/components` and `tui/views` (0%)** — All component rendering and the debug system need at least basic smoke tests.
