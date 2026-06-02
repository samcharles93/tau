# 60. Bidirectional Plugin Communication — Host Service API

## Status: Design constraint

### Motivation

The current interface is host→plugin only: host dispatches events, host runs commands, host pushes audit events. But real plugins need to initiate communication back to the host. Two critical patterns require this:

1. **Agent-callable plugins**: The agent needs to discover a plugin's tools and call them. The plugin registers tools with the host, then the host calls the plugin to execute them. But between calls, the plugin server might push notifications to the client — that's bidirectional streaming initiated by the plugin.

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

```shell
Host → Plugin.DispatchEvent(session_start, ctx)
Host → Plugin.RunCommand("/export", "session-id")
Host → Plugin.PipelineService.ProcessRequest(stream)
```

**Pattern B: Query (Plugin → Host pull)** — New. Plugin calls host. Plugin-initiated, asynchronous. Good for: context lookups, state queries, configuration reads.

```shell
Plugin → Host.GetSessionState("session-123")           // What's the current session state?
Plugin → Host.GetAvailableModels()                      // What models are available?
Plugin → Host.GetConfig("plugin.my-plugin.api-key")     // Read my API key
```

**Pattern C: Subscribe (Plugin pulls event stream)** — Plugin subscribes to host event bus. Instead of host pushing every event, plugin pulls only what it needs.

```shell
Plugin → Host.SubscribeEvents(["tool_call_started", "session_shutdown"])
Host → stream(ChatEvent, ChatEvent, ...)  // Plugin receives filtered events
```

This is more efficient than Pattern A for high-frequency events (like token streaming) because the plugin only receives events it subscribed to, not every event the host fires.

**Pattern D: Register + Callback (Plugin registers, Host calls back)** — Plugin registers resources (tools, commands, panels), then host calls back when needed.

```shell
Plugin → Host.RegisterTools(stream ToolDef, ToolDef, ...)
// Later, when agent wants to call a tool:
Agent → Host (via coordinator) → Host.ExecutePluginTool(plugin, tool, args)
// Host routes to the correct plugin automatically.
```

### Mapping to Plugin Capabilities

| Capability | Push (Host→Plugin) | Pull (Plugin→Host) | Register+Callback |
| ---------- | ------------------ | ------------------ | ----------------- |
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

**Bidirectional streaming is needed when**: Both sides produce data over time (token streaming, pipeline processing, plugin notifications). Neither side is purely a client or server.

### The Full Communication Surface

```shell
┌─ Tau Host ──────────────────────────────────────┐
│                                                 │
│  Exposes: HostService                           │
│    GetSessionState ←─── Plugin calls this       │
│    GetAvailableModels ←─── Plugin calls this    │
│    SubscribeEvents ←─── Plugin subscribes       │
│    RegisterTools ←─── Plugin registers          │
│    ExecutePluginTool ←─── Host routes to plugin │
│    Notify ←─── Plugin pushes to TUI             │
│    GetConfig/SetConfig ←─── Plugin storage      │
│                                                 │
│  Consumes: ExtensionService (per-plugin)        │
│    DispatchEvent ───→ Plugin receives           │
│    RunCommand ───→ Plugin executes              │
│    Reload ───→ Plugin reloads                   │
│                                                 │
│  Consumes: ProviderService (if capability)      │
│  Consumes: StreamService (bidirectional)        │
│  Consumes: PipelineService (bidirectional)      │
│  Consumes: UIService (if capability)            │
│  Consumes: AuditService (if capability)         │
│                                                 │
└─────────────────────────────────────────────────┘
```

### Security: Plugin Scoping

Not every plugin gets full HostService access. Capabilities gate the available host methods:

| Host method | Required capability |
| ----------- | ------------------- |
| GetSessionState | core (always available) |
| GetAvailableModels | provider |
| RegisterTools | tools |
| SubscribeEvents | audit |
| GetConfig/SetConfig | core |
| Notify | core |

The host checks the plugin's declared capabilities before allowing each call. A core-only plugin can't call RegisterTools. A provider plugin can't call SubscribeEvents.

### What This Enables

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
