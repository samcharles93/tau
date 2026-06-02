# Design Ideas & Architecture Decisions

## 1. Inline-Mode & Inline Streaming Migration

**Date:** 2026-06-02

### Decision
Migrate the TUI from full-screen (alternate buffer) mode to inline-mode with inline streaming using go-tui's `StreamAbove` / `PrintAbove` APIs.

### Architecture

**Mode:** `gt.WithInlineHeight(3)` — inline widget stays in terminal scrollback.

**Widget layout (dynamic height, 3–10 lines):**
- TextArea for input (grows via `SetInlineHeight(n)` as content adds lines)
- Status bar (model, status, notices, completions)
- Base 3 lines, grows up to 10 via `app.SetInlineHeight(n)` when textarea needs space, tool results, errors, forms, or lists grow to their required height
- Shrinks back to 3 when textarea is cleared

**Output flow:**
- **Streaming:** `app.StreamAbove()` → `StreamWriter` — write deltas character-by-character as they arrive from the LLM. Uses `WriteGradient` or `WriteStyled` for styling.
- **Completed messages:** `app.PrintAboveln()` / `app.PrintAboveStyledln()` — render final messages to scrollback above widget.
- **Tool output, errors, notifications:** `app.PrintAboveln()` — printed in arrival order, interleaved with chat messages.
- **Extension command results:** `app.PrintAboveln()` — printed where they arrive.

**Screen switching:**
- Settings modal → `app.EnterAlternateScreen()` / `app.ExitAlternateScreen()`
- Session list, session info, debug views stay as compact inline overlays (not fullscreen)

**Input:**
- Replace custom `chatInput` (single-line) with `tui.TextArea` (multi-line)
- Enter submits, Ctrl+J inserts newline
- Tab completion shown in status bar, handled via global key handler
- `Height()` method drives `SetInlineHeight(n)` via value watcher

### Implementation Plan

1. **`run.go`** — Add `gt.WithInlineHeight(3)` to `NewApp`
2. **`chatui.go`** — Major restructure:
   - Add `streamWriter`, `inlineTextarea` fields
   - `handleRuntimeEvent`: route completed msg → `PrintAboveln`, streaming → `StreamWriter`, notifications → `PrintAboveln`
   - Simplify `Render()` to just textarea + compact status bar
   - Add dynamic height management via textarea value watcher
   - Settings modal triggers `EnterAlternateScreen`/`ExitAlternateScreen`
   - Remove `renderHeader`, `renderMessages`, `renderMessage`, `renderStreamingContent`, `renderCompletions`
   - Keep `renderCompletions` as compact inline overlay or status-bar display
3. **`input.go`** — Replace `chatInput` with thin wrapper or use `tui.TextArea` directly
4. Remove `renderHeader` from element tree

### Rationale

- Inline mode keeps the app in the terminal scrollback, making it feel native to the terminal
- PrintAbove and StreamAbove handle output placement into scrollback
- Dynamic height keeps the widget compact when idle, grows when needed
- Full-screen mode reserved for complex modal views (settings)
