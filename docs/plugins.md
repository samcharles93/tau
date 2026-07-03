# Tau Plugin SDK

Tau's plugin system lets you extend the chat runtime with custom tools, slash
commands, and lifecycle event hooks. Plugins are self-contained Go binaries
that communicate with Tau over gRPC using [HashiCorp
go-plugin](https://github.com/hashicorp/go-plugin).

Plugins live in `~/.config/tau/plugins/` (or the directory configured via the
`PluginsDir` option). Tau discovers and launches them automatically at startup.
Use `/reload` to rediscover plugins without restarting Tau.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Plugin Lifecycle](#plugin-lifecycle)
3. [The Extension Interface](#the-extension-interface)
4. [Handshake & Bootstrap](#handshake--bootstrap)
5. [Declaring Capabilities (Optional)](#declaring-capabilities-optional)
6. [Tools (Agent-Callable Functions)](#tools-agent-callable-functions)
7. [Slash Commands (User-Callable)](#slash-commands-user-callable)
8. [Lifecycle Events](#lifecycle-events)
9. [EventResponse — Modifying Runtime Behaviour](#eventresponse--modifying-runtime-behaviour)
10. [HostService — Calling Back Into Tau](#hostservice--calling-back-into-tau)
11. [Panels and Views — Rendering Structured UI](#panels-and-views-rendering-structured-ui)
12. [Plugin Configuration](#plugin-configuration)
13. [Building, Installing, and go.mod](#building-installing-and-gomod)
14. [Complete Working Example](#complete-working-example)
15. [API Reference](#api-reference)
16. [Troubleshooting](#troubleshooting)

---

## Quick Start

The fastest way to understand the API is to read and run the
[hello plugin](https://github.com/samcharles93/tau/blob/main/examples/plugins/hello/main.go).

```bash
# Build the example plugin
cd examples/plugins/hello
go build -o tau-plugin-hello .

# Install it
mkdir -p ~/.config/tau/plugins
cp tau-plugin-hello ~/.config/tau/plugins/

# Run tau and use the plugin
tau
```

Inside Tau, type `/hello world` or ask the agent to call the `hello_greet` tool:

```
Greet me using the hello plugin
```

The hello plugin also demonstrates panels: `/hello panel` renders a one-shot
view, and `/hello watch` opens a live panel you can re-run to update in place
(`/hello close` closes it). See
[Panels and Views](#panels-and-views-rendering-structured-ui).

---

## Plugin Lifecycle

1. **Discovery** — Tau scans `~/.config/tau/plugins/` for executable files.
2. **Launch** — Each plugin binary is started as a subprocess. Communication
   happens over gRPC with a negotiated handshake.
3. **Capability check** — Tau calls `GetCapabilities()` to learn what the
   plugin provides. Plugins that don't implement the optional `Capable`
   interface default to the full legacy surface (commands + tools + events).
4. **Init** — Tau calls `Init()` and hands the plugin a broker ID so it can
   dial the host's `HostService` for config, session state, and notifications.
5. **Metadata** — Tau calls `Metadata()` to learn the plugin's name and slash
   commands. Commands are registered in the TUI/Web UI command palette.
6. **Tool discovery** — If the plugin advertises `CapabilityTools`, Tau calls
   `Tools()` and registers each tool in the agent tool registry as
   `plugin:<plugin-name>:<tool-name>`. The agent sees only `<tool-name>`.
7. **Runtime** — Tau forwards slash commands and tool calls to the plugin, and
   dispatches lifecycle events via `DispatchEvent()`.
8. **Unload** — On shutdown or `/reload`, Tau calls `client.Kill()` on the
   go-plugin process and unregisters all tools belonging to the plugin.

---

## The Extension Interface

A plugin implements `github.com/samcharles93/tau/pkg/plugin/api.Extension`:

```go
type Extension interface {
    Metadata() (name string, commands []*Command)
    RunCommand(ctx context.Context, name, args string) (output string, view *View, err error)
    Reload(ctx context.Context) (diagnostics []*Diagnostic, commands []*Command, err error)
    Tools(ctx context.Context) ([]*ToolDefinition, error)
    ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error)
    DispatchEvent(ctx context.Context, event string, sessionID string, payload *EventPayload) *EventResponse
}
```

All six methods are required. Return empty slices / nil / `""` for
capabilities your plugin does not support. `RunCommand`'s `view` return is
optional — see [Panels and Views](#panels-and-views-rendering-structured-ui)
for what it does and when to use it instead of, or alongside, `output`.

### Method Reference

| Method | Called when | Must return |
|--------|-------------|-------------|
| `Metadata()` | On plugin load and after `/reload` | `(pluginName, []*Command)` |
| `RunCommand(ctx, name, args)` | User invokes a slash command | `(output, view, error)` |
| `Reload(ctx)` | On `/reload` | `(diagnostics, updatedCommands, error)` |
| `Tools(ctx)` | On plugin load (if `CapabilityTools`) | `([]*ToolDefinition, error)` |
| `ExecuteTool(ctx, toolName, args)` | Agent calls a tool | `(content, isError, error)` |
| `DispatchEvent(ctx, event, sessionID, payload)` | Lifecycle event occurs | `*EventResponse` (or nil) |

---

## Handshake & Bootstrap

Every plugin binary must serve itself with this exact configuration so Tau
recognises it during the handshake:

```go
package main

import (
    "github.com/hashicorp/go-hclog"
    "github.com/hashicorp/go-plugin"
    pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
)

func main() {
    hclogger := hclog.New(&hclog.LoggerOptions{
        Level:      hclog.Info,
        Output:     os.Stderr,
        JSONFormat: false,
        Name:       "tau-plugin-myplugin",
    })

    myPlugin := &MyPlugin{}

    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: plugin.HandshakeConfig{
            ProtocolVersion:  1,
            MagicCookieKey:   "TAU_PLUGIN",
            MagicCookieValue: "tau",
        },
        Plugins: map[string]plugin.Plugin{
            "extension": &pluginapi.ExtensionPlugin{Impl: myPlugin},
        },
        GRPCServer: plugin.DefaultGRPCServer,
        Logger:     hclogger,
    })
}
```

### Key Constraints

- `ProtocolVersion` MUST be `1`.
- `MagicCookieKey` MUST be `"TAU_PLUGIN"`.
- `MagicCookieValue` MUST be `"tau"`.
- The plugin map key MUST be `"extension"`.
- `GRPCServer` MUST be `plugin.DefaultGRPCServer`.
- The `ExtensionPlugin` contains your `Impl` (your `Extension` implementation).

If any of these values differ, Tau will reject the plugin with a handshake
error logged to `~/.config/tau/tau.log`.

---

## Declaring Capabilities (Optional)

Plugins can optionally advertise which capabilities they provide by
implementing the `Capable` interface:

```go
type Capable interface {
    Capabilities() []string
}
```

Capability constants:

| Constant | Value | Meaning |
|----------|-------|---------|
| `api.CapabilityCommands` | `"commands"` | Plugin provides slash commands |
| `api.CapabilityTools` | `"tools"` | Plugin provides agent tools |
| `api.CapabilityEvents` | `"events"` | Plugin handles lifecycle events |
| `api.CapabilityViews` | `"views"` | Plugin renders panels (see [Panels and Views](#panels-and-views-rendering-structured-ui)) |

Plugins that do NOT implement `Capable` are assumed to support the full legacy
surface (commands + tools + events). Tau skips unsupported calls at runtime,
which avoids unnecessary gRPC round-trips. `CapabilityViews` is the one
exception to this default: since rendering UI is a net-new surface, it is
**never** assumed for plugins that don't implement `Capable` — you must
declare it explicitly to use panels.

Example:

```go
func (p *MyPlugin) Capabilities() []string {
    return []string{api.CapabilityCommands, api.CapabilityTools}
    // This plugin does NOT handle events; DispatchEvent will never be called.
}
```

---

## Tools (Agent-Callable Functions)

Tools are functions the LLM agent can call during a turn. They are advertised
by `Tools()` and executed by `ExecuteTool()`.

### Declaring a Tool

```go
func (p *MyPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    schema, _ := json.Marshal(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "path": map[string]any{
                "type":        "string",
                "description": "Absolute path to the file to read",
            },
            "max_lines": map[string]any{
                "type":        "integer",
                "description": "Maximum lines to return (default: 500)",
                "default":     500,
            },
        },
        "required": []string{"path"},
    })

    return []*pluginapi.ToolDefinition{{
        Name:        "read_file",
        Description: "Read the contents of a file from disk. Returns the file text.",
        InputSchema: string(schema),
    }}, nil
}
```

### ToolDefinition Fields

| Field | Type | Description |
|-------|------|-------------|
| `Name` | `string` | Tool identifier. Must be unique within the plugin. The agent sees `plugin:<pluginName>.<Name>` internally but the model uses just `<Name>`. |
| `Description` | `string` | Human-readable description. Sent to the LLM — write it for the model, not the user. Include return format, side effects, and constraints. |
| `InputSchema` | `string` | JSON Schema object serialised as a string. Describes the parameters the LLM must provide. Must be valid JSON. |

### Tool Naming and Registration

When Tau registers your tool, the internal name becomes `plugin:<pluginName>.<toolName>`:

- If your plugin's `Metadata()` returns name `"github"` and `Tools()` returns a tool named `"list_issues"`, the registered name is `plugin:github.list_issues`.
- The model sees `list_issues` in the function-calling request (the prefix is stripped).
- Tool execution is routed back to your plugin's `ExecuteTool()` with `toolName` as `"list_issues"` (prefix already stripped).

### Tool InputSchema Best Practices

1. **Always include `"type": "object"`** at the root.
2. **Use `"required"`** to declare which fields are mandatory.
3. **Provide `"description"`** for every property — the LLM reads these.
4. **Use `"default"`** for optional fields so the model knows what happens when omitted.
5. **Keep the schema focused.** Too many parameters confuse the model. 2-5 is ideal.
6. **Use `"enum"`** to constrain choices where possible.
7. **Document return format** in the tool description (e.g., "Returns JSON with keys: ...").

### Executing a Tool

```go
func (p *MyPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    // Parse the JSON arguments into a typed struct.
    var args struct {
        Path     string `json:"path"`
        MaxLines int    `json:"max_lines"`
    }
    if err := json.Unmarshal([]byte(arguments), &args); err != nil {
        return "", true, fmt.Errorf("parse arguments: %w", err)
    }
    // Apply defaults.
    if args.MaxLines <= 0 {
        args.MaxLines = 500
    }
    // Execute the tool logic.
    data, err := os.ReadFile(args.Path)
    if err != nil {
        // isError: true → Tau renders this as a failed tool call.
        return "", true, fmt.Errorf("read file: %w", err)
    }
    lines := strings.Split(string(data), "\n")
    if len(lines) > args.MaxLines {
        lines = lines[:args.MaxLines]
    }
    // Return the result. For structured data, return JSON.
    result, _ := json.Marshal(map[string]any{
        "lines":      lines,
        "total_lines": len(lines),
    })
    return string(result), false, nil
}
```

### ExecuteTool Return Values

| Value | Meaning |
|-------|---------|
| `content` | Tool result string sent to the agent. For structured data, use JSON. For text, use plain text. |
| `isError` | `true` if the tool failed. Tau renders this as a failed tool call with an error icon. |
| `err` | Non-nil only for gRPC transport errors. `isError: true` is the correct way to signal tool failure. |

### Timeout

Plugin tool execution has a configurable timeout (default: 30 seconds). The
`context.Context` passed to `ExecuteTool` is cancelled when the timeout is
reached. Tools performing network calls or heavy I/O should respect context
cancellation.

---

## Slash Commands (User-Callable)

Slash commands appear in the TUI's `/`-triggered autocomplete menu and the
Web UI's command palette.

### Declaring Commands

```go
func (p *MyPlugin) Metadata() (string, []*pluginapi.Command) {
    return "my-plugin", []*pluginapi.Command{
        {
            Name:          "status",
            Description:   "Show plugin status and health: /status",
            ExtensionName: "my-plugin",
        },
        {
            Name:          "config",
            Description:   "Show or set plugin configuration: /config [key] [value]",
            ExtensionName: "my-plugin",
        },
    }
}
```

### Command Fields

| Field | Type | Description |
|-------|------|-------------|
| `Name` | `string` | Command name **without** the leading `/` (the TUI strips it before matching). Convention: `<verb>`, e.g. `status`, not `/status`. |
| `Description` | `string` | Shown in the command palette. Include usage hints (with the `/`). |
| `ExtensionName` | `string` | Must match the plugin name returned by `Metadata()`. |

### Executing Commands

```go
func (p *MyPlugin) RunCommand(ctx context.Context, name, args string) (string, *pluginapi.View, error) {
    switch name {
    case "status":
        return "✅ MyPlugin is healthy. Tools registered: 3.", nil, nil
    case "config":
        if args == "" {
            return "Current config:\n  endpoint: https://api.example.com\n  retries: 3", nil, nil
        }
        parts := strings.SplitN(args, " ", 2)
        key := parts[0]
        if len(parts) > 1 {
            // Set config value.
            return fmt.Sprintf("Set %s = %s", key, parts[1]), nil, nil
        }
        return fmt.Sprintf("Key %q not found", key), nil, nil
    default:
        return "", nil, fmt.Errorf("unknown command: %s", name)
    }
}
```

---

## Lifecycle Events

Tau dispatches lifecycle events to plugins via `DispatchEvent()`. Each event
carries a typed payload in the `EventPayload` oneof.

### Event Table

| Event String | When | Payload Field | `EventPayload` Getter |
|-------------|------|---------------|----------------------|
| `"session_start"` | A chat session starts | `Session` | `payload.GetSession()` |
| `"context"` | Before every LLM turn (full message list) | `Context` | `payload.GetContext()` |
| `"before_llm_call"` | Right before the HTTP request is sent to the LLM | `BeforeLlmCall` | `payload.GetBeforeLlmCall()` |
| `"after_llm_call"` | After the LLM response finishes | `AfterLlmCall` | `payload.GetAfterLlmCall()` |
| `"before_tool_exec"` | Before a tool is executed | `BeforeToolExec` | `payload.GetBeforeToolExec()` |
| `"after_tool_exec"` | After a tool finishes | `AfterToolExec` | `payload.GetAfterToolExec()` |
| `"message_delta"` | On each streamed token | `MessageDelta` | `payload.GetMessageDelta()` |
| `"turn_start"` | At the beginning of a turn | `Turn` | `payload.GetTurn()` |
| `"turn_end"` | At the end of a turn | `Turn` | `payload.GetTurn()` |
| `"compaction_before"` | Before context compaction | `Compaction` | `payload.GetCompaction()` |
| `"compaction_after"` | After context compaction | `Compaction` | `payload.GetCompaction()` |
| `"schedule"` | Periodically (requires `TAU_SCHEDULE_INTERVAL`) | none | `nil` |

### Payload Type Reference

#### SessionEventPayload (event: `"session_start"`)

```go
type SessionEventPayload struct {
    SessionId string // UUID of the new session
    ModelId   string // Model ID (e.g., "deepseek-chat")
    Provider  string // Provider name (e.g., "deepseek")
}
```

#### ContextPayload (event: `"context"`)

```go
type ContextPayload struct {
    Messages []string // JSON-encoded chat.ChatMessage list
}
```

Each element in `Messages` is a JSON string. Unmarshal with
`json.Unmarshal([]byte(msg), &chat.ChatMessage{})`.

#### BeforeLLMCallPayload (event: `"before_llm_call"`)

```go
type BeforeLLMCallPayload struct {
    ModelId    string            // Model being called
    Messages   []string          // JSON-encoded ChatMessage list (the full context)
    Headers    map[string]string // HTTP headers that will be sent
    Parameters string            // JSON-encoded ChatParameters
}
```

This is the most powerful event for modifying LLM behaviour. You can:
- Inject or remove messages from context via `EventResponse.InjectMessages` / `RemoveMessageIndices`.
- Add HTTP headers via `EventResponse.AddHeaders`.
- Override the model ID via `EventResponse.ModifiedModelId`.
- Append system prompt text via `EventResponse.InjectSystemPrompt`.

#### AfterLLMCallPayload (event: `"after_llm_call"`)

```go
type AfterLLMCallPayload struct {
    ModelId      string // Model that was called
    FinishReason string // "stop", "length", "tool_calls", etc.
    Usage        string // JSON-encoded ChatUsage
}
```

#### ToolCallPayload (events: `"before_tool_exec"`)

```go
type ToolCallPayload struct {
    ToolName  string // Name of the tool about to execute
    Arguments string // JSON-encoded tool arguments
    CallId    string // Unique call identifier
}
```

You can block tool execution via `EventResponse.BlockToolExecution = true` and
rewrite arguments via `EventResponse.ModifiedToolArguments`.

#### ToolResultPayload (events: `"after_tool_exec"`)

```go
type ToolResultPayload struct {
    ToolName  string // Name of the tool that executed
    Arguments string // JSON-encoded arguments that were used
    Result    string // JSON-encoded tool result
    IsError   bool   // Whether the tool reported an error
    CallId    string // Unique call identifier
    Duration  string // Execution duration
}
```

You can rewrite the result via `EventResponse.ModifiedToolResult`.

#### MessageDeltaPayload (event: `"message_delta"`)

```go
type MessageDeltaPayload struct {
    Role     string // "assistant"
    Delta    string // The new token text
    Snapshot string // Full accumulated text so far
}
```

#### TurnPayload (events: `"turn_start"`, `"turn_end"`)

```go
type TurnPayload struct {
    Direction  string // "start" or "end"
    TurnNumber int32  // 1-based turn counter
}
```

#### CompactionPayload (events: `"compaction_before"`, `"compaction_after"`)

```go
type CompactionPayload struct {
    Direction          string // "before" or "after"
    MessageCountBefore int32  // Message count before compaction
    MessageCountAfter  int32  // Message count after compaction
}
```

### Event Handler Pattern

```go
func (p *MyPlugin) DispatchEvent(ctx context.Context, event, sessionID string, payload *pluginapi.EventPayload) *pluginapi.EventResponse {
    switch event {
    case "session_start":
        sess := payload.GetSession()
        p.logger.Info("session started", "id", sess.SessionId, "model", sess.ModelId)

    case "before_llm_call":
        llm := payload.GetBeforeLlmCall()
        // Example: inject a custom header.
        return &pluginapi.EventResponse{
            AddHeaders: map[string]string{
                "X-Custom-Header": "my-value",
            },
        }

    case "before_tool_exec":
        tool := payload.GetBeforeToolExec()
        // Example: block a specific tool.
        if tool.ToolName == "shell" {
            return &pluginapi.EventResponse{
                BlockToolExecution: true,
                BlockReason:        "Shell commands are disabled by policy",
            }
        }

    case "after_tool_exec":
        result := payload.GetAfterToolExec()
        // Example: rewrite tool output to censor sensitive data.
        if strings.Contains(result.Result, "SECRET") {
            return &pluginapi.EventResponse{
                ModifiedToolResult: `{"censored": true, "reason": "sensitive data removed"}`,
            }
        }

    case "turn_start":
        turn := payload.GetTurn()
        p.logger.Info("turn", "dir", turn.Direction, "num", turn.TurnNumber)

    case "schedule":
        // Periodic background work. Payload is nil.
        if err := p.heartbeat(ctx); err != nil {
            return &pluginapi.EventResponse{
                Diagnostics: []*pluginapi.Diagnostic{{
                    Severity: "warning",
                    Message:  fmt.Sprintf("Heartbeat failed: %v", err),
                }},
            }
        }
    }
    return nil // No response → no modification.
}
```

### Event Delivery

- Events are dispatched to all loaded plugins **in plugin load order**.
- Each plugin has a per-event timeout (default: 10 seconds). If a plugin
  takes longer, its dispatch is cancelled and a warning is logged.
- Responses from multiple plugins are **merged**:
  - `InjectMessages`, `RemoveMessageIndices`, `Diagnostics` are concatenated.
  - `InjectSystemPrompt` is joined with newlines.
  - `AddHeaders` maps are merged (later plugins win on key collision).
  - `BlockToolExecution` is OR'd (first plugin to block wins).
  - `ModifiedToolArguments` / `ModifiedToolResult` from the last responding
    plugin wins.
  - `SuppressDefault` is OR'd.
- Plugins that do not advertise `CapabilityEvents` are never called.

---

## EventResponse — Modifying Runtime Behaviour

The `EventResponse` struct is the primary mechanism for plugins to influence
the coordinator at runtime.

```go
type EventResponse struct {
    // InjectMessages adds JSON-encoded chat.ChatMessage values to the
    // conversation context before the LLM call.
    InjectMessages []string

    // RemoveMessageIndices specifies 0-based indices of messages to remove
    // from the context before the LLM call.
    RemoveMessageIndices []int32

    // InjectSystemPrompt is appended to the system prompt.
    InjectSystemPrompt string

    // ModifiedToolArguments replaces the tool arguments (JSON string) when
    // returned from a before_tool_exec handler.
    ModifiedToolArguments string

    // BlockToolExecution prevents a tool from running. Only meaningful from
    // before_tool_exec handlers.
    BlockToolExecution bool
    BlockReason        string // Shown to the user when the tool is blocked.

    // ModifiedToolResult replaces the tool result (JSON string) when
    // returned from an after_tool_exec handler.
    ModifiedToolResult string

    // AddHeaders injects HTTP headers into the LLM request. Only meaningful
    // from before_llm_call handlers.
    AddHeaders map[string]string

    // ModifiedModelId overrides the model ID for this request. Only
    // meaningful from before_llm_call handlers.
    ModifiedModelId string

    // Diagnostics are shown to the user in the TUI/Web UI.
    Diagnostics []*Diagnostic

    // SuppressDefault prevents Tau's default handling of this event.
    // Use with caution — most plugins should leave this false.
    SuppressDefault bool
}
```

### Event → Response Field Compatibility

Not every response field is valid for every event. The coordinator checks
the event type and only applies relevant fields:

| Response Field | Valid Events |
|---------------|--------------|
| `InjectMessages` | `context`, `before_llm_call` |
| `RemoveMessageIndices` | `context`, `before_llm_call` |
| `InjectSystemPrompt` | `context`, `before_llm_call` |
| `AddHeaders` | `before_llm_call` |
| `ModifiedModelId` | `before_llm_call` |
| `BlockToolExecution` / `BlockReason` | `before_tool_exec` |
| `ModifiedToolArguments` | `before_tool_exec` |
| `ModifiedToolResult` | `after_tool_exec` |
| `Diagnostics` | Any event |
| `SuppressDefault` | Any event |

---

## HostService — Calling Back Into Tau

Plugins can call back into the Tau host process via the `HostService` gRPC
service. Implement the `HostAware` interface to receive a `Host` handle:

```go
type HostAware interface {
    SetHost(h Host)
}
```

The `Host` interface provides:

```go
type Host interface {
    // GetConfig reads the plugin's config block. key="" returns the entire block.
    GetConfig(ctx context.Context, key string) (value string, found bool, err error)

    // SetConfig persists a config value for this plugin.
    SetConfig(ctx context.Context, key, value string) error

    // GetSessionState returns the JSON-encoded chat session state.
    GetSessionState(ctx context.Context, sessionID string) (stateJSON string, found bool, err error)

    // GetAvailableModels lists model IDs the host knows about.
    GetAvailableModels(ctx context.Context) ([]string, error)

    // Notify pushes a user-visible notification to the TUI and Web UI.
    Notify(ctx context.Context, level, message string) error

    // Log forwards a structured log line to the host logger.
    Log(ctx context.Context, level, message string, fields map[string]string) error

    // RenderView opens or updates a persistent panel in the host UI. See
    // Panels and Views below.
    RenderView(ctx context.Context, view *View) error

    // CloseView closes a panel previously opened via RenderView.
    CloseView(ctx context.Context, viewID string) error
}
```

### Example: Host-Aware Plugin

```go
type MyPlugin struct {
    host   pluginapi.Host
    logger *slog.Logger
}

// SetHost is called once during Init, before any command or tool runs.
func (p *MyPlugin) SetHost(h pluginapi.Host) {
    p.host = h
}

func (p *MyPlugin) RunCommand(ctx context.Context, name, args string) (string, *pluginapi.View, error) {
    if name == "models" && p.host != nil {
        models, err := p.host.GetAvailableModels(ctx)
        if err != nil {
            return "", nil, err
        }
        return "Available models:\n" + strings.Join(models, "\n"), nil, nil
    }
    // ... other commands
}

func (p *MyPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    // Read plugin config from the host.
    if p.host != nil {
        cfg, found, _ := p.host.GetConfig(ctx, "")
        if found {
            p.logger.Info("plugin config loaded", "config", cfg)
        }
    }
    // ... execute tool
}
```

### HostService Details

- `SetHost()` is called by the gRPC adapter during `Init()`. It is called
  **once**, before any command or tool execution.
- If your plugin does not implement `HostAware`, `SetHost()` is never called.
- `GetConfig()` / `SetConfig()` are scoped to the plugin's name. The host reads
  from the `plugins.<pluginName>` block in `config.yaml`. `SetConfig()`
  persists to `~/.config/tau/plugin-state.json`.
- `GetSessionState()` with an empty `sessionID` targets the currently active
  session. The returned JSON matches `chat.ChatSessionState`.
- `Notify()` pushes transient notifications to both the TUI (via the notify
  queue) and the Web UI (via toast container). Valid levels: `"info"`,
  `"warn"`, `"error"`.
- `Log()` forwards to tau's structured logger (`slog`). Use this instead of
  writing to stdout/stderr for log messages that should appear in the host's
  log output.
- `RenderView()` / `CloseView()` push structured panels to the TUI outside of
  a command invocation. See [Panels and Views](#panels-and-views-rendering-structured-ui).

---

## Panels and Views — Rendering Structured UI

Beyond plain-text command output, notifications, and tool results, plugins
can render structured panels — key/value summaries, tables, lists, progress
bars — directly into the TUI. A panel is a `View`: a tree of `Widget`s
identified by an `Id` that's scoped to your plugin.

There are two ways to deliver a `View`:

1. **Sync** — return it from `RunCommand`. It renders once, in place of (or
   alongside) the plain-text `output`, when that command completes.
2. **Async** — push it any time via `Host.RenderView`, independent of command
   invocation. Useful for live-updating panels (a dashboard, a log tail).
   Re-sending a `View` with the same `Id` replaces its content in place; call
   `Host.CloseView` to remove it. Async panels persist until closed, until
   your plugin is unloaded/reloaded (Tau closes them for you), or until Tau
   exits — there's no cross-restart persistence.

Panels are opt-in: declare `api.CapabilityViews` in `Capabilities()` before
using either path (see [Declaring Capabilities](#declaring-capabilities-optional)).

### The View and Widget Types

```go
type View struct {
    Id      string    // scoped to your plugin; Tau prefixes it internally
    Title   string    // optional panel title
    Widgets []*Widget
    Style   *Style    // optional panel-level style
}

type Widget struct {
    // Exactly one of these should be set.
    Text     *TextWidget
    Stack    *StackWidget
    KeyValue *KeyValueWidget
    List     *ListWidget
    Table    *TableWidget
    Progress *ProgressWidget
    Divider  *DividerWidget
    Status   *StatusWidget
}
```

| Widget | Fields | Renders as |
|--------|--------|------------|
| `TextWidget` | `Text string`, `Style *Style` | A styled text line |
| `StackWidget` | `Direction` (`VERTICAL`/`HORIZONTAL`), `Children []*Widget`, `Gap int32` | Nested layout |
| `KeyValueWidget` | `Entries []*Entry{Key, Value, ValueStyle}` | Aligned `key: value` lines |
| `ListWidget` | `Items []string`, `Ordered bool`, `Style *Style` | Bulleted or numbered list |
| `TableWidget` | `Headers []string`, `Rows []*Row{Cells}` | A column-aligned table |
| `ProgressWidget` | `Label string`, `Fraction float64`, `Style *Style` | A progress bar (negative fraction = indeterminate) |
| `DividerWidget` | `Label string` | A horizontal rule, optionally labeled |
| `StatusWidget` | `State` (`RUNNING`/`SUCCESS`/`FAILED`/`NEUTRAL`), `Label`, `Detail` | A status line with glyph |

`Style` (used at the panel, widget, and entry level) carries a semantic
`Tone` — `TONE_INFO`, `TONE_SUCCESS`, `TONE_WARN`, `TONE_ERROR`, `TONE_MUTED`
— resolved against Tau's theme palette, so your panel's colors stay
consistent as the user's theme changes. `FgHex`/`BgHex` are an escape hatch
for when a specific color matters more than theme consistency. Prefer `Tone`.

### Example: Sync and Async Panels

```go
func (p *MyPlugin) RunCommand(ctx context.Context, name, args string) (string, *pluginapi.View, error) {
    switch name {
    case "status":
        // Sync: this view renders once, in place of a plain-text reply.
        return "", &pluginapi.View{
            Id:    "status-panel",
            Title: "MyPlugin Status",
            Widgets: []*pluginapi.Widget{
                {Kind: &pluginapi.Widget_KeyValue{KeyValue: &pluginapi.KeyValueWidget{
                    Entries: []*pluginapi.KeyValueWidget_Entry{
                        {Key: "uptime", Value: "3h12m"},
                        {Key: "requests", Value: "1,204"},
                    },
                }}},
            },
        }, nil

    case "watch":
        // Async: push a panel that stays open and can be updated later.
        if p.host == nil {
            return "", nil, fmt.Errorf("host not available")
        }
        err := p.host.RenderView(ctx, &pluginapi.View{
            Id:    "watch-panel",
            Title: "Live Status",
            Widgets: []*pluginapi.Widget{
                {Kind: &pluginapi.Widget_Status{Status: &pluginapi.StatusWidget{
                    State: pluginapi.StatusWidget_RUNNING,
                    Label: "watching",
                }}},
            },
        })
        return "watch panel opened", nil, err

    case "unwatch":
        if p.host == nil {
            return "", nil, fmt.Errorf("host not available")
        }
        return "watch panel closed", nil, p.host.CloseView(ctx, "watch-panel")
    }
    return "", nil, fmt.Errorf("unknown command: %s", name)
}
```

### Panel Limits and Cleanup

- Each plugin may have at most `MaxViewsPerPlugin` (default 5) distinct
  **open** async views at once. Updating an already-open view's content never
  counts against this limit — only opening a new `Id` does. Exceeding the
  limit returns an error from `RenderView`.
- A plugin cannot close another plugin's view, even if it guesses the exact
  `Id` — Tau namespaces every view internally by plugin name.
- On unload or `/reload`, Tau closes every view your plugin left open, so a
  killed or restarted plugin process never leaves a stale panel on screen.

---

## Plugin Configuration

Plugins receive their configuration from tau's `config.yaml` under the
`plugins:` section:

```yaml
# ~/.config/tau/config.yaml
plugins:
  my-plugin:
    api_key_env: MY_PLUGIN_API_KEY
    endpoint: https://api.example.com
    retries: 3
```

The entire `my-plugin` block is passed to the plugin as a JSON-encoded string
when it calls `host.GetConfig(ctx, "")`. Individual keys are retrieved with
`host.GetConfig(ctx, "endpoint")`.

### Persisting State

`host.SetConfig(ctx, key, value)` persists plugin state to
`~/.config/tau/plugin-state.json`. This file survives plugin reloads and tau
restarts. Use it for:

- Cached data (API responses, indexes)
- Plugin preferences set at runtime
- OAuth tokens (store the access token here after the flow completes)

State is keyed by `pluginName.key`. Empty key writes to a `""` key (entire
block replacement).

### Reading Tau Config Directly

If you need to read tau's full config (not just your plugin block), import
`github.com/samcharles93/tau/internal/config`:

```go
import tauconfig "github.com/samcharles93/tau/internal/config"

cfg, err := tauconfig.LoadConfig()
// Access cfg.Providers, cfg.DefaultModel, etc.
```

Note: this only works when the plugin is built inside the tau repo (with a
`replace` directive). For standalone plugins, use `HostService.GetConfig()`.

---

## Building, Installing, and go.mod

### Inside the Tau Repo (Development)

Each example plugin in `examples/plugins/` has its own `go.mod`:

```
module github.com/samcharles93/plugins/hello

go 1.26.4

require (
    github.com/hashicorp/go-hclog v1.6.3
    github.com/hashicorp/go-plugin v1.8.0
    github.com/samcharles93/tau v0.0.0-20260602000000-000000000000
)

replace github.com/samcharles93/tau => ../../
```

The `replace` directive points to the tau repo root so you compile against
the local checkout. Build with:

```bash
cd examples/plugins/hello
go build -o tau-plugin-hello .
```

### Standalone Plugin (External Repo)

If your plugin lives in its own repository:

```bash
go get github.com/samcharles93/tau@latest
```

Your `go.mod` depends on a released version of tau. No `replace` directive.

### Install Path

```bash
mkdir -p ~/.config/tau/plugins
cp tau-plugin-myplugin ~/.config/tau/plugins/
chmod +x ~/.config/tau/plugins/tau-plugin-myplugin
```

Tau scans this directory at startup. Use `/reload` in the TUI or Web UI to
rediscover plugins without restarting Tau.

### Required Dependencies

Every plugin needs at minimum:

```
require (
    github.com/hashicorp/go-hclog v1.6.3
    github.com/hashicorp/go-plugin v1.8.0
    github.com/samcharles93/tau v0.0.0-...
)
```

No other dependencies are required for basic plugins. Add your own as needed
for HTTP clients, database drivers, etc.

---

## Complete Working Example

Below is a complete plugin that demonstrates tools, commands, lifecycle
events, and HostService callbacks. This is the canonical reference for
plugin authors.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/hashicorp/go-hclog"
    "github.com/hashicorp/go-plugin"
    pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
)

// CounterPlugin tracks per-session turn counts and demonstrates every
// plugin API surface: tools, commands, events, HostService.
type CounterPlugin struct {
    host   pluginapi.Host
    logger *slog.Logger

    mu      sync.Mutex
    turns   int
    started time.Time
}

func main() {
    hclogger := hclog.New(&hclog.LoggerOptions{
        Level: hclog.Info, Output: os.Stderr,
        JSONFormat: false, Name: "tau-plugin-counter",
    })

    p := &CounterPlugin{
        logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
        started: time.Now(),
    }

    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: plugin.HandshakeConfig{
            ProtocolVersion:  1,
            MagicCookieKey:   "TAU_PLUGIN",
            MagicCookieValue: "tau",
        },
        Plugins: map[string]plugin.Plugin{
            "extension": &pluginapi.ExtensionPlugin{Impl: p},
        },
        GRPCServer: plugin.DefaultGRPCServer,
        Logger:     hclogger,
    })
}

// ── Capable (optional) ────────────────────────────────────────────

func (p *CounterPlugin) Capabilities() []string {
    return []string{
        pluginapi.CapabilityCommands,
        pluginapi.CapabilityTools,
        pluginapi.CapabilityEvents,
    }
}

// ── HostAware ──────────────────────────────────────────────────────

func (p *CounterPlugin) SetHost(h pluginapi.Host) {
    p.host = h
}

// ── Metadata + Commands ────────────────────────────────────────────

func (p *CounterPlugin) Metadata() (string, []*pluginapi.Command) {
    return "counter", []*pluginapi.Command{
        {
            Name:          "counter",
            Description:   "Show turn counter statistics: /counter [reset]",
            ExtensionName: "counter",
        },
    }
}

func (p *CounterPlugin) RunCommand(ctx context.Context, name, args string) (string, *pluginapi.View, error) {
    if name != "counter" {
        return "", nil, fmt.Errorf("unknown command %q", name)
    }

    p.mu.Lock()
    defer p.mu.Unlock()

    if strings.TrimSpace(args) == "reset" {
        p.turns = 0
        p.started = time.Now()
        return "✅ Counter reset.", nil, nil
    }

    elapsed := time.Since(p.started).Round(time.Second)
    return fmt.Sprintf("📊 Turns: %d | Running: %s", p.turns, elapsed), nil, nil
}

func (p *CounterPlugin) Reload(ctx context.Context) ([]*pluginapi.Diagnostic, []*pluginapi.Command, error) {
    return nil, nil, nil
}

// ── Tools ──────────────────────────────────────────────────────────

func (p *CounterPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    schema, _ := json.Marshal(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "action": map[string]any{
                "type":        "string",
                "enum":        []string{"increment", "read", "reset"},
                "description": "What to do with the counter",
            },
        },
        "required": []string{"action"},
    })

    return []*pluginapi.ToolDefinition{{
        Name:        "counter_manage",
        Description: "Manage an in-session turn counter. Returns JSON with current count, start time, and uptime.",
        InputSchema: string(schema),
    }}, nil
}

func (p *CounterPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    if toolName != "counter_manage" {
        return "", true, fmt.Errorf("unknown tool %q", toolName)
    }

    var args struct{ Action string `json:"action"` }
    if err := json.Unmarshal([]byte(arguments), &args); err != nil {
        return "", true, fmt.Errorf("parse args: %w", err)
    }

    p.mu.Lock()
    defer p.mu.Unlock()

    switch args.Action {
    case "increment":
        p.turns++
    case "reset":
        p.turns = 0
        p.started = time.Now()
    }

    result, _ := json.Marshal(map[string]any{
        "turns":   p.turns,
        "started": p.started.Format(time.RFC3339),
        "uptime":  time.Since(p.started).String(),
    })
    return string(result), false, nil
}

// ── Lifecycle Events ──────────────────────────────────────────────

func (p *CounterPlugin) DispatchEvent(ctx context.Context, event, sessionID string, payload *pluginapi.EventPayload) *pluginapi.EventResponse {
    switch event {
    case "turn_end":
        p.mu.Lock()
        p.turns++
        n := p.turns
        p.mu.Unlock()

        // Send a notification every 10 turns.
        if n%10 == 0 && p.host != nil {
            _ = p.host.Notify(ctx, "info",
                fmt.Sprintf("🔢 %d turns completed this session", n))
        }

    case "before_tool_exec":
        tool := payload.GetBeforeToolExec()
        // Block shell commands with suspicious patterns.
        if tool.ToolName == "shell" && strings.Contains(tool.Arguments, "rm -rf") {
            return &pluginapi.EventResponse{
                BlockToolExecution: true,
                BlockReason:        "Destructive command blocked by counter plugin policy",
            }
        }

    case "schedule":
        // Periodic heartbeat: log uptime to the host logger.
        if p.host != nil {
            p.mu.Lock()
            uptime := time.Since(p.started).String()
            turns := p.turns
            p.mu.Unlock()
            _ = p.host.Log(ctx, "info", "counter heartbeat",
                map[string]string{"turns": fmt.Sprint(turns), "uptime": uptime})
        }
    }
    return nil
}
```

---

## API Reference

### Go Packages

| Package | Import Path | Contents |
|---------|------------|----------|
| Plugin API | `github.com/samcharles93/tau/pkg/plugin/api` | `Extension`, `ExtensionPlugin`, `Command`, `Diagnostic`, `ToolDefinition`, `EventPayload`, `EventResponse`, `View`, `Widget`, `Style`, `Host`, `HostAware`, `Capable` |
| Chat Types | `github.com/samcharles93/tau/internal/chat` | `ChatMessage`, `ChatRole`, `ChatUsage`, `ChatParameters`, `ChatSessionState`, `ChatModelRef` |
| Config | `github.com/samcharles93/tau/internal/config` | `LoadConfig()`, `Config`, `ProviderConfig` |

### gRPC Contract

The full `.proto` definition is at
[`pkg/plugin/api/extension.proto`](https://github.com/samcharles93/tau/blob/main/pkg/plugin/api/extension.proto). The
generated Go code is in
[`pkg/plugin/api/extension.pb.go`](https://github.com/samcharles93/tau/blob/main/pkg/plugin/api/extension.pb.go) and
[`pkg/plugin/api/extension_grpc.pb.go`](https://github.com/samcharles93/tau/blob/main/pkg/plugin/api/extension_grpc.pb.go).

### Core Types

| Type | Package | Description |
|------|---------|-------------|
| `Command` | `api` | Slash command definition (`Name`, `Description`, `ExtensionName`) |
| `Diagnostic` | `api` | Warning/error reported to the host (`Severity`, `Message`, `Path`) |
| `ToolDefinition` | `api` | Tool declaration (`Name`, `Description`, `InputSchema`) |
| `EventPayload` | `api` | Oneof carrying typed event data |
| `EventResponse` | `api` | Plugin's modifications to runtime behaviour |
| `View` | `api` | A structured panel (`Id`, `Title`, `Widgets`, `Style`) |
| `Widget` | `api` | One renderable element of a `View` (oneof of 8 kinds) |
| `ExtensionPlugin` | `api` | go-plugin shim wrapping an `Extension` |

---

## Troubleshooting

### Plugin does not appear in Tau

- Check the file is executable: `chmod +x ~/.config/tau/plugins/tau-plugin-*`.
- Check Tau's logs (`~/.config/tau/tau.log`) for handshake or launch errors.
- Verify the handshake constants match exactly (`TAU_PLUGIN` / `tau`).
- Verify the `go.mod` module path and `replace` directive are correct.
- Check that the plugin binary is for the correct OS/architecture.

### Tool not available to the agent

- Ensure `Tools()` returns the tool and `InputSchema` is valid JSON.
- Check that `InputSchema` is a proper JSON Schema object (starts with `{`).
- The tool is registered as `plugin:<name>.<tool>`. Use `/debug` in the TUI to
  see registered tools.
- If using `Capable`, verify `CapabilityTools` is in the list.

### Plugin process stays around after Tau exits

- go-plugin normally kills child processes on client disconnect. If your
  plugin spawns its own children, handle `SIGTERM` in the plugin and clean
  them up.
- Check for orphaned processes with `ps aux | grep tau-plugin`.

### Plugin panics are swallowed

- go-plugin isolates plugin crashes. Panics in `ExecuteTool` or
  `DispatchEvent` are caught by the gRPC server and returned as errors.
- Check `stderr` of the plugin or Tau's log for stack traces.

### DispatchEvent is never called

- Verify the plugin advertises `CapabilityEvents` (or doesn't implement
  `Capable` at all, which defaults to full capability).
- Check that the event name string matches exactly (case-sensitive).

### HostService calls return errors

- `SetHost()` must have been called before you can use `p.host`.
- If `SetHost()` is never called, verify your plugin implements `HostAware`.
- `GetConfig()` returns `found: false` if no config block exists for your
  plugin in `config.yaml`. Add a `plugins.<name>:` block.

### gRPC connection errors

- Ensure `go.sum` includes the correct versions of `go-plugin` and `grpc`.
- Run `go mod tidy` in your plugin directory to resolve dependencies.
- If building inside the tau repo, verify the `replace` directive points to
  the correct relative path.
