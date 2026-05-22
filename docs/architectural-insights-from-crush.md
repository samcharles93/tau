# Analysis: Glamour Usage & Architectural Lessons from Crush

---

## 1. Glamour Rendering — Current Issues in aim

Your current `renderMarkdown()` in markdown.go has three problems relative to how Crush does it:

| Issue | aim (current) | Crush (reference) |
| ----- | ------------- | ----------------- |
| **Renderer allocation per frame** | `glamour.NewTermRenderer()` called on every `renderTurn` | Renderers are **memoized per width** in a cache map — allocated once, reused forever |
| **No custom style config** | Uses `glamour.WithStylePath("dark")` (a built-in preset) | Passes a fully custom `ansi.StyleConfig` via `glamour.WithStyles(sty.Markdown)` — fine-grained control over headings, code blocks, lists, etc. tied to the brand palette |
| **No streaming optimisation** | Streaming content is rendered as plain text (`t.StreamingText.Width(...).Render(content)`) so you never see markdown formatting during streaming, only after the turn completes | Uses `streamingMarkdown` — a stable-prefix caching algorithm that incrementally glamour-renders the stream so the user sees **formatted markdown mid-stream** without re-rendering the entire document each flush |

There's also no chroma syntax-highlighting registration — Crush registers a custom chroma formatter for code blocks.

---

## 2. Architectural Ideas Worth Adopting from Crush

### A. **Renderer Cache (high-impact, low-effort)**

Replace the per-call `glamour.NewTermRenderer(...)` with a width-keyed singleton cache, and invalidate on theme change. This alone removes per-turn allocation overhead.

### B. **Custom `ansi.StyleConfig` in the theme package**

Instead of `glamour.WithStylePath("dark")`, define an `ansi.StyleConfig` struct inside theme that maps your brand colours to heading, code-block, blockquote, strong, emphasis, link, and list styles. Pass it via `glamour.WithStyles(...)`. This keeps the "no local colour literals" rule and gives you consistent markdown rendering.

### C. **Incremental Streaming Markdown (`streamingMarkdown`)**

Crush's biggest render-perf win. The algorithm:

1. Find a "safe markdown boundary" (blank line after which no construct is open — no fences, lists, tables, blockquotes).
2. Glamour-render content up to that boundary once, cache the output.
3. On each streaming delta that extends the prefix, only re-render the trailing partial and concatenate.
4. Fall back to a full render whenever the boundary detection has any doubt.

This gives formatted output mid-stream without O(n²) re-rendering.

### D. **Versioned Item + List-level Render Cache**

Crush's `list.Item` interface embeds a `Versioned` counter. The `List` component caches each item's rendered output and only re-renders on version bumps or width changes. "Finished" items are frozen (zero cost on redraw). This is far more scalable than aim's current model where `renderConversation` re-renders all turns when a width/cache mismatch is detected.

### E. **Per-section Sub-caches with FNV-64 Keying**

The `AssistantMessageItem` splits its output into three independently-cacheable sections (thinking, content, error). Only the section whose source hash changes is re-rendered. This prevents expensive thinking-block re-renders when only content is streaming.

### F. **Pubsub Delivery Semantics (lossy vs. must-deliver)**

Crush's broker distinguishes:

- `Publish()` — non-blocking, drops on full buffer (fine for streaming deltas)
- `PublishMustDeliver()` — bounded-blocking with per-subscriber timeout (for terminal events: finish, error, cancel)

aim's `pubsub.Bus` currently has a single delivery mode. Adding a must-deliver path prevents "missed finish" bugs where the TUI hangs after a saturated stream.

### G. **Message Service with Debounced Persistence**

Crush's `message.Service` coalesces rapid streaming updates into a single SQLite write per debounce window (33ms), while terminal-state updates flush synchronously. This is a good model for when aim adds store persistence — don't flush every delta.

### H. **Common Shared Dependencies Pattern**

Crush uses a `common.Common` struct that bundles `Workspace + Styles` and is passed down through the UI tree. This avoids ambient globals and makes it straightforward to support multiple themes or workspaces. aim's `tuiTheme` is a lighter version of this; when complexity grows, centralising `theme + config + services` into a single context struct keeps wiring clean.

### I. **Hooks System (PreToolUse)**

Crush's `hooks` package lets users define shell commands that fire before tool execution and return allow/deny/halt decisions. This is architecturally relevant for aim if/when agent tool-use is added — it provides a user permission layer without coupling to the UI.

---

## 3. Recommended Implementation Plan

| Priority | Change | Scope | Risk |
| ---------- | -------- | ------- | ------ |
| **P0** | Renderer cache — memoize `*glamour.TermRenderer` per width | markdown.go | Low — drop-in, zero API change |
| **P0** | Custom `ansi.StyleConfig` in theme instead of `"dark"` preset | theme.go + markdown.go | Low — defines brand markdown colours |
| **P1** | Streaming markdown rendering during stream (replace plain-text fallback) | New `internal/tui/streaming_markdown.go` | Medium — needs boundary-detection logic |
| **P1** | Versioned turn cache with freeze semantics | render.go | Medium — refactor `turnBlock` to embed a version counter |
| **P2** | Must-deliver pubsub path for terminal events | bus.go | Low — additive |
| **P2** | Per-renderer mutex (concurrency safety) | markdown.go | Low — only matters if tests or background workers run parallel renders |
| **P3** | Hooks-style permission layer for future tools | New `internal/hooks/` | Future — when agent tools land |
| **P3** | Debounced persistence layer in store | store | Future — when SQLite lands |

---

## 4. Specific Next Steps (Executable)

1. **Fix the hot path now**: Add a `var mdCache sync.Map` (or `map[int]*glamour.TermRenderer` + mutex) in markdown.go. Stop allocating a new renderer per call.

2. **Define `theme.MarkdownStyle() ansi.StyleConfig`** in theme.go using your brand colours for headings (navy), code blocks (dim background), links (purple), etc.

3. **Port `streamingMarkdown`** — the streaming_markdown.go file is ~200 lines of algorithm + ~150 lines of boundary detection. Adapt it to use your existing `renderMarkdown` as the underlying renderer.

4. **Update `renderAssistantStreaming`** to use the new incremental renderer instead of plain lipgloss text styling — this gives users formatted markdown as it streams in.

Completed: *Produce implementation plan* (4/4)

The biggest immediate win is **P0: stop allocating a new glamour renderer per render call** - that's a single-file change.
