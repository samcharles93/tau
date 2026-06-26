---

# Tau Plugin SDK

Tau’s plugin system lets you extend the chat runtime with custom tools, slash
commands, and lifecycle event hooks. Plugins are self-contained Go binaries
that communicate with Tau over gRPC using [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin).

Plugins live in `~/.config/tau/plugins/` (or the directory configured via the
`PluginsDir` option). Tau discovers and launches them automatically at startup.

## Table of contents

1. [Quick start](#quick-start)
2. [Plugin lifecycle](#plugin-lifecycle)
3. [The Extension interface](#the-extension-interface)
4. [Tools](#tools)
5. [Slash commands](#slash-commands)
6. [Lifecycle events](#lifecycle-events)
7. [Configuration](#configuration)
8. [Building and installing](#building-and-installing)
9. [API reference](#api-reference)
10. [Troubleshooting](#troubleshooting)

## Quick start

The fastest way to understand the API is to read and run the
[hello plugin](../examples/plugins/hello/main.go).

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

Inside Tau:

```
/hello world
```

Or ask the agent to call the plugin tool:

```
Greet me using the hello plugin
```

## Plugin lifecycle

1. **Discovery** — Tau scans `~/.config/tau/plugins/` for executable files.
2. **Launch** — Each plugin binary is started as a subprocess and connected via
gRPC using the handshake below.
3. **Metadata** — Tau calls `Metadata()` to learn the plugin name and slash
commands.
4. **Tool discovery** — Tau calls `Tools()` and registers returned tools in the
agent tool registry as `plugin:<plugin-name>:<tool-name>`.
5. **Runtime** — Tau forwards slash commands and tool calls to the plugin, and
sends lifecycle events via `DispatchEvent()`.
6. **Unload** — On shutdown or `/reload`, Tau kills the subprocess and
unregisters its tools.

### Handshake

Every plugin must use this exact handshake configuration so Tau recognizes it:

```go
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
})
```

## The Extension interface

A plugin implements `github.com/samcharles93/tau/pkg/plugin/api.Extension`:

```go
type Extension interface {
    Metadata() (name string, commands []*Command)
    RunCommand(ctx context.Context, name, args string) (string, error)
    Reload(ctx context.Context) (diagnostics []*Diagnostic, commands []*Command, err error)
    Tools(ctx context.Context) ([]*ToolDefinition, error)
    ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error)
    DispatchEvent(ctx context.Context, event string, sessionID string, payload *EventPayload) *EventResponse
}
```

All methods are required. If a plugin does not support a capability, return an
empty result or `nil`.

## Tools

Tools are functions the agent can call during a turn. They are advertised by
`Tools()` and executed by `ExecuteTool()`.

### Declaring a tool

```go
func (p *MyPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    schema, _ := json.Marshal(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "path": map[string]any{
                "type":        "string",
                "description": "File path to read",
            },
        },
        "required": []string{"path"},
    })

    return []*pluginapi.ToolDefinition{{
        Name:        "read_file",
        Description: "Read a file from disk",
        InputSchema: string(schema),
    }}, nil
}
```

`InputSchema` must be a valid JSON Schema object describing the parameters the
LLM should provide.

### Executing a tool

```go
func (p *MyPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    var args struct{ Path string `json:"path"` }
    if err := json.Unmarshal([]byte(arguments), &args); err != nil {
        return "", true, err
    }

    data, err := os.ReadFile(args.Path)
    if err != nil {
        return "", true, err
    }
    return string(data), false, nil
}
```

Return values:

- `content` — string result shown to the agent (usually JSON for complex results).
- `isError` — `true` if the tool failed; Tau renders this as a failed tool call.
- `err` — non-nil only for protocol/unrecoverable errors.

## Slash commands

Slash commands appear in the TUI/Web UI command palette and are invoked by name.

### Declaring commands

```go
func (p *MyPlugin) Metadata() (string, []*pluginapi.Command) {
    return "my-plugin", []*pluginapi.Command{{
        Name:        "/status",
        Description: "Show plugin status",
        ExtensionName: "my-plugin",
    }}
}
```

### Executing commands

```go
func (p *MyPlugin) RunCommand(ctx context.Context, name, args string) (string, error) {
    if name != "/status" {
        return "", fmt.Errorf("unknown command %q", name)
    }
    return "Plugin is healthy", nil
}
```

## Lifecycle events

Tau sends events to plugins via `DispatchEvent()`. Plugins can inspect the
payload and optionally return an `EventResponse` to modify behavior.

### Supported events

| Event | When | Payload kind |
|---|---|---|
| `session_start` | A chat session starts | `SessionEventPayload` |
| `context` | Before every LLM turn | `ContextPayload` |
| `before_llm_call` | Right before the LLM request is sent | `BeforeLLMCallPayload` |
| `after_llm_call` | After the LLM response finishes | `AfterLLMCallPayload` |
| `before_tool_exec` | Before a tool is executed | `ToolCallPayload` |
| `after_tool_exec` | After a tool finishes | `ToolResultPayload` |
| `message_delta` | On each streamed token | `MessageDeltaPayload` |
| `turn_start` / `turn_end` | At turn boundaries | `TurnPayload` |
| `compaction_before` / `compaction_after` | Around context compaction | `CompactionPayload` |
| `schedule` | Periodically, if `TAU_SCHEDULE_INTERVAL` is set | none |

### Event responses

Plugins can return `*EventResponse` to influence the runtime:

- `InjectMessages` — add JSON-encoded `chat.ChatMessage` values to context.
- `RemoveMessageIndices` — remove messages by index.
- `InjectSystemPrompt` — append text to the system prompt.
- `BlockToolExecution` / `BlockReason` — prevent a tool call.
- `ModifiedToolArguments` / `ModifiedToolResult` — rewrite tool input/output.
- `AddHeaders` / `ModifiedModelId` — mutate the LLM request.
- `Diagnostics` — emit diagnostics shown in the TUI.
- `SuppressDefault` — skip Tau’s default handling.

Most events can simply return `nil` if the plugin only needs to observe.

## Configuration

Plugins can read Tau’s global config from `~/.config/tau/config.yaml`. The
`plugins:` section is free-form YAML passed to each plugin by name. Plugins parse
it themselves; Tau does not enforce a schema.

Example:

```yaml
plugins:
  my-plugin:
    api_key_env: MY_PLUGIN_API_KEY
    endpoint: https://api.example.com
```

Use `github.com/samcharles93/tau/internal/config` if you want to load the same
config files Tau uses, or read the YAML directly for plugin-specific sections.

## Building and installing

Plugins are normal Go binaries. They must import
`github.com/samcharles93/tau/pkg/plugin/api` and use a `replace` directive when
working inside the Tau repo or a local checkout.

### Inside the Tau repo

Each plugin should have its own `go.mod`:

```go
module github.com/samcharles93/plugins/hello

go 1.26.4

require (
    github.com/hashicorp/go-hclog v1.6.3
    github.com/hashicorp/go-plugin v1.8.0
    github.com/samcharles93/tau v0.0.0-20260602000000-000000000000
)

replace github.com/samcharles93/tau => ../../
```

### Standalone plugin

If your plugin lives in its own repository, depend on a released version of
`github.com/samcharles93/tau`:

```bash
go get github.com/samcharles93/tau@latest
```

### Install path

```bash
mkdir -p ~/.config/tau/plugins
cp tau-plugin-myplugin ~/.config/tau/plugins/
```

Tau scans this directory at startup. Use `/reload` in the TUI or Web UI to
rediscover plugins without restarting Tau.

## API reference

Key packages and types:

- `github.com/samcharles93/tau/pkg/plugin/api`
  - `Extension` — interface to implement.
  - `ExtensionPlugin` — go-plugin shim.
  - `Command`, `Diagnostic`, `ToolDefinition`, `EventPayload`, `EventResponse`.
- `github.com/samcharles93/tau/internal/chat`
  - `ChatMessage`, `ChatRole`, `ChatUsage`, `ChatParameters` — used when
    decoding event payload strings.
- `github.com/samcharles93/tau/internal/config`
  - `LoadConfig()`, `ProviderConfig`, `Config` — optional config helpers.

See the [protocol buffer definition](../pkg/plugin/api/extension.proto) for
the full gRPC contract.

## Troubleshooting

**Plugin does not appear in Tau**

- Check the file is executable: `chmod +x ~/.config/tau/plugins/tau-plugin-*`.
- Check Tau’s logs (`~/.config/tau/tau.log`) for handshake or launch errors.
- Verify the handshake constants match exactly (`TAU_PLUGIN` / `tau`).

**Tool not available to the agent**

- Ensure `Tools()` returns the tool and `InputSchema` is valid JSON.
- The tool is registered as `plugin:<plugin-name>:<tool-name>`; the agent sees
the original `tool-name`.

**Plugin process stays around after Tau exits**

- go-plugin normally kills child processes on client disconnect. If your plugin
spawns its own children, handle `SIGTERM` in the plugin and clean them up.

**Plugin panics are swallowed**

- go-plugin isolates plugin crashes. Check `stderr` of the plugin or Tau’s log
for stack traces.
