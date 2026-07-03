# Example: Creating an MCP Plugin

This walks through [`plugins/mcp`](https://github.com/samcharles93/tau/blob/main/plugins/mcp/main.go),
a real, working plugin shipped in the tau repo. It bridges [Model Context
Protocol](https://modelcontextprotocol.io) (MCP) servers into tau: every tool
an MCP server exposes becomes a normal agent tool, with no changes to tau
itself. If you haven't read the [Plugin SDK](/plugins) guide yet, start
there — this page assumes you know the `Extension` interface, `Tools()` /
`ExecuteTool()`, and `DispatchEvent()`.

## Why a plugin, not native support

MCP servers speak JSON-RPC over one of the spec's transports — stdio (a
subprocess) or Streamable HTTP (a running server reached over HTTP). Tau's
tool-calling model is Go-native — tools are Go functions returning a result
string. Rather than teach the coordinator a second tool protocol, the MCP
plugin does the translation once: it connects to each configured MCP server,
lists its tools, and re-advertises each one as a `pluginapi.ToolDefinition`.
From the agent's point of view, an MCP tool looks identical to a built-in tool
or any other plugin tool.

## Configuration

Servers are declared under `plugins.mcp-plugin.servers` in `config.yaml`:

```yaml
plugins:
  mcp-plugin:
    servers:
      # stdio transport: a subprocess speaking MCP over stdin/stdout.
      - name: filesystem
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      - name: postgres
        command: npx
        args: ["-y", "@modelcontextprotocol/server-postgres", "$DATABASE_URL"]
      # Streamable HTTP transport: a running server reached over HTTP.
      - name: spawn
        url: http://localhost:9343/mcp
```

Each server uses one of the two MCP-spec transports, selected by config:

- `command`/`args` — **stdio**: exactly what you'd run by hand to start the
  server; the plugin execs it and speaks MCP over its stdin/stdout.
- `url` — **Streamable HTTP**: the plugin connects to the running server over
  HTTP.

Both transports are the official implementations from
[`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk),
so the plugin conforms to the MCP specification rather than defining its own
wire protocol. Set exactly one of `command` or `url` per server.

## Struct and capabilities

```go
type MCPPlugin struct {
    mu       sync.RWMutex
    sessions map[string]*mcpSession // server name → MCP session
    logger   *slog.Logger
    host     pluginapi.Host
}

func (p *MCPPlugin) SetHost(h pluginapi.Host) { p.host = h }

func (p *MCPPlugin) Capabilities() []string {
    return []string{pluginapi.CapabilityTools, pluginapi.CapabilityCommands, pluginapi.CapabilityInteractive}
}
```

`CapabilityInteractive` matters here specifically because `/mcp-reconnect`
calls `host.Confirm()` before tearing down a live connection (see below) — it
tells tau this plugin will make `HostService.Confirm`/`Input` calls, so tau
doesn't skip wiring them up.

## Bridging tools

Servers connect lazily, on the first `Tools()` call rather than at plugin
startup — this keeps tau's boot fast even if an MCP server is slow to spawn:

```go
func (p *MCPPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    p.mu.RLock()
    needsLoad := len(p.sessions) == 0
    p.mu.RUnlock()

    if needsLoad {
        _ = p.loadServers(ctx) // release the lock first to avoid deadlocking loadServers
    }

    p.mu.RLock()
    defer p.mu.RUnlock()

    var tools []*pluginapi.ToolDefinition
    for _, s := range p.sessions {
        serverTools, err := s.listTools(ctx)
        if err != nil {
            p.logger.Warn("failed to list tools", "server", s.name, "err", err)
            continue
        }
        tools = append(tools, serverTools...)
    }
    return tools, nil
}
```

Each MCP tool is renamed `<server>.<tool>` so tools from different servers
never collide — `filesystem.read_file` and `postgres.read_file` can coexist:

```go
func (s *mcpSession) listTools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    resp, err := s.session.ListTools(ctx, &mcp.ListToolsParams{})
    // ...
    tools[i] = &pluginapi.ToolDefinition{
        Name:        s.name + "." + t.Name,
        Description: t.Description,
        InputSchema: schema, // MCP's InputSchema, re-marshaled as-is
    }
}
```

`ExecuteTool` reverses the naming to route the call back to the right server:

```go
func (p *MCPPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error) {
    serverName, shortName, ok := strings.Cut(toolName, ".")
    if !ok {
        return "", true, fmt.Errorf("invalid tool name %q (expected server.tool)", toolName)
    }
    // look up (or lazily connect) the session for serverName, then:
    return session.callTool(ctx, shortName, arguments)
}
```

MCP tool results can contain text or image content blocks; `callTool`
serializes them to a JSON array of `{"type": "text", "text": ...}` /
`{"type": "image", "data": ..., "mimeType": ...}` objects, which becomes the
tool's return string.

## Slash commands for operability

An always-connected background bridge needs a way to inspect and recover it
without restarting tau — that's what the plugin's three commands are for:

| Command | Purpose |
|---------|---------|
| `/mcp-list` | Show connected servers and their tool counts |
| `/mcp-reconnect <server>` | Tear down and reconnect one server |
| `/mcp-reload` | Reconnect everything from the current config |

`/mcp-reconnect` is the interesting one — it uses `host.Confirm()` to avoid
silently killing a connection something else might be mid-call against:

```go
func (p *MCPPlugin) cmdReconnect(ctx context.Context, serverName string) (string, error) {
    if p.host != nil {
        confirmed, err := p.host.Confirm(ctx,
            "Reconnect MCP server",
            "Disconnect and reconnect to "+serverName+"? This will interrupt any active connections.")
        if err != nil || !confirmed {
            return "Cancelled.", nil
        }
    }
    // ... close the old session, reconnect from config
}
```

`Confirm()` suspends the command until the user answers a yes/no prompt
rendered by the TUI (or Web UI) — this is the same interactive surface
described in [Advanced Plugins](/examples/advanced-plugin).

## Build and install

```bash
cd plugins/mcp
go build -o tau-plugin-mcp .

mkdir -p ~/.config/tau/plugins
cp tau-plugin-mcp ~/.config/tau/plugins/

tau
```

`plugins/mcp` is a standalone Go module (its own `go.mod`, with a `replace`
directive pointing back at the tau repo root) — see
[Building, Installing, and go.mod](/plugins#building-installing-and-go-mod)
for the difference between in-repo and standalone plugin builds.

## Try it

```yaml
plugins:
  mcp-plugin:
    servers:
      - name: filesystem
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
```

```
/mcp-list
```

Then ask the agent something that needs the bridged tool — e.g. "list the
files in /tmp" — and it calls `filesystem.list_directory` exactly like any
other tool.
