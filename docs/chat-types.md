# Chat Types Reference

All chat types live in `internal/chat/types.go`. This package defines the command/event contract and is imported by every other subsystem — it has no behavior, only types.

## ChatCommand (TUI/Web → Coordinator)

All commands implement the `ChatCommand` interface (marker: `IsChatCommand()`).

| Command | Fields | Purpose |
| ------- | ------ | ------- |
| `StartChatSessionCommand` | `SessionID`, `Config` | Initialize a new session |
| `SubmitChatPromptCommand` | `SessionID`, `RequestID`, `Prompt`, `SubmittedAt` | Submit user input |
| `SteerChatPromptCommand` | `SessionID`, `RequestID`, `Text` | Inject text during in-flight response |
| `UpdateChatSessionCommand` | `SessionID`, `Patch` | Change model, temperature, system prompt, etc. |
| `CancelChatRequestCommand` | `SessionID`, `RequestID` | Cancel in-flight LLM request |
| `ResetChatSessionCommand` | `SessionID` | Reset to initial session state |
| `CloseChatSessionCommand` | `SessionID` | Close and persist session |
| `ReloadExtensionsCommand` | `SessionID` | Reload all plugin extensions |
| `RunExtensionCommandCommand` | `SessionID`, `Name`, `Args` | Execute a plugin slash command |
| `RespondInteractivePromptCommand` | `RequestID`, `Confirmed`, `Canceled`, `Response` | Answer a tool confirmation/question |
| `ListSessionsCommand` | `Limit`, `Cursor` | List saved sessions |
| `LoadSessionCommand` | `SessionID` | Load a saved session's messages |
| `DeleteSessionCommand` | `SessionID` | Delete a saved session |
| `ExportSessionCommand` | `SessionID`, `Format` | Export session as JSONL |

## ChatEvent (Coordinator → TUI/Web)

All events implement the `ChatEvent` interface (marker: `IsChatEvent()`).

### Session Lifecycle Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `ChatSessionSnapshotEvent` | `State` | Full state sync (on start, after turns, on update) |
| `SessionLoadedEvent` | `State` | After a saved session is loaded |

### Streaming Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `ChatResponseStartedEvent` | `SessionID`, `RequestID`, `StartedAt` | LLM begins generating |
| `ChatResponseDeltaEvent` | `SessionID`, `RequestID`, `Delta`, `Snapshot`, `ReceivedAt` | Each text token |
| `ChatReasoningDeltaEvent` | `SessionID`, `RequestID`, `Delta`, `Snapshot`, `ReceivedAt` | Each reasoning token |
| `ChatResponseCompletedEvent` | `State`, `RequestID`, `FinishReason`, `CompletedAt` | LLM finishes generating |
| `ChatResponseCancelledEvent` | `SessionID`, `RequestID` | User cancels generation |

### Tool Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `ChatToolCallDeltaEvent` | `SessionID`, `RequestID`, `CallID`, `Index`, `ToolName`, `ArgumentsSummary` | Tool call being streamed |
| `ChatToolExecutionStartedEvent` | `SessionID`, `RequestID`, `CallID`, `ToolName`, `ArgumentsSummary`, `StartedAt` | Tool execution begins |
| `ChatToolOutputEvent` | `SessionID`, `RequestID`, `CallID`, `Chunk` | Live stdout chunk from tool |
| `ChatToolExecutionCompletedEvent` | `SessionID`, `RequestID`, `CallID`, `ToolName`, `Status`, `ResultSummary`, `IsError`, `CompletedAt` | Tool execution ends |

### Notification & Error Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `ChatRuntimeErrorEvent` | `SessionID`, `RequestID`, `Message`, `Fatal`, `OccurredAt` | Runtime error |
| `ChatNotificationEvent` | `Message`, `Level` ("info"/"warn"/"error"), `OccurredAt` | Informational notice |

### Interactive Prompt Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `InteractivePromptRequestedEvent` | `RequestID`, `Kind` ("confirm"/"question"), `Title`, `Message`, `RequestedAt` | Tool needs user input |

### Session Management Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `SessionsListedEvent` | `Sessions`, `NextCursor` | Response to ListSessionsCommand |
| `SessionDeletedEvent` | `SessionID` | Response to DeleteSessionCommand |
| `SessionExportedEvent` | `SessionID`, `Format`, `Path` | Response to ExportSessionCommand |

### Extension Events

| Event | Fields | When |
| ----- | ------ | ---- |
| `ExtensionsReloadedEvent` | `Plugins` | After plugin reload |
| `ExtensionCommandsChangedEvent` | `Commands` | Plugin command registry changes |
| `ExtensionCommandResultEvent` | `Name`, `Result` | Result of a plugin command |
| `CommandsChangedEvent` | `Commands` | Full command registry changes |

## Core Types

### ChatSessionState

The complete session state carried in snapshots:

```go
type ChatSessionState struct {
    SessionID      string
    Provider       string
    Model          ChatModelRef
    Status         string            // "idle", "streaming", "error"
    Parameters     ChatParameters
    Messages       []ChatMessage
    PendingAssistant string         // partial in-flight assistant text
    ActiveRequestID  string
    LastUsage      ChatUsage
    SystemPrompt   string
    ReasoningEffort string
    ShowReasoning  bool
}
```

### ChatMessage

```go
type ChatMessage struct {
    Role             ChatRole      // "system", "user", "assistant", "tool"
    Content          string
    ReasoningContent string
    ToolCalls        []ChatToolCall
    ToolCallID       string        // for role "tool" messages
    Name             string        // optional tool/function name
}
```

### ChatToolCall

```go
type ChatToolCall struct {
    ID       string
    Type     string           // always "function"
    Function ChatFunctionCall
}

type ChatFunctionCall struct {
    Name      string
    Arguments string          // JSON string
}
```

### ChatParameters

```go
type ChatParameters struct {
    MaxTokens       int
    Temperature     float64
    ReasoningEffort string
}
```

### ChatModelRef

```go
type ChatModelRef struct {
    ID            string
    URL           string              // provider base URL override
    Config        *ModelConfig        // context window, pricing, capabilities
    ContextWindow int                 // maximum context window in tokens
    Cost          *ChatCost           // per-1M-token pricing
}
```

### ChatCost

```go
type ChatCost struct {
    Input      float64
    Output     float64
    CacheRead  float64
    CacheWrite float64
}
```

### ChatUsage

```go
type ChatUsage struct {
    PromptTokens     int
    CompletionTokens int
    OutputTokens     int
    TotalTokens      int
}
```

### ChatSessionConfig

```go
type ChatSessionConfig struct {
    Provider       string
    Model          ChatModelRef
    SystemPrompt   string
    Parameters     ChatParameters
    ShowReasoning  bool
    ReasoningEffort string
}
```

### ChatSessionPatch

Used in `UpdateChatSessionCommand` to change settings:

```go
type ChatSessionPatch struct {
    Model           *ChatModelRef
    SystemPrompt    *string
    MaxTokens       *int
    Temperature     *float64
    ReasoningEffort *string
    Provider        *string
}
```

## Non-ChatEvent Bus Types

These types are published on the event bus but do not implement `ChatEvent`:

### ScheduleTickEvent

Published at a configurable interval for background work (plugin scheduling):

```go
type ScheduleTickEvent struct {
    OccurredAt time.Time
}
```

### PluginLifecycleEvent

Published for plugin lifecycle notifications:

```go
type PluginLifecycleEvent struct {
    Event     string
    SessionID string
    Payload   any  // *api.EventPayload at rest
}
```

## StreamCallbacks

The streaming callback interface used by `Streamer`:

```go
type StreamCallbacks struct {
    OnDelta          func(delta, snapshot string)
    OnReasoningDelta func(delta, snapshot string)
    OnToolCallDelta  func(delta ChatToolCallDelta)
}
```

## CommandRef

Used by the command registry and TUI for slash command autocomplete:

```go
type CommandRef struct {
    Name        string   // e.g., "/model", "/plugin:hello:greet"
    Label       string   // human-readable label
    Description string
    AcceptsArgs bool
}
```

## Extension Types

### ExtensionCommand

A slash command provided by a plugin:

```go
type ExtensionCommand struct {
    Name          string
    Description   string
    ExtensionName string
}
```

### ExtensionReloader

Interface implemented by the plugin manager:

```go
type ExtensionReloader interface {
    ReloadExtensions(ctx context.Context, idle bool) (ExtensionReloadResult, error)
    ExtensionCommands() []ExtensionCommand
    RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error)
}
```

## SessionSummary

```go
type SessionSummary struct {
    ID           string
    ModelID      string
    Provider     string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Status       string
    MessageCount int
    TotalTokens  int
    Cost         float64
}
```

## WebSocket Wire Protocol

The `Envelope` wrapper used on the WebSocket between the Go bridge and the Vue SPA:

```go
type Envelope struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

The TypeScript protocol types are mirrored in `internal/webui/src/lib/protocol.ts`. See [Server & Bridge](server.md) for the wire format details.
