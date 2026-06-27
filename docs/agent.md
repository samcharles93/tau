# Agent Coordinator

The agent coordinator (`internal/agent/coordinator.go`) is the central runtime that implements the `ChatRuntime` interface. It owns the turn loop, tool execution, session state, and plugin lifecycle dispatch. Every client (TUI, Web UI) sends commands to it and receives events from it via the event bus.

## Core Interface

```go
// internal/chat/types.go
type ChatRuntime interface {
    Send(cmd ChatCommand) error
    Close()
}
```

The coordinator receives `ChatCommand` values on an internal channel and publishes `ChatEvent` values on the event bus. It does not know or care whether commands come from the TUI, the Web UI, or a headless stdin client.

## CoordinatorConfig

The coordinator is created with a `CoordinatorConfig`:

```go
type CoordinatorConfig struct {
    TokenSource       TokenSource       // bearer token resolution (legacy)
    Streamer          Streamer          // LLM API call adapter
    Registry          *Registry         // tool registry (from tools package)
    MaxToolIterations int               // max tool-calling rounds per turn
    ParallelToolCalls bool              // whether to run tools in parallel
    ShowReasoning     bool              // emit reasoning deltas to clients
    ExtensionReloader ExtensionReloader // plugin manager
    SessionStore      SessionStore      // session persistence
    OnPluginEvent     func(...)         // plugin lifecycle hook
    ScheduleInterval  time.Duration     // background tick interval
}
```

## Turn Loop

The coordinator runs a `loop()` goroutine that selects on the command channel and schedule ticker. Each turn is processed by `runTurn()`:

1. **Build request** — Build the system prompt (from templates + plugins + skills), collect tool schemas, assemble message history.
2. **Stream** — Call `Streamer.StreamChatCompletionFull()` with the assembled request.
3. **Receive deltas** — Process `OnDelta` (text), `OnReasoningDelta` (reasoning), `OnToolCallDelta` (tool calls) callbacks, emitting corresponding `ChatEvent` values.
4. **Execute tools** — If the model returned tool calls, execute them (parallel or sequential) and loop back to step 2.
5. **Complete** — Emit `ChatResponseCompletedEvent` with final session state.

The turn loop has a `MaxToolIterations` guard (default: 10) to prevent infinite tool-calling loops.

## Command Handlers

| Command | Handler | Description |
| ------- | ------- | ----------- |
| `StartChatSessionCommand` | `handleStart()` | Initializes a new session with config |
| `SubmitChatPromptCommand` | `handleSubmit()` | Submits user prompt, starts a turn |
| `SteerChatPromptCommand` | `handleSteer()` | Steers an in-flight response (injects text) |
| `UpdateChatSessionCommand` | `handleUpdate()` | Patches session settings (model, temperature, etc.) |
| `CancelChatRequestCommand` | `handleCancel()` | Cancels an in-flight LLM request |
| `ResetChatSessionCommand` | `handleReset()` | Resets session to initial state |
| `CloseChatSessionCommand` | `handleClose()` | Closes the session, persists final state |
| `ReloadExtensionsCommand` | `handleReloadExtensions()` | Reloads plugin extensions |
| `RunExtensionCommandCommand` | `handleRunExtensionCommand()` | Runs a plugin slash command |
| `RespondInteractivePromptCommand` | `handleInteractiveResponse()` | Answers a tool's interactive prompt (confirm, select, input) |
| `ListSessionsCommand` | `handleListSessions()` | Lists saved sessions from store |
| `LoadSessionCommand` | `handleLoadSession()` | Loads a saved session's messages |
| `DeleteSessionCommand` | `handleDeleteSession()` | Deletes a saved session |
| `ExportSessionCommand` | `handleExportSession()` | Exports a session as JSONL |

## Event Emission

The coordinator publishes all events on the event bus:

- **`emit(event)`** — Non-blocking publish; if the bus channel is full, the event is dropped (used for streaming deltas).
- **`emitMustDeliver(event)`** — Bounded-blocking publish (500ms timeout); used for terminal events that must not be dropped (session snapshots, completion, errors).

## Streamer Interface

```go
type Streamer interface {
    StreamChatCompletionFull(
        ctx context.Context,
        session chat.ChatSessionState,
        bearerToken string,
        extraHeaders map[string]string,
        cb chat.StreamCallbacks,
    ) (chat.CompletionResult, error)
}
```

The streamer is implemented by `internal/app/streamer.go` as a dynamic adapter that picks the correct ai-sdk provider per turn based on the session's configured provider and model. It supports:
- OpenAI, Anthropic, DeepSeek, Groq, Mistral, Google Gemini, Ollama, xAI, Perplexity, Cohere, Azure
- Streaming responses with tool calls
- Reasoning content (DeepSeek, Anthropic extended thinking)
- Provider-specific header injection (via `extraHeaders`)

## Tool Execution

See [Tools](tools.md) for the full tool system. The coordinator:
1. Collects tool schemas from `Registry.Schemas()`.
2. Sends them in the LLM request as `tools[]`.
3. When the model returns tool calls, dispatches to `Registry.Get(name).Execute()`.
4. Parallel execution uses `executeToolsParallel()` with a `sync.WaitGroup`.

## Prompt Construction

System prompts are built by `internal/agent/prompt.go`:

- **Template files**: `internal/agent/templates/*.md.tpl` — Go text/template templates.
- **Context discovery**: `DiscoverContextFiles()` walks the project directory for `AGENTS.md` and similar context files.
- **Skill prompts**: Skill descriptions and instructions are injected via `skills.ToPromptIndex()`.
- **Plugin prompts**: Plugins can inject additional system prompt text via `EventResponse.InjectSystemPrompt`.

The final prompt is assembled from: base template + project context + skills + plugin injections + user override.

## Session State

The coordinator holds a `chat.ChatSessionState` that includes:
- `SessionID` — unique session identifier
- `Provider`, `Model` — configured provider and model reference
- `Parameters` — temperature, max_tokens, reasoning_effort
- `Messages` — full message history (system, user, assistant, tool)
- `Status` — "idle", "streaming", "error"
- `ActiveRequestID` — current in-flight request if streaming

State is persisted to SQLite on close and after each completed turn by `internal/sessions/`.

## Plugin Integration

The coordinator broadcasts lifecycle events to all loaded plugins:
- `session_start`, `turn_start`, `turn_end`
- `before_llm_call`, `after_llm_call`
- `before_tool_exec`, `after_tool_exec`
- `compaction_before`, `compaction_after`
- `schedule` (periodic background tick)

Plugins can respond with modifications: inject messages, remove messages, append system prompt text, block tools, modify tool arguments/results, add HTTP headers, or change the model ID.

See [Plugin SDK](plugins.md) for the plugin API.
