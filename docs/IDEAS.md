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

## 31. Bundled Reference Plugin with External Plugin Ecosystem

### Status: Planned

### Motivation

Tau should ship with one ready-made extension that demonstrates the plugin contract and gives immediate value (MCP support), while still encouraging community plugins hosted in separate repositories.

### Decision

- Keep `plugins/tau-plugin-mcp` inside the tau repository as a maintained reference implementation.
- Do not require users to install it.
- Preserve support for third-party plugins located in external repositories.

### Why this model

- New users get a working example of plugin structure and runtime behavior without extra setup.
- Plugin authors can copy or fork a known-good implementation as a template.
- Core maintainers can validate extension APIs against a real in-tree consumer.
- Community ecosystem remains open and decoupled from the core repo.

### Implementation sketch

**Discovery and loading**
- Keep local development discovery from `plugins/` in `internal/plugin/manager.go`.
- Add/configure user-level plugin paths in `internal/config/config.go` (for example `plugins.paths`), so external repos can be discovered without vendoring.
- Ensure `internal/plugin/plugin.go` metadata includes clear identity fields (`name`, `version`, `source`) to distinguish bundled vs external plugins in diagnostics.

**CLI and UX**
- Add docs and CLI output that classify plugin origin:
  - bundled (in-repo reference),
  - local path,
  - external repo install.
- Surface this in plugin listing and startup logs so users understand that bundled plugins are optional examples, not mandatory dependencies.

**Documentation flow**
- Position `plugins/tau-plugin-mcp` as the canonical "hello world + real capability" example.
- Add a short "build your own plugin" walkthrough that points to this plugin as a template and then to external hosting recommendations.

### Files to modify (incremental)

| File | Change |
|------|--------|
| `internal/config/config.go` | Add or formalize configurable plugin search paths |
| `internal/plugin/manager.go` | Merge discovery from bundled and external paths with deterministic precedence |
| `internal/plugin/plugin.go` | Expand plugin metadata to include origin/source |
| `internal/cli/commands.go` | Improve plugin list/status output to show plugin origin |
| `docs/NEXT_STEPS.md` | Add implementation milestones for external plugin discovery UX |
| `plugins/tau-plugin-mcp/README.md` | Document role as optional reference plugin/template |

### Open Questions

1. Should bundled plugins be auto-enabled or only shown as available until explicitly enabled?
2. Should external plugin installs be managed by a future `tau plugin install` command or remain path-based first?
3. What precedence should apply when a bundled and external plugin share the same name?

---

## 32. Dedicated Plugin Repository and Remote Install UX

### Status: Planned

### Motivation

A separate plugin repository (for example `samcharles93/tau-plugins`) gives cleaner ownership and versioning for extensions while keeping the main tau repository focused on core runtime and UX.

### Decision direction

- Keep tau core in this repository.
- Host first-party and community-curated plugins in a dedicated plugins repository.
- Provide a simple install command using a source spec format like `owner/repo:plugin`.

### Proposed install command

```shell
tau plugin install samcharles93/tau-plugins:mcp
```

Optional future flags:

```shell
tau plugin install samcharles93/tau-plugins:mcp@v1.2.0
tau plugin install samcharles93/tau-plugins:mcp --ref main
tau plugin install samcharles93/tau-plugins:mcp --verify
```

### Source spec format

`<owner>/<repo>:<plugin>[@<version>]`

- `owner/repo` maps to a Git remote source.
- `plugin` identifies a subdirectory or manifest entry inside the plugins repo.
- `version` maps to a git tag or release.

### Installation flow (MVP)

1. Parse source spec in CLI.
2. Clone/fetch repo into a cache directory (`~/.cache/tau/plugins/repos/<owner>/<repo>`).
3. Resolve plugin from manifest (`plugins.yaml`) or default path (`plugins/<plugin>`).
4. Copy plugin artifact/files into user install dir (`~/.tau/plugins/<plugin>`).
5. Record lock metadata (`source`, `resolved_commit`, `version`, `installed_at`) for updates and audit.

### Recommended repository layout (`tau-plugins`)

```text
plugins/
  mcp/
    plugin.yaml
    README.md
    bin/
      tau-plugin-mcp-linux-amd64
      tau-plugin-mcp-darwin-arm64
      tau-plugin-mcp-windows-amd64.exe
plugins.yaml
```

### Security and trust model

- Default to pinned tag/commit installs.
- Add optional checksum/signature verification in `plugin.yaml`.
- Prompt before enabling shell/file permissions declared by plugin manifests.

### Files to modify (incremental)

| File | Change |
|------|--------|
| `internal/cli/commands.go` | Add `tau plugin install` command and source-spec parsing |
| `internal/plugin/manager.go` | Add install registry and lockfile handling |
| `internal/plugin/plugin.go` | Add source/version/commit metadata fields |
| `internal/config/config.go` | Add plugin install/cache directories and defaults |
| `docs/NEXT_STEPS.md` | Add staged roadmap for remote plugin install support |

### Open Questions

1. Should `tau plugin install` support GitHub shorthand only at first, or generic git URLs from day one?
2. Should plugin manifests allow multiple binaries per OS/ARCH or require a single URL template?
3. Should Tau auto-update plugins by default, or only through explicit `tau plugin update`?

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

---

## 54. Custom Provider Types via Extensions Architecture

### Status: Not yet planned

### Motivation

Provider auth is hardcoded to three types: `api_key`, `none`, `oauth_pkce`. Real-world providers (MaaS gateways, org-specific proxies) need multi-stage auth flows, custom token exchange, per-model URL overrides, and non-standard auth methods. Embedding these in the core platform package is:

- A security risk — infrastructure details (URLs, auth flows) should not be in a public repo
- A maintenance burden — each org-specific flow adds complexity to the generic paths
- A hard cap on adoption — users can't add providers without forking Tau

### Design

Extensions register **provider types** via a new Lua API. A provider type is a named bundle of auth hooks and model discovery logic that the extension supplies:

```lua
-- Extension: maas_provider.lua
tau.register_provider_type("maas_oauth", {
  name = "MaaS Gateway",
  description = "Two-stage OAuth PKCE → MaaS JWT exchange",
  -- Auth hooks
  post_authenticate = function(ctx)
    -- Called after OAuth PKCE completes.
    -- ctx.bearer_token contains the OAuth token.
    -- Return the final token to use for API calls.
    local jwt = tau.http.post(ctx.exchange_url, {
      headers = { Authorization = "Bearer " .. ctx.bearer_token },
      body = { expiration = ctx.expiry or "8h" }
    })
    return jwt.token
  end,
  -- Model URL resolution
  model_url = function(model_id, base_url, model_cfg)
    return model_cfg.url or base_url .. "/v1/chat/completions"
  end,
  -- Config schema — what fields the user puts in their config.yaml
  config_schema = {
    token_exchange_url = { type = "string", required = true },
    token_expiry = { type = "string", default = "8h" },
  }
})
```

**Config integration** — users reference the provider type in `config.yaml`:

```yaml
providers:
  maas:
    type: maas_oauth        # matches registered provider type
    base_url: https://api.example.com
    auth:
      type: oauth_pkce       # base auth mechanism
      authorize_url: ...
      token_url: ...
      client_id: ...
      token_auth_method: basic
      token_exchange_url: ...  # extension-specific field
      token_expiry: "8h"       # extension-specific field
```

**Fallback**: If no extension registers the type, the standard built-in types (`api_key`, `none`, `oauth_pkce`) work as before. Zero-config for standard providers.

**Files to create/modify**:
- `internal/extensions/provider.go` — provider type registry, hook dispatch
- `internal/extensions/lua/provider_api.go` — Lua bindings: `tau.register_provider_type`
- `internal/app/chat.go` — check for registered provider type before falling back to built-in
- `internal/platform/` — NO changes. Provider-specific logic lives in extensions.

---

## 55. Inline-Streaming TUI Mode

### Status: Not yet planned

### Motivation

Tau currently uses go-tui in full-screen alternate-screen mode. This works but has limitations:

- **Text selection is broken** — mouse capture prevents native terminal copy/paste
- **No scrollback** — closed sessions lose their history entirely (session persistence helps but requires /resume)
- **Native terminal feel is lost** — full-screen apps feel isolated from the rest of the terminal

go-tui's inline mode (`WithInlineHeight`) solves all of these:
- Terminal scrollback works natively (mouse events disabled by default)
- Messages scroll into terminal history via `PrintAbove` / `StreamAbove`
- Text selection works out of the box
- Chat history persists in scrollback after exit
- Modals/settings/session picker use `EnterAlternateScreen()` / `ExitAlternateScreen()`
- Streaming LLM responses via `StreamAbove()` with styled/gradient output

### Design

**TUI layout** — two modes, toggled at runtime:

```
┌─ Inline mode (default) ──────────────────────────────┐
│  [scrollback: all previous messages, terminal output] │
│  ...                                                  │
│  You: Hello                                           │
│  Claude: Hi there! How can I help?                    │
│  ┌─ tau input ──────────────────────────────────────┐ │
│  │ > _                                              │ │
│  └──────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘

┌─ Alternate screen (modal) ───────────────────────────┐
│  ╭─ Sessions ───────────────────────────╮             │
│  │  › 2026-06-02 12:00  claude  msgs:38 │             │
│  │    2026-06-01 09:30  gpt-4   msgs:12 │             │
│  │    ↑↓: navigate  Enter: resume  Esc  │             │
│  ╰──────────────────────────────────────╯             │
└──────────────────────────────────────────────────────┘
```

**Implementation approach**:

```go
// internal/tui/chatui.go
app, err := tui.NewApp(
    tui.WithInlineHeight(3),  // input area
    tui.WithRootComponent(chatPanel),
)
```

- Input area: 3-row inline widget at bottom
- Messages: `app.PrintAboveln()` / `app.QueuePrintAboveln()` for discrete messages
- Streaming: `app.StreamAbove()` for character-by-character LLM output
- Modals (session list, settings, info): `app.EnterAlternateScreen()` / `app.ExitAlternateScreen()`
- Keybind: Esc when idle → stop app; Esc in modal → close modal and return to inline

**Migration path**: The existing full-screen rendering paths (messages list, chat input, modals) are refactored so the message list becomes `PrintAbove` calls and the input+status becomes the inline widget. Modals remain as full-screen overlays via alternate screen.

**Files to modify**:
- `internal/tui/chatui.go` — switch to inline mode, refactor message rendering to `PrintAbove`/`StreamAbove`
- `internal/tui/views/session_list.go` — already rendered as modal, works with alternate screen
- `internal/tui/views/session_info.go` — same
- `internal/tui/views/settings.go` — same
- `internal/tui/components/` — may simplify (no scrollable message list needed)

---

## 56. Sensitive Provider Logic in Extensions

### Status: Not yet planned

### Motivation

Provider-specific auth logic (MaaS token exchange, org-specific proxies, custom auth flows) contains infrastructure details that must not appear in a public repository. The MaaS token exchange patch exposed internal URLs, endpoint paths, and auth flow details in commit messages, source code, and a `FIX_PLAN.md` file.

Extensions solve this because:
- Extensions live outside the Tau repo (in `~/.config/tau/extensions/` or user directories)
- They can be `.gitignore`'d or kept in private repos
- The core Tau codebase stays generic and infrastructure-agnostic

### Design

**Principle**: Any logic that contains organisation-specific URLs, endpoint paths, or auth flow details belongs in an extension, never in `internal/platform/`.

**Current leakage points** (fixed by #54 — Custom Provider Types):
- `internal/platform/maas.go` — `ExchangeMaaSToken` with hardcoded exchange URL pattern → becomes a `post_authenticate` hook in an extension
- `internal/config/config.go` — `TokenExchangeURL`, `TokenExpiry` fields on `AuthConfig` → extension-defined config schema
- `internal/app/chat.go` — token exchange wiring → generic hook call, extension provides the logic

**What stays in core**:
- Standard auth types: `api_key`, `none`, `oauth_pkce` (these are universal)
- Provider type registry and hook dispatch
- Extension API for registering provider types

**What goes to extensions**:
- Token exchange URLs and logic
- Custom auth headers/methods
- Per-model URL resolution
- Provider-specific error handling

**Security note**: Extension code runs in-process (Lua VM). Token values are passed as strings. Extensions should not log tokens. The `slog` calls in `maas.go` printing token prefixes should be removed or guarded behind a debug flag.

---

## 57. Migrate Extension Architecture from Lua to go-plugin with gRPC

### Status: Not yet planned

### Motivation

The current extensions architecture runs Lua scripts in-process via gopher-lua. This has several limitations:

- **No isolation** — a misbehaving extension can hang or crash Tau
- **No streaming** — Lua can't efficiently stream data (LLM responses)
- **No language choice** — extensions must be written in Lua
- **Limited concurrency** — shared Lua VM with a global interpreter lock
- **Development friction** — no type safety, limited IDE support, unfamiliar language for many developers

[hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) with gRPC is a mature, production-proven extension architecture used by Terraform, Vault, Nomad, and Packer. It provides:

- **Process isolation** — each extension runs as a separate binary
- **gRPC streaming** — native bidirectional streaming for LLM token output
- **Any language** — extensions can be written in Go, Rust, Python, or any gRPC-capable language
- **Type-safe contracts** — protobuf definitions are the single source of truth
- **Auto-negotiated transports** — falls back from gRPC to net/rpc for simpler extensions
- **Built-in health checking** — host detects crashed plugins and can restart them
- **Secure by default** — stdio-based transport, no network ports exposed

### Architecture (from go-plugin streaming example)

The example demonstrates the core pattern: a shared interface, proto-generated gRPC stubs, and a streaming data path with chunking for large payloads.

```
┌─ Tau (host) ─────────────────────────────────────────┐
│                                                        │
│  plugin.NewClient(&plugin.ClientConfig{                │
│    Plugins: map[string]plugin.Plugin{                  │
│      "streamer": &shared.StreamerPlugin{Impl: impl},   │
│    },                                                  │
│    Cmd: exec.Command("./plugins/streamer"),             │
│  })                                                    │
│                                                        │
│  rpcClient.Dispense("streamer") → Streamer interface   │
│  streamer.Configure(...)                               │
│  streamer.Write(ctx, data)  // client-side streaming   │
│  streamer.Read(ctx)          // server-side streaming  │
│                                                        │
└────────────────────┬───────────────────────────────────┘
                     │ gRPC over stdio
┌─ Plugin (separate binary) ────────────────────────────┐
│                                                        │
│  plugin.Serve(&plugin.ServeConfig{                     │
│    Plugins: map[string]plugin.Plugin{                  │
│      "streamer": &shared.StreamerPlugin{               │
│        Impl: &FileStreamer{...},                       │
│      },                                                │
│    },                                                  │
│  })                                                    │
│                                                        │
│  // FileStreamer implements shared.Streamer            │
│  func (fs *FileStreamer) Read(ctx) ([]byte, error)     │
│  func (fs *FileStreamer) Write(ctx, b []byte) error    │
│  func (fs *FileStreamer) Configure(ctx, ...) error     │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**Key files from the reference example**:

| File | Role |
|------|------|
| `proto/streamer.proto` | Protobuf service definition (`Configure`, `Read` server-stream, `Write` client-stream) |
| `shared/interface.go` | Go interface that both host and plugin agree on |
| `shared/client.go` | gRPC client adapter — translates `Streamer` calls to gRPC |
| `shared/server.go` | gRPC server adapter + `StreamerPlugin` (the `go-plugin` shim) |
| `plugin/plugin.go` | Plugin binary: `main()` with `plugin.Serve()`, real implementation of `Streamer` |
| `main.go` | Host: `plugin.NewClient()`, dispenses plugin, calls interface methods |

### Tau-Specific Design

**Phase 1: Core plugin infrastructure**

Create a `pkg/plugin` package (public API for extension authors):

```go
// pkg/plugin/extension.go — the interface extension authors implement
type Extension interface {
    Name() string
    Commands() []Command
    OnSessionStart(ctx context.Context, session SessionInfo) error
    OnSessionShutdown(ctx context.Context, session SessionInfo) error
    OnToolCall(ctx context.Context, tool ToolCall) (ToolResult, error)
}

type Command struct {
    Name        string
    Description string
    Handler     func(ctx context.Context, args string) (string, error)
}

type SessionInfo struct {
    ID        string
    ModelID   string
    Provider  string
    CreatedAt time.Time
}
```

**Phase 2: Proto service definition**

```protobuf
// internal/plugin/proto/extension.proto
service ExtensionService {
  rpc GetName(GetName.Request) returns (GetName.Response);
  rpc GetCommands(GetCommands.Request) returns (GetCommands.Response);
  rpc OnSessionStart(SessionEvent) returns (google.protobuf.Empty);
  rpc OnSessionShutdown(SessionEvent) returns (google.protobuf.Empty);
  rpc StreamChat(stream StreamChat.Chunk) returns (stream StreamChat.Chunk);
}
```

**Phase 3: Plugin discovery and lifecycle**

- `~/.config/tau/plugins/` directory scanned on startup
- Each subdirectory contains a compiled plugin binary + optional config
- `tau plugins list` shows loaded plugins with status
- `tau plugins reload` sends SIGHUP to all plugins and re-discovers
- Plugin crashes are detected via health check; auto-restart with backoff

**Phase 4: Migration from Lua**

- Existing Lua extension API (`tau.register_tool`, etc.) is deprecated but NOT removed
- New extensions use the go-plugin API
- Lua shim layer: a go-plugin extension that wraps the gopher-lua VM, allowing existing Lua extensions to run as a plugin binary
- Eventually the Lua VM can be removed entirely

**Streaming benefit for Tau**: The `StreamChat` RPC uses bidirectional streaming — the host sends user prompts and receives LLM tokens, tool call intermediates, and final responses over the same gRPC stream. This is a direct replacement for the current coordinator's `StreamChatCompletionFull` callback pattern.

### Files to Create/Modify

| File | Action | Phase |
|------|--------|-------|
| `pkg/plugin/extension.go` | New — public `Extension` interface | 1 |
| `pkg/plugin/command.go` | New — command registration types | 1 |
| `pkg/plugin/events.go` | New — session/tool event types | 1 |
| `internal/plugin/proto/extension.proto` | New — protobuf service definition | 2 |
| `internal/plugin/proto/` | New — generated Go code (`protoc-gen-go`, `protoc-gen-go-grpc`) | 2 |
| `internal/plugin/server.go` | New — gRPC server adapter + ExtensionPlugin shim | 2 |
| `internal/plugin/client.go` | New — gRPC client adapter | 2 |
| `internal/plugin/manager.go` | New — plugin discovery, lifecycle, health checking | 3 |
| `internal/app/chat.go` | Modify — replace extensionManager with pluginManager | 3 |
| `go.mod` | Modify — add `google.golang.org/grpc`, `google.golang.org/protobuf`, `github.com/hashicorp/go-plugin` | 1 |

### Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Plugin binary compilation burden | Medium | Ship a `tau plugin new` scaffolder; provide pre-built plugin binaries via `tau plugins install` |
| gRPC + protobuf adds build complexity | Medium | `//go:generate` directives; CI pipeline handles proto generation |
| Plugin version mismatch | Low | Handshake protocol version check on connect; plugin manifest with semver range |
| Startup latency (spawning processes) | Low | Plugins started once and kept alive; health check pings are cheap |
| Memory overhead (N plugin processes) | Low | Typical deployment is 1-5 plugins; each Go plugin ~10-20MB RSS |

---

## 58. Extension Surface Design — Capability-Based Plugin System

### Status: Design phase

### Motivation

The initial Extension interface (Metadata, RunCommand, Reload, DispatchEvent) is deliberately narrow — just commands and lifecycle events. To support the wilder ideas (Custom TUI Panels, Multi-Model Router, Compliance/Audit, Live Token Stream Processor, External Tool Registry), the interface must grow. But we can't just keep adding methods — that breaks every existing plugin on each release.

**The solution**: capability-based extension surface. Plugins declare which capabilities they support. The host discovers capabilities during handshake. Unknown capabilities are gracefully ignored. This is how gRPC service discovery already works — we formalize it.

### Design: Capability-Based Plugin Interface

Instead of one monolithic `Extension` interface, plugins implement **capability interfaces**. Each capability is a separate gRPC service. Plugins opt in to the capabilities they need.

```
┌─ Plugin binary ───────────────────────────────────┐
│                                                     │
│  Capabilities declared in plugin manifest:          │
│                                                     │
│  ☑ core         — metadata, commands, lifecycle    │
│  ☐ provider     — custom auth, model routing       │
│  ☐ stream       — token interception/transformation │
│  ☐ tools        — dynamic tool registration        │
│  ☐ ui           — custom panels, keybindings       │
│  ☐ audit        — observation sink for all events  │
│  ☐ pipeline     — request/response middleware      │
│                                                     │
│  Only implements the gRPC services it needs.       │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### Proto: Capability Discovery

```protobuf
// Capabilities are discovered during plugin handshake.
service ExtensionService {
  // Core — every plugin must implement.
  rpc GetCapabilities(CapabilitiesRequest) returns (CapabilitiesResponse);
  rpc GetMetadata(GetMetadataRequest) returns (GetMetadataResponse);
  rpc Reload(ReloadRequest) returns (ReloadResponse);
  rpc DispatchEvent(DispatchEventRequest) returns (DispatchEventResponse);
}

// Optional capability A: Provider (custom auth/model routing).
service ProviderService {
  rpc ResolveModel(ResolveModelRequest) returns (ResolveModelResponse);
  rpc ExchangeToken(ExchangeTokenRequest) returns (ExchangeTokenResponse);
  rpc DiscoverModels(DiscoverModelsRequest) returns (stream ModelInfo);
}

// Optional capability B: Stream (token interception).
service StreamService {
  // Bidirectional: host sends tokens, plugin returns transformed tokens.
  rpc ProcessStream(stream StreamChunk) returns (stream StreamChunk);
}

// Optional capability C: Tools (dynamic registration).
service ToolService {
  rpc RegisterTools(ToolRegistrationRequest) returns (stream ToolDefinition);
  rpc ExecuteTool(ToolExecutionRequest) returns (ToolExecutionResponse);
}

// Optional capability D: UI (custom panels).
service UIService {
  rpc RenderPanel(PanelRequest) returns (stream PanelUpdate);
  rpc HandleKey(KeyEvent) returns (KeyResponse);
}

// Optional capability E: Audit (observation).
service AuditService {
  rpc Observe(stream AuditEvent) returns (AuditAck);
}

// Optional capability F: Pipeline (middleware).
service PipelineService {
  // Plugin sits between host and LLM. Host sends request, plugin may:
  // - Forward unchanged (passthrough)
  // - Rewrite (change model, system prompt, messages)
  // - Short-circuit (return cached/static response)
  // - Route (send to different model)
  rpc ProcessRequest(stream PipelineEvent) returns (stream PipelineEvent);
}

message CapabilitiesResponse {
  repeated string capabilities = 1; // ["core", "provider", "stream", ...]
}
```

### Go Interface: Capability Interfaces

```go
// pkg/plugin/capabilities.go

// Core is required — every plugin implements this.
type Core interface {
    Metadata() (name string, commands []*proto.Command)
    RunCommand(ctx context.Context, name, args string) (string, error)
    Reload(ctx context.Context) ([]*proto.Diagnostic, []*proto.Command, error)
    DispatchEvent(ctx context.Context, event string, context map[string]string)
}

// Provider enables custom auth flows and model routing.
type ProviderCapability interface {
    // ResolveModel returns the model to use for a request.
    // Can route based on prompt content, user preference, cost budget.
    ResolveModel(ctx context.Context, req ResolveModelRequest) (ModelRef, error)
    
    // ExchangeToken transforms a base auth token into a provider-specific token.
    ExchangeToken(ctx context.Context, baseToken string, config map[string]string) (string, error)
    
    // DiscoverModels returns available models (may differ from config).
    DiscoverModels(ctx context.Context, baseURL string) ([]ModelInfo, error)
}

// StreamProcessor intercepts and transforms LLM output tokens.
type StreamCapability interface {
    // ProcessStream is bidirectional streaming.
    // Receives tokens from the LLM, returns transformed tokens.
    // Can: pass through, redact PII, translate, inject context.
    ProcessStream(ctx context.Context, input <-chan StreamChunk, output chan<- StreamChunk) error
}

// ToolProvider registers and executes custom tools.
type ToolCapability interface {
    // RegisterTools returns tool definitions to register with the registry.
    RegisterTools(ctx context.Context) ([]ToolDef, error)
    
    // ExecuteTool runs a tool and returns the result.
    ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)
}

// UIPanel renders a custom UI component in the TUI.
type UICapability interface {
    // RenderPanel is server-streaming — plugin pushes updates to the TUI.
    RenderPanel(ctx context.Context, panelID string, output chan<- PanelUpdate) error
    
    // HandleKey processes a key event in the plugin's panel.
    HandleKey(ctx context.Context, panelID string, key KeyEvent) (KeyResponse, error)
}

// AuditSink receives a stream of all observable events.
type AuditCapability interface {
    // Observe is client-streaming — host pushes events, plugin acknowledges.
    Observe(ctx context.Context, events <-chan AuditEvent) error
}

// Pipeline sits in the request/response path.
type PipelineCapability interface {
    // ProcessRequest is bidirectional streaming.
    // Plugin receives request, can rewrite, route, or short-circuit.
    ProcessRequest(ctx context.Context, input <-chan PipelineEvent, output chan<- PipelineEvent) error
}
```

### Capability Discovery Flow

```
1. Host starts plugin binary
2. gRPC handshake completes
3. Host calls GetCapabilities() — plugin returns ["core", "provider", "audit"]
4. Host checks each capability:
   - "core" → always required
   - "provider" → register in provider resolution chain
   - "audit" → subscribe to event bus
   - "ui" → register panel in TUI layout
   - "stream" → wrap streamer with interceptor
   - Unknown → silently ignore (forward compatibility)
5. Plugin runs — only receives calls for capabilities it declared
```

### How Each Wild Idea Maps to Capabilities

| Idea | Capabilities | What happens |
|------|-------------|--------------|
| **Custom TUI Panels** | core + ui | GetMetadata + RenderPanel streams to TUI; HandleKey for keypresses |
| **Multi-Model Router** | core + provider + pipeline | ResolveModel picks model; ProcessRequest rewrites the prompt/system for that model |
| **Compliance/Audit** | core + audit | Observe receives every ChatEvent; plugin logs/signs/stores to external system |
| **Live Token Stream** | core + stream | ProcessStream receives tokens, redacts PII, injects links, translates; returned tokens render in TUI |
| **External Tool Registry** | core + tools | RegisterTools adds postgres/splunk/k8s tools; ExecuteTool runs them via plugin's network access |
| **Session Intelligence** | core + audit + tools | Observe watches all sessions; tools let user query the knowledge base |
| **Post-Chat Automation** | core + audit | Observe triggers on SessionShutdown; plugin reads messages, creates GitHub issue/Slack post |

### Protocol Evolution Strategy

**Adding a new capability (e.g., v1.1 adds "search" capability):**
1. Add `SearchService` to proto, add `SearchCapability` to Go interface
2. Generate new stubs — old clients don't need to recompile, their handshake won't list "search"
3. New plugins that declare "search" get the capability; old plugins ignore it
4. Host gracefully handles missing capabilities

**No breaking change ever needed** — capabilities are additive. The host discovers what a plugin supports and only calls those services. A v1.0 plugin runs unchanged on v2.x host because the host knows the plugin doesn't implement new services.

### What This Enables

**Plugin chaining**: The pipeline capability means plugins can compose:
```
User Prompt → [Audit Logger] → [PII Redactor] → [Model Router] → LLM
LLM Response → [PII Restorer] → [Translator] → [Audit Logger] → TUI
```

**Sandboxing**: Each plugin is a separate process. The audit plugin can't crash the streaming plugin. The UI plugin can't access the model router's credentials.

**Language freedom**: A Python developer writes the audit plugin (gRPC supports Python). A Rust developer writes the high-performance stream processor. A Go developer writes the model router. All work together.

### Implementation Plan

**Phase 1** — Core capability (current state): Metadata, RunCommand, Reload, DispatchEvent. Every plugin implements this. No capability discovery yet — all plugins are implicitly core-only.

**Phase 2** — Add `GetCapabilities` RPC and capability discovery in the manager. Plugins declare their capabilities. Host inspects and routes accordingly. Add `StreamService` (token interception) as the first optional capability.

**Phase 3** — Add `ProviderService`, `ToolService`, `UIService`, `AuditService`, `PipelineService` one at a time. Each is a separate proto service, separate Go interface, additive (no breaking changes).

**Phase 4** — Plugin chaining and ordering. Allow users to configure plugin order in the pipeline. "Run PII Redactor first, then Model Router, then Audit."

### Files

| File | Phase |
|------|-------|
| `internal/plugin/proto/extension.proto` | 2 — add `GetCapabilities` RPC, `StreamService` |
| `internal/plugin/proto/provider.proto` | 3 — `ProviderService` |
| `internal/plugin/proto/stream.proto` | 2 — `StreamService` |
| `internal/plugin/proto/tools.proto` | 3 — `ToolService` |
| `internal/plugin/proto/ui.proto` | 3 — `UIService` |
| `internal/plugin/proto/audit.proto` | 3 — `AuditService` |
| `internal/plugin/proto/pipeline.proto` | 3 — `PipelineService` |
| `internal/plugin/capabilities.go` | 2 — Go capability interfaces |
| `internal/plugin/manager.go` | 2 — capability discovery, routing |

---

## 59. Plugin Isolation Guarantees

### Status: Design constraint

### Motivation

Users can install any number of plugins. They must be guaranteed that plugins won't interfere with each other unless explicitly designed to do so. This isn't just about process crashes — it's about namespace collision, event ordering, resource contention, and pipeline composability.

go-plugin provides process isolation for free (separate binaries, separate OS processes). But we need additional guarantees at the host level.

### Isolation Dimensions

#### 1. Process Isolation (already provided by go-plugin)

- Each plugin is a separate OS process
- One plugin crashing does not affect others or the host
- Memory, file descriptors, network sockets are per-process
- Plugin A cannot access Plugin B's memory
- Host can detect crashed plugins via health check and restart them

#### 2. Namespace Isolation — Commands and Tools

**Problem**: Two plugins register `/export` command. Which one runs?

**Rule**: Command names and tool names are **plugin-scoped**. The host prepends the plugin name as a namespace:

```
Plugin "maas-auth" registers command "login"     → /maas-auth:login
Plugin "slack-bot" registers command "login"     → /slack-bot:login

No collision. Both can coexist.
```

For built-in commands that plugins shadow (e.g., a plugin wants to override `/export`), the user configures it explicitly:

```yaml
plugins:
  shadow:
    export: my-exporter  # route /export to plugin "my-exporter"
```

**Rule**: A plugin can only deregister its own commands/tools. Plugin A cannot remove Plugin B's commands.

#### 3. Pipeline Ordering — Explicit, Configurable

**Problem**: User installs PII Redactor and Model Router. Both are pipeline plugins. Which runs first? Does Redactor see the original prompt or the routed one?

**Rule**: Pipeline plugins receive events in a user-configured order. The order is explicit, not implicit:

```yaml
plugins:
  pipeline:
    request:
      - pii-redactor    # runs first — sees original user prompt
      - model-router    # runs second — sees redacted prompt, chooses model
      - audit-logger    # runs third — logs the final request
    response:
      - pii-redactor    # runs first — restores PII in the response
      - translator      # runs second — translates to user's language
      - audit-logger    # runs third — logs final response
```

**Rule**: Each pipeline plugin sees the output of the previous plugin in the chain. The first plugin sees the raw host output. No plugin can skip or remove another plugin from the chain — that's host configuration, not plugin behavior.

#### 4. Event Dispatch — Parallel by Default

**Problem**: A slow audit plugin should not delay the streaming plugin.

**Rule**: Event dispatch (lifecycle events, tool call notifications) fans out to all plugins **in parallel**. Each plugin's handler runs in its own goroutine. A slow plugin blocks only itself.

**Exception**: Pipeline processing is sequential (see #3 above) because middleware order matters.

**Timeout**: Each plugin gets a configurable timeout for event handling. If a plugin exceeds the timeout, the host logs a warning and moves on. The plugin is not killed — it may just be slow. Configurable via:

```yaml
plugins:
  timeout:
    event_dispatch: 5s
    pipeline_step: 30s
    command_execution: 60s
```

#### 5. Observation Isolation — Plugins Don't See Each Other

**Problem**: An audit plugin should see what the user sees, not what other plugins see.

**Rule**: Audit plugins receive events **after** pipeline processing. They see the final request (going to the LLM) and the final response (shown to the user), not intermediate pipeline states. This also means audit plugins cannot observe each other's processing.

**Rule**: No plugin can subscribe to another plugin's internal events. Plugin A cannot observe Plugin B's DispatchEvent calls.

**Rule**: If a plugin needs to communicate with another plugin, it must be explicit — they share a pub/sub topic that both subscribe to. This is opt-in, not default.

#### 6. Resource Limits

**Problem**: A plugin allocates 32GB of memory or opens 10,000 file descriptors.

**Rule**: Resource limits are configurable per-plugin or globally. Implemented via OS-level cgroups/rlimits where available:

```yaml
plugins:
  limits:
    max_memory_mb: 512
    max_file_descriptors: 256
    max_cpu_seconds_per_request: 30
```

On platforms without cgroups (macOS, Windows), best-effort with `runtime.SetMemoryLimit` and `os/rlimit`.

#### 7. Plugin Identity — Immutable After Load

**Problem**: A plugin changes its name or commands after reload.

**Rule**: Plugin identity (name, capabilities, command names) is established at load time. If a reload changes the identity, the host treats it as a new plugin — old commands are deregistered, new ones registered. Users are notified via a diagnostic.

This prevents a plugin from silently replacing another plugin's commands by changing its own name.

### Isolation Matrix

| Concern | Default | Override |
|---------|---------|----------|
| Process crashes | Plugin dies, others unaffected | Health check + auto-restart |
| Command name collision | Plugin-scoped namespacing | Explicit shadow config |
| Tool name collision | Plugin-scoped namespacing | Explicit shadow config |
| Pipeline ordering | Declared by user config | Required — no implicit ordering |
| Event dispatch ordering | Parallel (no ordering guarantee) | Sequential via pipeline config |
| Slow plugin blocks others | No — timeout + parallel dispatch | Timeout per plugin |
| Plugin A observes Plugin B | No — audit sees final output only | Explicit pub/sub topic |
| Resource exhaustion | Per-plugin limits | Configurable |
| Identity change on reload | Treated as new plugin | Diagnostic notification |

### What This Means for the Extension Surface

These isolation guarantees are enforced by the **host** (the plugin manager), not by individual plugins. The proto interface doesn't need to change — isolation is a host-side concern. The manager:

1. Prefixes command/tool names with plugin namespace before registering
2. Routes pipeline events through the configured chain in order
3. Dispatches events to plugins via goroutines with timeouts
4. Wraps plugin processes with resource limits (where OS supports it)
5. Validates plugin identity on reload and handles changes gracefully

**The key principle**: plugins are guests in the host's house. The host sets the rules. Plugins can't negotiate around them.

---

## 60. Bidirectional Plugin Communication — Host Service API

### Status: Design constraint

### Motivation

The current interface is host→plugin only: host dispatches events, host runs commands, host pushes audit events. But real plugins need to initiate communication back to the host. Two critical patterns require this:

1. **Agent-callable plugins** (MCP client): The agent needs to discover a plugin's tools and call them. The plugin registers tools with the host, then the host calls the plugin to execute them. But between calls, the MCP server might push notifications to the client — that's bidirectional streaming initiated by the plugin.

2. **Context-aware plugins** (pipeline, stream): A model router plugin needs to know the current session state, available models, and user preferences to make routing decisions. It can't be passive — it needs to query the host.

### Design: HostService — The Plugin's Window into Tau

The host exposes a **HostService** gRPC service. Every plugin gets a client for this service during handshake. This is the plugin's API surface for calling back into tau.

```protobuf
// HostService is exposed by tau to all plugins.
// It is the plugin's window into the host.
service HostService {
  // --- Session ---
  rpc GetSessionState(SessionRequest) returns (SessionState);
  rpc ListActiveSessions(ListSessionsRequest) returns (ListSessionsResponse);
  
  // --- Models ---
  rpc GetAvailableModels(ModelsRequest) returns (stream ModelInfo);
  rpc GetModelConfig(ModelConfigRequest) returns (ModelConfig);
  
  // --- Tools ---
  // Register tools that plugins make available to the agent.
  rpc RegisterTools(stream ToolRegistration) returns (ToolRegistrationAck);
  // Called BY the agent THROUGH the host to execute a plugin-registered tool.
  // The host routes to the correct plugin.
  rpc ExecutePluginTool(ToolExecutionRequest) returns (ToolExecutionResponse);
  
  // --- Events ---
  // Subscribe to tau event bus (ChatEvent stream).
  // Plugin chooses: push (host calls plugin) or pull (plugin subscribes).
  rpc SubscribeEvents(EventSubscription) returns (stream ChatEvent);
  
  // --- Notifications ---
  // Plugin can push notifications to the TUI.
  rpc Notify(NotificationRequest) returns (NotificationResponse);
  
  // --- Storage ---
  // Plugin can read/write plugin-scoped key-value storage.
  rpc GetConfig(ConfigRequest) returns (ConfigResponse);
  rpc SetConfig(ConfigRequest) returns (ConfigResponse);
}
```

### Two Communication Patterns

**Pattern A: Hook (Host → Plugin push)** — Existing model. Host calls plugin. Fast, synchronous-ish. Good for: lifecycle events, command execution, pipeline processing.

```
Host → Plugin.DispatchEvent(session_start, ctx)
Host → Plugin.RunCommand("/export", "session-id")
Host → Plugin.PipelineService.ProcessRequest(stream)
```

**Pattern B: Query (Plugin → Host pull)** — New. Plugin calls host. Plugin-initiated, asynchronous. Good for: context lookups, state queries, configuration reads.

```
Plugin → Host.GetSessionState("session-123")           // What's the current session state?
Plugin → Host.GetAvailableModels()                      // What models are available?
Plugin → Host.GetConfig("plugin.my-plugin.api-key")     // Read my API key
```

**Pattern C: Subscribe (Plugin pulls event stream)** — Plugin subscribes to host event bus. Instead of host pushing every event, plugin pulls only what it needs.

```
Plugin → Host.SubscribeEvents(["tool_call_started", "session_shutdown"])
Host → stream(ChatEvent, ChatEvent, ...)  // Plugin receives filtered events
```

This is more efficient than Pattern A for high-frequency events (like token streaming) because the plugin only receives events it subscribed to, not every event the host fires.

**Pattern D: Register + Callback (Plugin registers, Host calls back)** — Plugin registers resources (tools, commands, panels), then host calls back when needed.

```
Plugin → Host.RegisterTools(stream ToolDef, ToolDef, ...)
// Later, when agent wants to call a tool:
Agent → Host (via coordinator) → Host.ExecutePluginTool(plugin, tool, args)
// Host routes to the correct plugin automatically.
```

### Mapping to Plugin Capabilities

| Capability | Push (Host→Plugin) | Pull (Plugin→Host) | Register+Callback |
|-----------|-------------------|-------------------|------------------|
| core | DispatchEvent | GetConfig, Notify | RunCommand |
| provider | ExchangeToken (push) | GetAvailableModels, GetModelConfig | DiscoverModels (stream) |
| stream | ProcessStream (bidir) | — | — |
| tools | ExecuteTool (push) | — | RegisterTools → ExecutePluginTool |
| ui | HandleKey (push) | — | RenderPanel (stream out) |
| audit | — | SubscribeEvents (pull) | — |
| pipeline | ProcessRequest (bidir) | GetSessionState | — |

### Why Both Push and Pull?

**Push is better when**: The host knows exactly when an event happens (session started, tool called, key pressed). The plugin shouldn't have to poll.

**Pull is better when**: The plugin needs context that the host has but doesn't know the plugin needs it (current session state, available models, user config). Or when the plugin wants to filter what it receives (subscribe to specific events, not all).

**Bidirectional streaming is needed when**: Both sides produce data over time (token streaming, pipeline processing, MCP notifications). Neither side is purely a client or server.

### The Full Communication Surface

```
┌─ Tau Host ───────────────────────────────────┐
│                                                │
│  Exposes: HostService                          │
│    GetSessionState ←─── Plugin calls this      │
│    GetAvailableModels ←─── Plugin calls this   │
│    SubscribeEvents ←─── Plugin subscribes      │
│    RegisterTools ←─── Plugin registers         │
│    ExecutePluginTool ←─── Host routes to plugin│
│    Notify ←─── Plugin pushes to TUI            │
│    GetConfig/SetConfig ←─── Plugin storage     │
│                                                │
│  Consumes: ExtensionService (per-plugin)       │
│    DispatchEvent ───→ Plugin receives          │
│    RunCommand ───→ Plugin executes             │
│    Reload ───→ Plugin reloads                  │
│                                                │
│  Consumes: ProviderService (if capability)     │
│  Consumes: StreamService (bidirectional)        │
│  Consumes: PipelineService (bidirectional)      │
│  Consumes: UIService (if capability)           │
│  Consumes: AuditService (if capability)        │
│                                                │
└────────────────────────────────────────────────┘
```

### Security: Plugin Scoping

Not every plugin gets full HostService access. Capabilities gate the available host methods:

| Host method | Required capability |
|------------|-------------------|
| GetSessionState | core (always available) |
| GetAvailableModels | provider |
| RegisterTools | tools |
| SubscribeEvents | audit |
| GetConfig/SetConfig | core |
| Notify | core |

The host checks the plugin's declared capabilities before allowing each call. A core-only plugin can't call RegisterTools. A provider plugin can't call SubscribeEvents.

### What This Enables

**MCP client plugin**: Declares `tools` capability. Calls `Host.RegisterTools()` to register MCP server tools. Agent discovers them. When agent calls a tool, host routes to `Host.ExecutePluginTool()` which calls back to the plugin. The MCP server can also push notifications through the bidirectional stream.

**Context-aware router**: Declares `provider` + `pipeline` capabilities. Before routing a request, calls `Host.GetSessionState()` and `Host.GetAvailableModels()` to make an informed decision. Has full context without the host needing to push it.

**Notification plugin**: Declares `core`. Calls `Host.Notify()` to push status messages to the TUI. "Model router: selected claude-sonnet-4 (cost: $0.002/1K)".

**Persistent plugin state**: Declares `core`. Calls `Host.GetConfig()` to read its API keys and `Host.SetConfig()` to save state between sessions. Plugin-scoped key-value store, persisted to SQLite.

### Plugin Config Schema

Plugins declare their config schema during `GetMetadata()`. The host reads matching config from `~/.config/tau/config.yaml` under a `plugins.<plugin-name>` block and makes it available via `Host.GetConfig()`:

```yaml
# ~/.config/tau/config.yaml
plugins:
  mcp-client:
    mcpServers:
      postgres:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-postgres", "$DATABASE_URL"]
      filesystem:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    auto_discover: true
```

```go
// Plugin declares its config schema at Metadata time.
func (p *MCPPlugin) Metadata() (string, []*proto.Command) {
    return "mcp-client", []*proto.Command{}, proto.ConfigSchema{
        Fields: map[string]proto.ConfigField{
            "mcpServers":   {Type: "object", Required: false},
            "auto_discover": {Type: "bool", Default: "true"},
        },
    }
}

// Later, plugin reads its config:
serversJSON, _ := host.GetConfig(ctx, "mcpServers")
autoDiscover, _ := host.GetConfig(ctx, "auto_discover")
```

**Key properties**:
- Config is **plugin-namespaced** — `plugins.mcp-client` in config.yaml maps to the "mcp-client" plugin
- **No re-parsing** — host validates config against schema at load time, plugin gets pre-validated values
- **Hot reload** — host watches config.yaml for changes, notifies plugins via `Reload()` when their config changes
- **Persistent writes** — `Host.SetConfig()` writes plugin-scoped state to SQLite, not to config.yaml (user config is read-only from the plugin's perspective)


---

## 61. Plugin Discovery via Git Naming Convention

### Status: Design

### Motivation

Plugin binaries sitting in `~/.config/tau/plugins/` requires manual installation. For tau to have an ecosystem, plugins need to be discoverable and installable with minimal friction. The established pattern — used by kubectl, gh CLI, oh-my-zsh, and many others — is a naming convention: `tau-plugin-<name>`.

### Design

**Discovery**: Plugins are Git repositories matching the pattern `tau-plugin-<name>`. They can live anywhere (GitHub, GitLab, self-hosted Gitea). Tau doesn't need a central registry — it searches GitHub, or users specify a repo directly.

**Installation**:

```
tau plugins search mcp           # searches GitHub for "tau-plugin-mcp"
tau plugins install mcp           # git clone → go build → install to ~/.config/tau/plugins/
tau plugins install gh:user/mcp   # install from specific GitHub repo
tau plugins install https://gitlab.example.com/team/tau-plugin-mcp
tau plugins list                  # show installed plugins
tau plugins update mcp            # git pull → rebuild → replace
tau plugins uninstall mcp        # remove binary
```

**Repository structure**:

```
tau-plugin-mcp/
├── main.go          # plugin entry point (implements plugin.Extension)
├── go.mod           # module tau-plugin-mcp
├── go.sum
└── README.md
```

No complex scaffolding — just a single Go binary. The `tau plugins new mcp` command scaffolds this structure.

**How it works**:

1. `tau plugins install mcp` searches GitHub API for repos named `tau-plugin-mcp`
2. If exactly one match, clones to `~/.cache/tau/plugins/src/tau-plugin-mcp`
3. Runs `go build -o ~/.config/tau/plugins/tau-plugin-mcp .`
4. Reloads the plugin manager (new binary appears in plugins dir)
5. Plugin connects, registers tools/commands

**Updates**: `tau plugins update` does `git pull` in the cached source, rebuilds, replaces the binary. Plugin manager detects the change and restarts the plugin.

**Compatibility**: Plugin declares required tau version range in its `Metadata()`. Host checks before loading:

```go
func (p *Plugin) Metadata() (string, []*proto.Command) {
    return "mcp-plugin", nil,
        proto.RequiresTau(">=0.5.0") // host validates
}
```

**Private plugins**: `tau plugins install gh:my-org/private-mcp` works with `gh auth` token. Self-hosted GitLab with `GITLAB_TOKEN` env var.

### Why Not a Central Registry?

Central registries (VS Code marketplace, npm) require infrastructure, moderation, and trust. The Git naming convention is:

- **Decentralized** — no single point of failure or control
- **Already understood** — kubectl, gh, oh-my-zsh users know the pattern
- **Versioned** — Git tags = plugin versions
- **Self-hostable** — enterprises can run their own GitLab with private plugins
- **Zero infra cost** — GitHub search is free

If the ecosystem grows large enough to need curation, an optional index repo (`tau-plugins/index`) can provide a curated list with metadata, but it's additive, not required.

### Comparison: gh CLI Extension Architecture

The gh CLI's extension system (https://github.com/cli/cli/tree/trunk/pkg/extensions) is a model of simplicity:

```go
// The entire extension interface (1K lines of code total in the package):
type Extension interface {
    Name() string           // name without gh- prefix
    Path() string           // path to executable
    URL() string            // repo URL
    CurrentVersion() string
    LatestVersion() string
    IsPinned() bool
    UpdateAvailable() bool
    IsBinary() bool
    IsLocal() bool
    Owner() string
}

type ExtensionManager interface {
    List() []Extension
    Install(ghrepo.Interface, string) error
    InstallLocal(dir string) error
    Upgrade(name string, force bool) error
    Remove(name string) error
    Dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, error)
    Create(name string, tmplType ExtTemplateType) error
    EnableDryRunMode()
    UpdateDir(name string) string
}
```

**Key differences from tau's go-plugin approach**:

| Concern | gh CLI | tau (go-plugin) |
|---------|--------|-----------------|
| Communication | stdin/stdout | gRPC over stdio |
| Process model | One-shot exec per command | Long-lived process |
| Capabilities | Implicit (binary name = subcommand) | Explicit (proto service discovery) |
| Installation | `gh repo clone` + auto-detect on PATH | Binary in plugins dir |
| Interface size | ~10 methods (manager) | ~50+ methods (proto services) |
| Language support | Any language (just needs a binary) | Any gRPC-capable language |

**What tau should adopt from gh**:
- **Naming convention + PATH discovery** — `tau-plugin-*` binaries on PATH
- **Small manager interface** — the manager doesn't need to know about capabilities
- **Official extensions list** — a curated set of GitHub-owned extensions for discovery
- **`Dispatch` with stdin/stdout** — for simple command extensions, gRPC is overkill

**What tau gets from go-plugin that gh doesn't**:
- Rich bidirectional streaming (LLM tokens, pipeline processing)
- Tool registration with the agent
- Event hooks (session lifecycle, tool calls)
- Health checking and auto-restart
- Capability negotiation at handshake time

**The right model for tau**: Both. Simple command extensions use the gh model (binary on PATH, exec on demand). Rich capability plugins use go-plugin (long-lived process, gRPC streaming). A plugin declares its mode in its manifest — the manager routes accordingly.

---

## 62. Rich Event Hook Surface — Complete Lifecycle Coverage

### Status: Design — based on extension-contract-spec + Pi comparison matrix

### Motivation

The current plugin DispatchEvent carries only session_start/session_shutdown as bare map[string]string. Pi has 24 typed events. The user's own extension contract spec defines 10 lifecycle events with typed payloads and response transforms. The gap is massive — real plugins (compliance, model router, PII redactor) need to observe and modify every phase of the agent lifecycle.

### Design: Typed Event Payloads + Response Transforms

Instead of string maps, events carry typed proto oneof payloads. Modifying events return EventResponse that the coordinator merges and applies.

### Event Map

| Event | Fires | Plugin Can |
|-------|-------|------------|
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

