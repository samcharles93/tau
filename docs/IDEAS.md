## 7. Component Debug System (`/debug components`)

### Idea
A `/debug components [NAME]` slash command that lets the developer (end-user) preview and test individual go-tui components in isolation. Without a name, it lists all registered components. With a name, it opens a full-screen debug view showing that component with controllable test state.

### Implementation sketch

**`internal/tui/components/registry.go`** — a global registry mapping component names to test constructors:
```go
type DebugFactory func(app *gt.App) (gt.Component, []debugControl)

type debugControl struct {
    Label string
    State *gt.State[string]  // or interface for bool/int/string
    Type  string              // "toggle", "cycle", "text"
}
```
Each component registers itself via `init()` or a `Register(name string, factory DebugFactory)` call. The registry lives in the `components` package so components can self-register.

**`internal/tui/views/debug.go`** — the debug view:
- Opens as a full-screen view (via `/debug components toggle`)
- Left half: the component under test, rendered with test state
- Right half: control panel showing each `debugControl` with buttons/sliders to mutate state
- Footer: component name + "Esc to close"
- Keymap: Esc dismisses, Tab cycles controls

**`/debug components` (no name)** — opens a `Modal` layout containing a `List` component showing all registered component names. Enter opens the debug view for the selected one.

**Why this matters:** With GSX components, you can't just `go run` a single component in isolation — they need an app context, state binding, and the full go-tui lifecycle. The debug view provides that scaffolding so you can iterate on a component's look-and-feel without launching the full chat TUI.

---

## 8. Initial Component Set (Settings Prerequisites)

### Idea
Before building the settings view, build the component primitives it needs. Order matters: bottom-up, each component tested via `/debug components` before the next depends on it.

### Implementation order

| # | Component | GSX file | Shadcn inspiration | What it is |
|---|-----------|----------|-------------------|-------------|
| 1 | `Select` | `components/select.gsx` | `select.tsx` + `native-select.tsx` | Horizontal option cycler: `‹ model-a ›` with Left/Right arrows, Enter to confirm. Exposes `State[string]`, `Options []string`, `OnChange`. |
| 2 | `Toggle` | `components/toggle.gsx` | `switch.tsx` | Boolean toggle: `[X] Show reasoning` / `[ ] Show reasoning`. Exposes `State[bool]`, `Label string`, `OnChange`. |
| 3 | `List` | `components/list.gsx` | `command.tsx` (item list portion) | Scrollable selectable list: renders `[]Item` with highlighted selection, Up/Down navigation, Enter to select. Exposes `State[[]Item]`, `State[int]` selected index, `OnSelect`. |
| 4 | `Modal` | `layouts/modal.gsx` | `dialog.tsx` | Full-screen backdrop overlay with focus trap, Esc-to-close. Exposes `State[bool]` open/close. Renders a single child component slot. |
| 5 | `Form` | `layouts/form.gsx` | `field.tsx` | Label+control pairs with Tab/Shift+Tab navigation. Exposes `[]FormField` where each field has `Label`, `Description`, and a child component. |

### Component contracts (Go types)

```go
// internal/tui/components/select.go (hand-written, not GSX — just types)
package components

type SelectOption struct {
    Value string
    Label string
}

// Select is a GSX component. Its Go-side type is:
// type Select struct {
//     Value   *gt.State[string]
//     Options []SelectOption
//     OnChange func(string)
// }
```

```go
// internal/tui/components/toggle.go
package components

// Toggle is a GSX component:
// type Toggle struct {
//     Checked  *gt.State[bool]
//     Label    string
//     OnChange func(bool)
// }
```

```go
// internal/tui/components/list.go
package components

type ListItem struct {
    ID          string
    Label       string
    Description string
    Disabled    bool
}

// List GSX component:
// type List struct {
//     Items    *gt.State[[]ListItem]
//     Selected *gt.State[int]
//     OnSelect func(ListItem)
// }
```

```go
// internal/tui/layouts/modal.go
package layouts

// Modal GSX component:
// type Modal struct {
//     Open    *gt.State[bool]
//     Title   string
//     Width   int  // 0 = auto
//     Height  int  // 0 = auto
// }
// Child content is added via GSX children: <Modal><Content /></Modal>
```

```go
// internal/tui/layouts/form.go
package layouts

type FormField struct {
    Label       string
    Description string
    Control     gt.Component  // the child component (Select, Toggle, etc.)
}

// Form GSX component:
// type Form struct {
//     Fields []FormField
// }
```

### After components are built: the Settings view

`internal/tui/views/settings.go` composes:
```
Modal(Open: showSettings)
  └─ Form(Fields: [
       {Label: "Model", Control: Select(Options: models, Value: currentModel)},
       {Label: "Reasoning", Control: Toggle(Checked: showReasoning)},
     ])
```

The view file is ~40 lines of wiring. All rendering, focus, and keyboard behavior lives in the components/layouts below it.

### Debug integration
Each component registers in `init()`:
```go
// In components/select.gsx (or a companion select.go)
func init() {
    Register("select", func(app *gt.App) (gt.Component, []debugControl) {
        value := gt.NewStateForApp(app, "model-a")
        comp := &Select{
            Value:   value,
            Options: []SelectOption{{"model-a", "Model A"}, {"model-b", "Model B"}},
        }
        return comp, []debugControl{
            {Label: "value", State: typedState(value), Type: "cycle", Options: []string{"model-a", "model-b"}},
        }
    })
}
```

Then `tau` → `/debug components` → select "select" from the list → full-screen debug view opens with the Select component cycling through test models and the control panel on the right showing the current state.

---

## 9. Automated Vulnerability Scanning and Issue Creation

### Status: Implemented
Vulnerability scanning was introduced into the GitHub Actions workflows (under `.github/workflows/vuln-check.yml`), which runs daily and raises GitHub issues when code-level vulnerabilities are identified.

---

## 10. Session Persistence with `/session` Command

### Status: Planned (MUST HAVE)

### Motivation
Session persistence is the single biggest UX gap (see gap analysis UX #1). Conversations vanish on exit with no way to resume, replay, export, or audit past sessions. Both `internal/store` (stub) and the session state machine exist — what's missing is the wiring and the TUI surface.

### Command Design

Two entry points with different affordances:

- **`/resume`** — fast path: opens an interactive session picker directly (list + fuzzy filter). No sub-commands.
- **`/session`** — full session management with sub-commands. Also opens the session list when used bare.

### Sub-commands

| Command | Description |
|---------|-------------|
| `/session` (bare) | Open interactive session list (paginated, sortable by date/model/tokens) |
| `/session export [id]` | Export session as JSONL to stdout or file |
| `/session exportHTML [id]` | Export as a standalone HTML page with the full TUI-rendered conversation (similar to Pi export) |
| `/session delete [id]` | Delete a session (with confirmation) |
| `/session info [id]` | Print session metadata to stdout in a formatted table |

### Picker UX

When the user runs `/session` or `/resume` in the TUI, a modal opens showing:
- Most recent 10 sessions (paginated)
- Each row: date, model, message count, token total, cost
- Up/Down to navigate, Enter to select, fuzzy filter via typing
- Selected session loads and replaces current conversation (with confirmation if unsaved changes)

### `/session info` Output Format

Inspired by Pi's session info output, adapted for Tau:

```shell
Session Info

File: /home/sam/.tau/sessions/2026-05-31T07-40-16-496Z_019e7cf9-f4f0-7270-8a63-6b1975803183.jsonl

ID: 019e7cf9-f4f0-7270-8a63-6b1975803183

Messages
User:       1
Assistant:  10
Tool Calls: 27
Tool Results: 27
Total:      38

Tokens
Input:      92,785
Output:     5,328
Cache Read: 592,256
Total:      690,369

Cost
Total:      $0.0161
```

### Implementation sketch

**Storage layer** (`internal/store/`):
- Use the pre-existing `store` package (currently a stub with only `doc.go`).
- SQLite via `sqlc` + migrations for session metadata (id, model, provider, created_at, total_tokens, cost).
- JSONL files on disk for full message history (append-only, easy to read/externalize).
- Storage path: `~/.tau/sessions/` (configurable via env).

**Chat layer changes** (`internal/chat/`):
- After a turn completes (`ChatResponseCompletedEvent`), the runtime persists the complete message list to the JSONL file.
- On session close, persist a final summary row to SQLite (token counts, cost, model, duration).
- Add a `ListSessions(limit, offset)` method and a `LoadSession(id)` method to the runtime interface.

**TUI changes** (`internal/tui/`):
- `/session` and `/resume` slash commands added to the handler.
- `SessionListPanel` — a scrollable list view with inline stats per row, similar to the existing `DebugListView` but data-driven.
- `SessionInfoPanel` — renders the formatted table block shown above in a modal.
- Loaded session replaces the current `messages` state, with a confirmation dialog if the current session has un-persisted changes.

**CLI changes** (`internal/cli/`):
- `tau sessions` — list recent sessions in a table.
- `tau sessions export <id>` — export to JSONL.
- `tau sessions resume <id>` — launch TUI with a pre-loaded session.

### Files to create/modify

| File | Change |
|------|--------|
| `internal/store/schema.sql` | New: SQLite schema for sessions table |
| `internal/store/queries.sql` | New: CRUD queries for session metadata |
| `internal/store/session.go` | New: SessionStore interface + SQLite impl |
| `internal/chat/types.go` | Add `SessionSummary` type (metadata without full messages) |
| `internal/chat/runtime.go` | Add `ListSessions()` and `LoadSession()` methods |
| `internal/tui/chatui.go` | Add `/session`, `/resume` command handlers |
| `internal/tui/views/session_list.go` | New: session picker modal |
| `internal/tui/views/session_info.go` | New: session info display |
| `internal/cli/commands.go` | Add `tau sessions` sub-command |
| `internal/app/chat.go` | Wire session persistence into coordinator startup/teardown |

### Open Questions

1. Should auto-save happen after every turn (real-time) or only on graceful exit? Real-time is safer against crashes; on-exit is simpler.
2. Cost tracking requires model pricing data — should this come from config (`CostConfig`) or from an external pricing API?

---

## 11. Context Window Management and Token Counting

### Status: Not yet planned

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
|------|--------|
| `internal/chat/tokens.go` | New: TokenCounter interface + tiktoken/simple impls |
| `internal/chat/truncation.go` | New: TruncationStrategy + implementations |
| `internal/chat/types.go` | Add ContextLimit, TokenCount to ChatSessionState |
| `internal/tui/chatui.go` | Show token count in status bar; add `/context` command |
| `internal/config/config.go` | Add token_counter and truncation_strategy config fields |
| `internal/agent/coordinator.go` | Apply truncation before building request messages |

---

## 12. Conversation Branching (`/branch`)

### Status: Not yet planned

### Motivation

Users often want to explore a different direction from a conversation fork point — try a different prompt, experiment with an alternative approach, or redo a response. Currently the only option is `/new` to start over completely.

### Design

- Each branch is a separate message history sharing a common prefix
- `/branch` creates a branch at the current conversation state
- `/branch list` shows all branches in the current session
- `/branch switch <id>` switches to a different branch
- Visual indicator in the TUI showing branch depth / position
- Branches persisted in the session JSONL with a `branch_id` field per message

### Storage

- JSONL stores `branch_id` alongside each message
- Session metadata tracks `current_branch_id` and `branch_ids` list
- Branches share common messages (by reference, not duplication)

---

## 13. Message Editing and Regeneration

### Status: Not yet planned

### Motivation

Users frequently want to edit a previous message and regenerate the assistant's response. This is standard in ChatGPT, Claude, and other chat UIs. Tau currently has no way to edit or retry.

### Design

**Edit previous user message**:
- `/edit <message_index>` opens an inline editor at the specified message
- After editing and pressing Enter, the conversation is truncated to just before the edited message
- The edited message is submitted as a new prompt, generating a fresh response

**Regenerate last response**:
- `/retry` or `/regenerate` command
- Removes the last assistant message and resubmits the previous user prompt
- Works with Ctrl+R keyboard shortcut when idle

**Implementation**:
- `ChatSessionState.TruncateTo(index int)` — removes all messages after the given index
- `ChatSessionState.ReplaceMessage(index int, content string)` — replaces a message at index
- New command: `RetryLastPromptCommand`
- New command: `EditMessageCommand{Index int, NewContent string}`

---

## 14. Config Validation Command (`tau config validate`)

### Status: Not yet planned

### Motivation

Users configure providers in YAML and currently discover errors only at startup. A `tau config validate` CLI command would let users verify their config without launching a full session, and could be used in CI/CD or pre-commit hooks.

### Design

```
tau config validate          # validate global + project config
tau config validate --global # validate global only
tau config validate --json   # output as JSON with field-level errors
tau config path              # print config file paths being used
```

Output:
```
✓ global config: /home/user/.config/tau/config.yaml
✓ project config: /work/apps/tau/.tau.yaml
✓ provider "anthropic": valid (oauth_pkce)
⚠ provider "openai": api_key_env MYSECRET_API_KEY is empty or unset
✓ 2 providers, 0 errors, 1 warning
```

---

## 15. Prompt Caching (Anthropic-style)

### Status: Not yet planned

### Motivation

Many providers (Anthropic, DeepSeek, OpenAI) support prompt caching to reduce latency and cost for repeated system prompts and tool definitions. Tau already sends large system prompts and tool definitions on every request — caching would yield immediate cost/latency wins.

### Design

- Add `cache_control` breakpoints in the request body for system prompt and tool definitions
- Configurable via model compat: `supports_prompt_caching: true`
- `ChatUsage` already has `CacheRead` and `CacheWrite` fields — surface them
- Track cache hit ratio in session metadata
- Show cache savings in status bar (`Cache: 95% hit`)

---

## 16. Improved Test Coverage (Quality)

### Status: Ongoing

### Current state (2026-06-02)

| Package | Coverage | Tests |
|---------|----------|-------|
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

---

## 17. Input History Persistence

### Status: Not yet planned

### Motivation

Shell-like input history (up/down arrow to recall previous prompts) is standard in chat UIs. Currently pressing Up/Down scrolls the message view, but there's no way to recall previous inputs.

### Design

- `ChatPanel` tracks a ring buffer of recent inputs (`[]string`, max 100)
- Up arrow in empty input: recall previous prompt
- Down arrow: move forward through history
- History persists across sessions in `~/.tau/history.jsonl`
- `/history` command to view/search input history

---

## 18. Multi-Provider Switching in TUI

### Status: Not yet planned

### Motivation

Users configure multiple providers (OpenAI, Anthropic, local Ollama) but can only use one per session. Switching requires editing config or restarting with `--provider`. The TUI should allow on-the-fly provider switching.

### Design

- `/provider <name>` slash command to switch providers
- Provider list in Settings modal with current provider highlighted
- On switch: create a new coordinator session or reconfigure the existing one
- Model list refreshes automatically for the new provider
- Provider name shown in header and status bar (already partially done)

### Challenges

- Auth token may need re-resolution for the new provider
- Different providers may have different model APIs and compat configs
- The streaming layer needs to handle provider-specific quirks

---

## 19. Rate Limiting and Retry with Backoff

### Status: Not yet planned

### Motivation

The streaming layer (`internal/streaming/openai.go`) makes a single HTTP call and returns the error on failure. Rate limits (HTTP 429) and transient errors (5xx) are not retried. This leads to failed turns on busy providers.

### Design

- Configurable retry policy per provider: `max_retries`, `initial_backoff`, `max_backoff`
- Exponential backoff with jitter for 429 and 5xx responses
- Respect `Retry-After` headers when present
- Show retry status in TUI notification bar
- Config: `retry: { max_retries: 3, initial_backoff: 1s, max_backoff: 30s }`

---

## 20. Conversation Search (`/find`)

### Status: Not yet planned

### Motivation

Long conversations with the agent can span dozens of messages. Users need a way to search within the current conversation for specific topics, files mentioned, or past results.

### Design

- `/find <query>` searches current conversation messages
- `/find /pattern/` for regex search
- Results shown in a modal list with context snippets
- Enter jumps to that message in the scroll view
- Search scope: current session only (across-branch search later)

---

## 21. Clipboard Integration

### Status: Not yet planned

### Motivation

Copying model responses or tool outputs currently requires terminal selection (mouse + Shift). A keyboard shortcut to copy the last assistant response would improve UX significantly.

### Design

- `Ctrl+Y` copies last assistant message to system clipboard
- Uses `github.com/atotto/clipboard` or platform-specific shell commands
- `/copy` slash command with optional index: `/copy 3` copies message #3
- Notification: "Copied 1,234 characters to clipboard"

---

## 22. Provider Health / Connectivity Check

### Status: Not yet planned

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

---

## 23. Skill Installation via CLI

### Status: Not yet planned

### Motivation

Skills are discovered from filesystem paths but there's no `tau skill install` command to fetch skills from remote sources (GitHub repos, URLs, or a skill registry).

### Design

```
tau skill install <source>          # install from URL or GitHub path
tau skill install gh:user/repo      # shorthand for GitHub
tau skill install ./path/to/skill   # local path
tau skill list                      # list installed skills with status
tau skill remove <name>             # uninstall a skill
tau skill update [name]             # update skill(s)
```

- Skills installed to `~/.tau/skills/` (user) or `.tau/skills/` (project)
- Metadata stored alongside skill for update tracking (source URL, version, installed_at)
- `tau.yaml` skill manifest for version/update info

### Open question

Should skills be versioned (git tags) or always latest? A lockfile (`tau-skills.lock`) similar to npm/pip would pin versions.

---

## 24. Compact Token Usage / Cost Dashboard

### Status: Not yet planned

### Motivation

Users care about cost but have no visibility into cumulative spending. Tau should track total usage across sessions and show a simple dashboard.

### Design

- `~/.tau/usage.json` accumulates token counts and costs across sessions
- `/usage` command: shows current session + all-time stats
- Weekly/monthly breakdowns
- Configurable spending alerts (`max_monthly_spend: $50`)
- `tau usage` CLI command for terminal dashboard

### Output format

```
Usage Summary

This session:  $0.0123  |  45,230 tokens  |  12 messages
Today:         $0.0456  |  180,450 tokens  |  48 messages
This month:    $1.2345  |  4.5M tokens     |  1,200 messages
All time:      $12.3456 |  45M tokens      |  12,000 messages
```

---

## 25. Lua Extension: Runtime Performance & Sandboxing

### Status: Not yet planned

### Motivation

Extensions currently run in-process via gopher-lua with no CPU/memory limits. A misbehaving extension can hang or crash Tau. Additionally, there's no hot-reload — changes require `/reload`.

### Design

**Resource limits**:
- Per-call timeout via Lua `debug.sethook` instruction counting
- Memory budget (VM-level allocation tracking)
- Enforce in `lua_host.go` before each `Call`

**File watching**:
- Watch extension directories with `fsnotify`
- Auto-reload changed extensions with notification
- Configurable: `extensions.auto_reload: true`

**Sandbox hardening**:
- Audit gopher-lua module allowlist — ensure `os.execute`, `io.open`, etc. are disabled or restricted
- Path allowlist for file operations
- Extension manifest declares required permissions: `permissions: [read, write, shell]`

---

## 26. Tab Completion for File Paths and Tool Arguments

### Status: Not yet planned

### Motivation

Slash commands have autocomplete, but file paths and tool arguments in free-form text do not. Users typing `write file:./src/` should get filesystem path completion.

### Design

- Detect file path patterns in input text (words starting with `./`, `/`, `~/`)
- Tab completion shows matching files/directories from the current working directory
- Works in both slash command arguments and free-text prompts
- Priority: slash command completions first, then path completions

---

## 27. Multi-Line Input with Paste Buffering

### Status: Not yet planned (MUST HAVE)

### Motivation

Single-line input is cramped for composing long prompts. Users need `Shift+Enter` / `Ctrl+J` for newlines. Large pastes should replace inline content with a placeholder `[paste #1 +90 lines]` — the full text is buffered in memory and sent as plaintext when the user submits, but the input area stays readable.

Advanced idea (inspired by GitHub Copilot): paste large content to a temp file, then let the agent selectively view or search it via FTS5/BM25 rather than dumping the entire file into context.

---

## 28. Input History: Scroll Between Previous Prompts

### Status: Not yet planned

### Motivation

No way to recall previous prompts. Pressing `Up Arrow` should scroll through input history line-by-line until reaching the top of the current input, then switch to the previous message. Alternately, dedicated keybinds like `Ctrl+UpArrow` / `Ctrl+DownArrow` to jump between full messages directly.

### Design

- Ring buffer of recent inputs (max 100) tracked in `ChatPanel`
- When cursor is at top of current multi-line input, Up steps to previous prompt
- `Ctrl+Up` / `Ctrl+Down` skips directly to previous/next full message
- Persist to `~/.tau/history.jsonl` across sessions

---

## 29. Auto-Scroll Lock During Streaming

### Status: Not yet planned

### Motivation

Scrolling up during streaming is overridden by each new delta. Users cannot read earlier content while the agent is generating. This is a real annoyance — agent harnesses should not force-scroll when the user has intentionally scrolled away.

### Design

- `autoScroll` boolean state: `true` when at bottom, `false` when user scrolls away
- When `autoScroll == false`: deltas are appended to the message view but the scroll position is not forced to bottom; the streaming content pane becomes visually separated from the main message list
- When user scrolls back to bottom: `autoScroll` resets to `true`, panes rejoin
- Needs architectural investigation for split-pane behaviour in go-tui

---

## 30. Inline Tool Call Visualization

### Status: Not yet planned

### Motivation

Tool calls currently appear only as transient text notices in the status bar ("tool started: read /path/to/file") and then vanish. Users cannot see what the agent did, review tool inputs/outputs, or expand/collapse tool call blocks. This makes the agent's decision-making opaque.

### Design

- Each tool call rendered as an expandable block inline in the message list
- Collapsed state: one-line summary — `🔧 read /path/to/file (1.2ms)`
- Expanded state: shows full input arguments and truncated output
- Status indicators: success (green ✓), error (red ✗), truncated (yellow ⚡)
- Toggle with Enter when focused, or click to expand with mouse

---

## 31. Markdown Rendering with Syntax Highlighting

### Status: Not yet planned (upstream PR exists)

### Motivation

Messages are displayed as plain text. Code-heavy responses are hard to read without syntax highlighting, code block formatting, bold/italic, headers, and lists. A chat tool for developers must render code well.

### Design

- go-tui has a pending PR for markdown rendering: https://github.com/grindlemire/go-tui/pull/69
- Once merged: upgrade go-tui, enable markdown rendering on assistant message content
- Use Chroma for syntax highlighting of code blocks
- Graceful fallback: unrecognized languages render as monospace plain text
- Config toggle: `ui.render_markdown: true` (default on)

---

## 32. Code Block Folding

### Status: Not yet planned

### Motivation

Long code blocks in model responses consume lots of vertical space, pushing conversation context out of view.

### Design

- Code blocks longer than N lines (configurable, default 10) are rendered collapsed
- Collapsed state: first 2 lines + "[+45 lines]" fold indicator
- Expand/collapse with Enter or click
- Fold state persists during the current session

---

## 33. Mouse Wheel Scroll Support

### Status: Not yet planned

### Motivation

`gt.WithMouse()` is set on the app but there is no mouse scroll handler. Users expect scroll wheel support in the message list.

### Design

- Add scroll event handler to the messages scrollable container
- Map scroll wheel to `scrollY` state adjustments
- Scroll up/down by configurable line count (default 3 lines per tick)

---

## 34. Jump to Top/Bottom Keybinds

### Status: Not yet planned

### Motivation

Large conversations require repeated arrow presses to navigate. Need `Home`/`End` or `gg`/`G` keybinds.

### Design

- `Ctrl+Home` / `gg`: jump to top of message list
- `Ctrl+End` / `G`: jump to bottom (re-enable auto-scroll)
- `PageUp` / `PageDown`: already implemented (10 lines)

---

## 35. Message Timestamps

### Status: Not yet planned

### Motivation

No indication of when messages were sent or how long responses took. Missing temporal context.

### Design

- Optional timestamp displayed inline after each message role label
- Format: `Tau · 14:32:05 (3.2s)` — time sent + duration for assistant messages
- Toggle via config: `ui.show_timestamps: true`

---

## 36. Streaming Progress Indicator

### Status: Not yet planned

### Motivation

The static `▌` cursor is the only streaming indicator. A more meaningful progress animation would improve the polished feel.

### Design

- Animated spinner or pulsing indicator during streaming (e.g., `▌▌ ▌▌ ▌▌ ` rotating)
- Optional typing speed indicator: characters/second or tokens/second
- Low priority — cosmetic improvement

---

## 37. Spinner for Async Operations

### Status: Not yet planned

### Motivation

Model refresh and extension reload show text notices but no progress indicator. User can't tell if something is actively happening.

### Design

- Show a spinner in the status bar during model refresh (`⏳ refreshing models…`)
- Extension reload: spinner + "reloading extensions…"
- Both operations are fast (<2s typically) but the spinner gives feedback that work is happening

---

## 38. MCP (Model Context Protocol) Server Support

### Status: Not yet planned (HIGHEST extensibility gap)

### Motivation

Tau cannot connect to external MCP servers — the biggest extensibility gap in the current architecture. MCP is the emerging standard for AI tool integration. Adding MCP client support would unlock the entire ecosystem of MCP-compatible tools, resources, and prompts without writing Lua extensions.

### Design

**`internal/mcp/` package**:
- `MCPClient` interface: `Connect(serverCommand string, env []string)`, `ListTools()`, `CallTool()`, `ListResources()`, `Close()`
- Transport: stdio (primary) + SSE/HTTP (future)
- Server lifecycle: start process, negotiate protocol version, initialize capabilities
- Tool discovery: on connect, list tools and register them into the agent's tool registry under an `mcp/<server>/` namespace

**Config**:
```yaml
mcp:
  servers:
    - name: filesystem
      command: npx
      args: ["-y", "@anthropic/mcp-server-filesystem", "/path/to/allowed"]
    - name: github
      command: npx
      args: ["-y", "@anthropic/mcp-server-github"]
      env:
        GITHUB_TOKEN: $GITHUB_TOKEN
```

**Integration**:
- On session start: connect to all configured MCP servers, discover tools, register into agent tool registry
- Tool namespacing: `mcp/filesystem/read_file`, `mcp/github/create_issue`
- MCP tools coexist with built-in and Lua-extension tools
- On session close: disconnect all MCP servers

### Open questions

1. OAuth support for MCP servers that require it?
2. Should MCP servers be per-session or global (shared across sessions)?
3. Tool result size limits for MCP tools?

---

## 39. Multi-Provider Routing and Failover

### Status: Not yet planned

### Motivation

One provider per session with no failover. Users cannot use cheaper models for simple tasks or fail over to a backup provider when the primary is rate-limited or down.

### Design

- `ProviderRouter` in config: maps task types to providers
- `fallback` chain: if primary provider returns 429/5xx, try next in chain
- `/provider` slash command for manual switch
- Provider health status indicator in header

```yaml
routing:
  default: anthropic
  fallback:
    - openai
    - ollama-local
  task_routing:
    simple: ollama-local    # cheap tasks to local
    code: anthropic          # complex tasks to premium
```

---

## 40. Structured Output / JSON Mode

### Status: Not yet planned

### Motivation

No way to request and validate structured responses (JSON Schema) from the model. Cannot use Tau for programmatic data extraction, API response generation, or structured workflows.

### Design

- `/json <schema>` slash command to request JSON-structured output
- Passes `response_format: { type: "json_schema", json_schema: {...} }` to the API
- Validates response against schema on completion
- Non-interactive mode: `tau --json-schema schema.json --prompt "..."`

---

## 41. HTTP API / Web Interface

### Status: Not yet planned

### Motivation

Terminal-only interface. No way to programmatically interact with Tau for integration with other tools, CI/CD pipelines, or web-based workflows.

### Design

- Optional HTTP server (`tau serve` or `tau --api`)
- REST endpoints: `POST /chat`, `GET /sessions`, `GET /sessions/:id`, `DELETE /sessions/:id`
- SSE streaming for chat responses
- Auth: local-only by default (localhost binding), optional API key
- Swagger/OpenAPI spec for the HTTP API

---

## 42. YAML-Configurable Tools (No-Code)

### Status: Not yet planned

### Motivation

Users must write Lua to add even simple tools. Many use cases could be served by declarative YAML tool definitions for shell commands, HTTP requests, or file templates.

### Design

```yaml
tools:
  - name: deploy
    description: Deploy the current project to staging
    type: shell
    command: make deploy STAGE=staging
    timeout: 60s
    
  - name: search-docs
    description: Search internal documentation
    type: http
    url: https://docs.internal/api/search
    method: POST
    headers:
      Authorization: Bearer $DOCS_API_KEY
    body_template: '{"query": "{{.query}}"}'
```

- `type: shell` — runs a shell command with argument templating
- `type: http` — makes an HTTP request with header/body templating
- `type: template` — renders a Go template and returns the result
- Arguments extracted from `{{.arg_name}}` placeholders
- Available alongside Lua extensions, not replacing them

---

## 43. Extension Marketplace & Discovery

### Status: Not yet planned

### Motivation

Extensions must be manually placed in filesystem directories. No `tau extensions search` or `tau extensions install` command. High friction for discovering and installing extensions.

### Design

```
tau extensions search <query>       # search a registry
tau extensions install <name>       # install from registry
tau extensions install gh:user/repo # install from GitHub
tau extensions list                 # list installed
tau extensions update [name]        # update to latest
tau extensions remove <name>        # uninstall
```

- Registry: GitHub repo index or simple JSON API
- Install target: `~/.tau/extensions/` (user) or `.tau/extensions/` (project)
- Lockfile: `tau-extensions.lock` pins versions
- Skills already work similarly (§23) — the same CLI pattern applies

---

## 44. Custom TUI Components from Extensions

### Status: Not yet planned

### Motivation

Lua extensions can add slash commands but cannot add custom UI panels, modals, or widgets. Extensions feel like backend-only additions.

### Design

- Extension API: `tau.ui.createPanel(title, content)` → returns a panel ID
- Extension API: `tau.ui.updatePanel(id, content)` → updates live
- Extension API: `tau.ui.createModal(title, content, buttons)` → modal dialog
- Extension API: `tau.ui.removePanel(id)` → cleanup
- Rendered via go-tui `MountPersistent` with extension-owned state
- Panels appear in the chat area or as sidebars
- Security: extension panels cannot access the main message list or input

---

## 45. Expanded Lua Host API Surface

### Status: Not yet planned

### Motivation

Lua extensions currently get lifecycle hooks and slash commands but cannot access messages, sessions, or the TUI rendering pipeline. Extensions are sandboxed too aggressively to be useful for many workflows.

### Design

- `tau.session.messages()` → read-only access to current session messages
- `tau.session.system_prompt()` → read current system prompt
- `tau.session.model()` → read current model info
- `tau.config.get(key)` → read config values
- `tau.fs.read(path)`, `tau.fs.write(path, content)` → restricted filesystem access (within working directory)
- All APIs are read-only where mutation could corrupt session state
- Permission declarations in extension manifest control API access

---

## 46. Pipeline / Command Chaining

### Status: Not yet planned

### Motivation

No way to compose commands or create multi-step workflows. Power users limited to single requests.

### Design

- `/chain` or `/pipe` command: compose multiple operations
- Example: `/chain /model claude-sonnet | /system "You are a Python expert" | write a fibonacci function`
- Simpler approach: a `tau recipe` file format for reusable command sequences

```yaml
# tau-recipes/code-review.yaml
name: code-review
steps:
  - command: /model claude-sonnet
  - command: /system "You are a senior code reviewer. Be thorough and constructive."
  - prompt: "Review the following code for bugs, security issues, and style problems:\n{{.input}}"
```

---

## 47. Request/Response Logging & Trace Mode

### Status: Not yet planned

### Motivation

No trace capability for debugging LLM behaviour. Hard to debug why a model produced a certain response or why tool calls were made.

### Design

- `--trace` CLI flag or `/trace on|off` TUI command
- When enabled: log every request body + full response to `~/.tau/traces/` as JSONL
- Each trace entry: timestamp, request (model, messages, tools, parameters), response (full SSE stream, tool calls, usage)
- Privacy: traces contain full conversation content — warn user on first enable
- Trace replay: `tau trace replay <id>` to replay a request through any model

---

## 48. Fuzzing and Property-Based Testing

### Status: Not yet planned

### Motivation

22 test files but no fuzz testing for session state transitions. Edge cases in the state machine may be uncovered late.

### Design

- Fuzz `ChatSessionState` transitions: random sequences of BeginTurn, AppendDelta, CompleteTurn, CancelTurn, FailTurn, Reset, Close
- Property: no transition should leave the state machine in an invalid state
- Property: every operation on a closed session must return an error
- Property: PendingAssistant and ActiveRequestID are consistent with Status

---

## 49. Benchmarking

### Status: Not yet planned

### Motivation

No benchmarks for streaming throughput, turn loop latency, or tool execution. Performance regressions are invisible.

### Design

- `BenchmarkStreamRead`: SSE parsing throughput with varying message sizes
- `BenchmarkToolLoop`: full turn loop with mock LLM that returns tool calls
- `BenchmarkCoordinatorSubmit`: command submission latency under concurrent senders
- `BenchmarkEventBusPublish`: high-frequency publish throughput with multiple subscribers

---

## 50. Remove `/system` Command

### Status: Planned

### Motivation

The `/system` command sets the system prompt but there's no way to view the current value, and it's not the right UX for the agent workflow. System prompts should be set in the agent template/config, not ad-hoc during chat. The command is useful only for one-shot testing.

### Action

- Remove `/system` from TUI command handler and autocomplete
- Remove `UpdateChatSessionCommand` support for system prompt patches from TUI (keep in protocol for internal use)
- `ChatSessionConfig.SystemPrompt` still settable at session creation time

---

## 51. Confirmation Replaced by Session Resume on Exit

### Status: Planned

### Motivation

Once session persistence (§10) is implemented, closing the app should not require confirmation. Instead, on exit, display a summary of the session and how to resume it (`tau --resume <id>` or `tau --resume` for the most recent). No confirmations needed — auto-save handles safety.

### Design

- On `Ctrl+C` or `/exit`: save session, print summary to terminal after TUI closes
- Summary: session ID, message count, token total, resume command
- Example: `Session 019e7cf9 saved. Resume: tau --resume 019e7cf9`
- No dialog, no confirmation — instant exit with auto-save

---

## 52. Per-Message Keybind Improvements

### Status: Not yet planned

### Motivation

Basic editing keybinds are missing: `Ctrl+Left/Right` for word navigation, `Ctrl+K` for forward-delete-line, `Ctrl+W` / `Ctrl+Backspace` for backward-delete-word.

### Design

- Extend `chatInput.KeyMap()` with the missing keybinds
- `Ctrl+Left` / `Ctrl+Right`: already implemented as word navigation
- `Ctrl+K`: delete from cursor to end of line
- `Ctrl+W` or `Ctrl+Backspace`: delete word before cursor
- `Ctrl+A` / `Ctrl+E`: home/end (already have Home/End keys)

---

## 53. Custom Theming from Config

### Status: Not yet planned

### Motivation

Theme is hardcoded in `internal/theme/theme.go`. Users cannot define colour profiles in config.

### Design

```yaml
ui:
  theme: dracula  # or custom
  themes:
    custom:
      background: "#1e1e2e"
      foreground: "#cdd6f4"
      accent: "#cba6f7"
      dim: "#6c7086"
      error: "#f38ba8"
      success: "#a6e3a1"
```

- Built-in themes: `tau` (default), `dracula`, `nord`, `solarized-dark`, `catppuccin`
- Custom themes override any subset of colours
- Theme package reads from config at startup, falling back to compiled-in defaults
