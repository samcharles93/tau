# Terminal UI (TUI)

Tau's terminal UI is a reactive, inline-rendering chat interface built on the `pkg/taui` framework. It renders directly into the terminal scrollback (no alternate screen) and communicates with the coordinator through the event bus.

## Architecture

```
internal/tui/
├── inline_chat.go       # Root component — event loop, rendering, tool display
├── run.go               # Run() entry point, delegates to RunInline
├── run_taui.go          # RunInline — taui bootstrap, event subscription, cleanup
├── api.go               # TUIConfig struct, ModelRefresher type
├── inline_commands.go   # Slash command table
├── inline_completions.go # Tab-completion engine
└── notify/
    └── notify.go        # Queue-based notification system
```

## Lifecycle

1. `app.RunChat()` creates the coordinator and `TUIConfig`, then calls `tui.Run()`.
2. `Run()` delegates to `RunInline()`, which:
   - Creates the `taui.TUI` engine
   - Subscribes to `ChatEvent` on the event bus (client name `"tui"`)
   - Creates an `inlineChat` instance
   - Enters the taui render loop
3. `inlineChat` starts three goroutines:
   - `eventLoop()` — receives `ChatEvent` from the bus, dispatches to `onRuntimeEvent()`
   - `spinnerLoop()` — animates the working indicator while the model is generating
   - `statusLoop()` — rotates through queued notifications

## inlineChat State

The `inlineChat` struct holds all UI state:

| Field | Purpose |
| ----- | ------- |
| `provider` | Current provider name |
| `modelName` | Current model ID |
| `sessionID` | Session identifier |
| `showReasoning` | Whether reasoning content is displayed |
| `availableModels` | Model list for `/model` completions |
| `availableProviders` | Provider list for provider switching |
| `registryCommands` | Command registry entries |
| `extensionCommands` | Plugin command entries |
| `sessionSummaries` | Session list for `/sessions` |
| `turnText` | Accumulated assistant text for current turn |
| `turnReasoning` | Accumulated reasoning text for current turn |
| `activeTools` | Map of in-progress tool calls |
| `working` | Whether a request is in flight |
| `running` | Whether the TUI is running |
| `webURL` | Web UI URL to display in status bar |

## Event Handling

`onRuntimeEvent()` routes each `ChatEvent` type to state updates:

| Event | State Update |
| ----- | ------------ |
| `ChatSessionSnapshotEvent` | Sync full session state, update message history |
| `ChatResponseStartedEvent` | Set `working = true`, clear `turnText`/`turnReasoning` |
| `ChatResponseDeltaEvent` | Append to `turnText` |
| `ChatReasoningDeltaEvent` | Append to `turnReasoning` |
| `ChatToolCallDeltaEvent` | Update tool name/args in `activeTools` |
| `ChatToolExecutionStartedEvent` | Add tool to `activeTools` with "running" status |
| `ChatToolOutputEvent` | Append live tool output |
| `ChatToolExecutionCompletedEvent` | Set tool status to "ok" or "error" |
| `ChatResponseCompletedEvent` | Set `working = false`, finalize message |
| `ChatRuntimeErrorEvent` | Show error notification |
| `ChatNotificationEvent` | Queue notification |
| `InteractivePromptRequestedEvent` | Show inline confirm/input prompt |
| `SessionsListedEvent` | Update `sessionSummaries` |
| `CommandsChangedEvent` | Update `registryCommands` |

## Rendering

The inline render uses taui widgets:

- **`taui.Box`** — Layout container (vertical/horizontal stacking)
- **`taui.Text`** — Styled text spans with color, bold, italic
- **`taui.Paragraph`** — Word-wrapped text blocks
- **`taui.LineInput`** — Single-line text input with history
- **`taui.Completions`** — Drop-down autocomplete menu
- **`taui.ToolRow`** — Tool call status display with icons
- **`taui.Loader`** — Animated spinner

Each turn is rendered as:
1. User message (prefixed with `>`)
2. Reasoning content (if `showReasoning` is enabled, dimmed color)
3. Tool calls (each as a `ToolRow` with status icons and truncated arguments)
4. Assistant response text
5. Completion indicator (model name, token count, timing)

## Slash Commands

All slash commands are defined in `internal/tui/inline_commands.go` as a `slashCommand` slice. Each entry has:

```go
type slashCommand struct {
    name        string
    aliases     []string
    usage       string
    description string
    run         func(args string) error
    complete    func(token string) []string
}
```

### Built-in Commands

| Command | Aliases | Description |
| ------- | ------- | ----------- |
| `/model` | — | Switch the active model |
| `/system` | — | View or edit the system prompt |
| `/temperature` | — | Set sampling temperature |
| `/max-tokens` | — | Set max completion tokens |
| `/reset` | — | Reset the session |
| `/reasoning` | `/think` | Toggle reasoning visibility or set effort |
| `/refresh` | — | Refresh the models.dev catalog |
| `/sessions` | `/history` | List saved sessions |
| `/export` | — | Export session as JSONL |
| `/help` | `/?` | Show command help |
| `/debug` | — | Toggle debug mode |
| `/provider` | — | Toggle a provider on/off, or `/provider login <name>` for OAuth |
| `/quit` | `/exit` | Exit tau |

### Extension Commands

Plugin slash commands are dynamically added to the table when plugins are loaded. They appear as `/plugin:<name>:<command>`.

## Tab Completions

`internal/tui/inline_completions.go` provides dynamic tab completions:

1. If input starts with `/`, complete command names from the registry.
2. For `/model`, complete model IDs from `availableModels`.
3. For `/sessions`, complete session IDs.
4. For boolean toggles, complete `on`/`off`.
5. For model names without `/` prefix, complete from available models.

The completions use `taui.CompletionSet` and fuzzy-match against the current token under the cursor.

## Notifications

`internal/tui/notify/notify.go` implements a queue-based notification system:

- Notifications are queued with a `Level` (info, warn, error) and expiry time.
- The `statusLoop()` goroutine rotates through queued notifications at ~3-second intervals.
- Notifications are rendered in the status bar area.
- Expired notifications are automatically removed.

## Model Refresher

The `ModelRefresher` type (`internal/tui/api.go`) is a function that the TUI can call to re-discover available models:

```go
type ModelRefresher func(ctx context.Context) ([]chat.ChatModelRef, error)
```

It's called by `/refresh` and on demand when the model list needs updating.

## TUIConfig

```go
type TUIConfig struct {
    SessionID          string
    ModelName          string
    Provider           string
    AvailableModels    []chat.ChatModelRef
    AvailableProviders []string
    InitialCommands    []chat.CommandRef
    Bus                *eventbus.Bus
    RefreshModels      ModelRefresher
    ShowReasoning      bool
    ReasoningEffort    string
    Debug              bool
    WebURL             string           // Web UI URL to display
}
```
