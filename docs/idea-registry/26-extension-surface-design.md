# 58. Extension Surface Design — Capability-Based Plugin System

## Status: Design phase

### Motivation

The initial Extension interface (Metadata, RunCommand, Reload, DispatchEvent) is deliberately narrow — just commands and lifecycle events. To support the wilder ideas (Custom TUI Panels, Multi-Model Router, Compliance/Audit, Live Token Stream Processor, External Tool Registry), the interface must grow. But we can't just keep adding methods — that breaks every existing plugin on each release.

**The solution**: capability-based extension surface. Plugins declare which capabilities they support. The host discovers capabilities during handshake. Unknown capabilities are gracefully ignored. This is how gRPC service discovery already works — we formalize it.

### Design: Capability-Based Plugin Interface

Instead of one monolithic `Extension` interface, plugins implement **capability interfaces**. Each capability is a separate gRPC service. Plugins opt in to the capabilities they need.

```shell
┌─ Plugin binary ─────────────────────────────────────┐
│                                                     │
│  Capabilities declared in plugin manifest:          │
│                                                     │
│  ☑ core         — metadata, commands, lifecycle     │
│  ☐ provider     — custom auth, model routing        │
│  ☐ stream       — token interception/transformation │
│  ☐ tools        — dynamic tool registration         │
│  ☐ ui           — custom panels, keybindings        │
│  ☐ audit        — observation sink for all events   │
│  ☐ pipeline     — request/response middleware       │
│                                                     │
│  Only implements the gRPC services it needs.        │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### Proto: Capability Discovery

```protobuf
// Capabilities are discovered during plugin handshake.
service Discovery {
  rpc GetCapabilities(CapabilitiesRequest) returns (CapabilitiesResponse);
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
  // Passive observer — receives all ChatEvents, cannot modify them.
  rpc Observe(stream ChatEvent) returns (AuditAck);
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

```shell
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
| ---- | ------------ | ------------ |
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

```plaintext
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
| ---- | ----- |
| `internal/plugin/proto/extension.proto` | 2 — add `GetCapabilities` RPC, `StreamService` |
| `internal/plugin/proto/provider.proto` | 3 — `ProviderService` |
| `internal/plugin/proto/stream.proto` | 2 — `StreamService` |
| `internal/plugin/proto/tools.proto` | 3 — `ToolService` |
| `internal/plugin/proto/ui.proto` | 3 — `UIService` |
| `internal/plugin/proto/audit.proto` | 3 — `AuditService` |
| `internal/plugin/proto/pipeline.proto` | 3 — `PipelineService` |
| `internal/plugin/capabilities.go` | 2 — Go capability interfaces |
| `internal/plugin/manager.go` | 2 — capability discovery, routing |
