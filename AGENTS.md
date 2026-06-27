# TAU Project Guidelines

## Git Commits IMPORTANT

Use `git commit -m ""` for all commits to ensure proper signing.

ONLY EVER COMMIT USING THIS APPROACH

All commit messages MUST use conventional commit format. This is required for automated releases via release-please.

Format: `<type>: <description>` or `<type>(<scope>): <description>`

| Prefix | Version Bump | Use When |
| ------ | ------------ | -------- |
| `feat:` | minor (0.1.0 → 0.2.0) | Adding new functionality |
| `fix:` | patch (0.1.0 → 0.1.1) | Fixing a bug |
| `perf:` | patch | Performance improvements |
| `refactor:` | patch | Code changes that don't add features or fix bugs |
| `docs:` | patch | Documentation only |
| `test:` | patch | Adding or updating tests |
| `chore:` | patch | Maintenance, dependencies, tooling |
| `ci:` | patch | CI/CD changes |
| `build:` | patch | Build system changes |
| `revert:` | patch | Reverting a previous commit |

For BREAKING CHANGES (major bump, e.g. 0.1.0 → 1.0.0), add `!` after the type: `feat!: remove deprecated API` or include `BREAKING CHANGE:` in the commit body.

Examples:

```shell
git commit -m "feat: add custom slash command support"
git commit -m "fix(agent): correct tool loop iteration count"
git commit -m "refactor(tui): extract completion logic into dedicated component"
git commit -m "chore: update go dependencies"
```

## Code Style & Linting

We enforce strict formatting, linting, and language modernization standards for all Go files:

- **Formatting**: Format all Go files using `gofumpt`:

  ```bash
  golangci-lint fmt ./...
  ```

- **Modernization**: Use `go fix` to apply and keep modern Go library usages and constructs updated:

  ```bash
  go fix ./...
  ```

- **Linting**: Verify code quality and static analysis warnings using `golangci-lint`:

  ```bash
  golangci-lint run
  ```

- All code must pass `golangci-lint` and `go fix ./...` checks before being committed.

## Strict Requirements

- Never hard code any values, paths, or configurations in the code. Always use environment variables or configuration files to manage such information.
- Ensure that all sensitive information like API keys, tokens or passwords are stored securely and not exposed in the codebase or logs and anything in the project repository must be ignored via .gitignore.
- Never hard code colour values (hex literals like `"#DA1710"`) or define local colour variables. If a consistent colour/styling system is needed, create a shared package (e.g. `internal/theme`) first and import it from consuming code.

## Project Overview

Tau is a provider-agnostic, coding agent with an interactive terminal UI. It features an agentic tool-calling loop, plugin/extension system, skill discovery, session persistence, and a taui-based reactive UI.

## Architecture

```flow
cmd/tau/main.go (binary entry point)
        │ delegates to cli
        ▼
internal/cli/ (thin command definitions, flag parsing)
        │ delegates to app
        ▼
internal/app/ (orchestration: creates eventbus.Bus, wires subsystems as Clients)
        │
        ├─► Creates eventbus.Bus (central event router — type-based, no string topics)
        │       │
        │       ├─► Client("coordinator") — agent.Coordinator
        │       ├─► Client("tui")         — tui client (event subscription)
        │       ├─► Client("skills")      — skills.Manager
        │       ├─► Client("registry")    — command registry
        │       └─► (extensible: any subsystem can become a Client)
        │
        ├─► internal/agent/coordinator.go (agentic turn loop)
        │       │
        │       ├─► internal/agent/tools/ (built-in tools + registry)
        │       ├─► internal/plugin/ (extension loading + execution)
        │       ├─► internal/skills/ (SKILL.md discovery)
        │       └─► internal/chat/ (types, commands, events)
        │
        ├─► internal/app/streamer.go (ai-sdk adapter)
        ├─► internal/app/platform.go (token resolution via ai-sdk)
        │
        └─► internal/tui/ (taui-based inline terminal UI)
                │
                └─► internal/tui/notify/ (queue-based notification system)
```

### Communication Flow

```flow
┌──────────────────────────────────────────────────────────────────┐
│                        eventbus.Bus                               │
│                                                                   │
│  Publisher[ChatEvent] ──────► Bus ──────► Subscriber[ChatEvent]   │
│  Publisher[SkillEvent] ─────►     ─────► Subscriber[SkillEvent]   │
│                                                                   │
│  Routing: reflect.Type, not string topics. Compile-time safety.   │
│  Multicast: all subscribers of type T receive every event.        │
│  Total order: single pump goroutine serializes all publications.  │
└──────────────────────────────────────────────────────────────────┘

TUI ──Send(ChatCommand)──► Coordinator ──Publish(ChatEvent)──► Bus ──► TUI
  ▲                                                              │
  └──────────────── Subscribe(ChatEvent) ────────────────────────┘
```

## Project Organisation

The project follows a **layered architecture** with a command/event boundary between the TUI and chat runtime. All internal packages live under `internal/`.

### Layers (top → bottom)

| Layer | Packages | Role |
| ----- | -------- | ---- |
| Entry point | `cmd/tau/` | Binary bootstrap; delegates to `cli` |
| CLI | `internal/cli/` | Command definitions & flag parsing (thin handlers) |
| Orchestration | `internal/app/` | Wires subsystems together for each use case (chat, token, models) |
| Domain | `internal/chat/`, `internal/skills/`, `internal/agent/` | Core business logic, commands, events |
| Presentation | `internal/tui/` | taui-based interactive terminal UI |
| Infrastructure | `internal/config/`, `internal/eventbus/`, `internal/store/`, `internal/sessions/`, `internal/indexing/` | Config, event bus, persistence, search |

### Package Responsibilities

- **`app`** — Service/orchestration layer. Resolves tokens (via ai-sdk runtime), discovers models, creates the coordinator, command registry, skill manager, and launches the TUI or one-shot stream. CLI commands call `app.*` functions.
- **`cli`** — Thin command definitions using urfave/cli. Parses flags and delegates to `app`.
- **`chat`** — Chat types and contracts. Defines `ChatCommand`, `ChatEvent`, `ChatRuntime`, `ChatSessionState`, `ChatSessionConfig`, `ChatSessionPatch`, `ChatModelRef`, `ChatParameters`, `ChatUsage`, `ChatToolCall`, `ChatToolDef`, `CommandRef`, `ExtensionCommand`, `ExtensionReloader`, `SessionSummary`, and all concrete event/command types. Consumed by every other package; exports only types (no behaviour).
- **`eventbus`** — Central in-process event bus. Routes events by Go type (`Publisher[ChatEvent]` → `Subscriber[ChatEvent]`) rather than string topics. Adapted from Tailscale's `util/eventbus` (BSD-3-Clause). The bus enforces total ordering of published events via a single pump goroutine. Clients are named handles that own their publishers and subscribers; `Client.Close()` cascades cleanup. Designed so that subsystems communicate through the bus without importing each other.
- **`agent`** — Coordinator runtime: agentic turn loop, tool execution, plugin lifecycle dispatch, session persistence. Receives `ChatCommand` via its command channel and publishes `ChatEvent` on the event bus. Defines `Streamer` and `TokenSource` interfaces.
- **`tui`** — taui-based interactive terminal UI. Subscribes to `ChatEvent` on the event bus and sends `ChatCommand` to the coordinator. Files are prefixed `inline_*` reflecting the inline rendering approach. Uses `pkg/taui` for all widget rendering (no direct go-tui dependency).
  - **Core (`internal/tui/`)**: Handles app bootstrapping, event watchers, slash command dispatch, completions, and inline rendering.
  - **Notify (`internal/tui/notify/`)**: Queue-based notification system.
- **`config`** — Loads `~/.config/tau/config.yaml` (global) and `.tau.yaml` (project-local); foundation package with no internal imports.
- **`skills`** — Skill discovery from markdown/YAML files, lifecycle management, activation tracking. Publishes `skills.Event` on the event bus when the catalog is refreshed.
- **`store`** — Session persistence layer (SQLite + raw SQL). Defines `SessionStore` interface and `SessionSummary` struct. Implements JSONL export.
- **`sessions`** — Session lifecycle management (create, update, close, branch). Wraps the coordinator and store for session orchestration.
- **`plugin`** — gRPC-based plugin/extension system using HashiCorp go-plugin.
- **`registry`** — Command registry: discovers built-in, custom (markdown-based), skill-based, and extension commands. Publishes `CommandsChangedEvent` on the event bus so the TUI can update completions.
- **`indexing`** — Full-text search over sessions using Bleve.
- **`chat/commands`** — User-defined custom command loading from markdown files (user-level `~/.config/tau/commands/` and project-level `.tau/commands/`).

### Dependency Rules

1. **CLI → App → Domain/Infra** — never the reverse.
2. **Domain packages** (`chat`, `skills`, `agent`) may import infrastructure (`config`, `eventbus`) but never `cli`, `app`, or `tui`.
3. **TUI** imports `chat`, `eventbus`, and TUI-local packages only — never `app`, `cli`, or `config` directly.
4. **Infrastructure** packages (`config`, `eventbus`) have zero internal imports — they are leaf dependencies.
5. **`app`** is the only package that may import both domain and infrastructure to wire them together.
6. **`pkg/taui`** has zero tau-internal imports — it is a standalone TUI framework dependency.

### Communication Pattern

- TUI sends `ChatCommand` through the runtime's command channel (point-to-point).
- Coordinator publishes `ChatEvent` via `eventbus.Publisher[ChatEvent]` on the bus.
- TUI subscribes to events via `eventbus.Subscriber[ChatEvent]` and renders updates.
- Skills manager publishes `skills.Event` via `eventbus.Publisher[skills.Event]`.
- Command registry publishes `chat.CommandsChangedEvent` via `eventbus.Publisher[chat.CommandsChangedEvent]`.
- No external message broker; the in-process event bus routes by Go type, not string topics.
- Subsystems communicate through the bus without importing each other — they import `eventbus` and the shared type definitions in `chat` or `skills`.

## Core Principles

Tau is built on three pillars: **extensible**, **playful**, and **constant**.

- **Extensible** — Tau provides primitives, not products. The session tree, event dispatch, schedule hook, tool registry, and plugin API are the platform. Everything else (GitHub polling, code review, deployment workflows) belongs in plugins or extensions. Before adding a feature, ask: *could this be a plugin?* If yes, don't hard-code it — expose the API surface and let plugins build on it. Tau is never done; it grows through its plugin ecosystem.
- **Playful** — The tool should be a joy to use. Design interactions that delight. Error messages should be helpful, not cryptic. The TUI should feel responsive and alive. Status icons, colour, and layout should reward attention. If it feels like work, it's not done.
- **Constant** — Using tau should produce strong, reliable outputs every time. Sessions are deterministic within their context. The agent loop is predictable and debuggable. Prompts are versioned templates, not ad-hoc strings. The system prompt and tool set produce consistent quality regardless of provider or model.

## Anti-Duplication & Architectural Integrity

To prevent orphaned logic, duplicate systems, and disconnected code, **all agents must adhere to the following rules**:

1. **Investigate Before Implementation**: Never scaffold a new package, type, or event without first performing a codebase search (`grep`, `glob`, or exploration skills) for related terminology. If a queue, parser, or command registry already exists, **reuse and adapt it** rather than creating a new one.
2. **Immediate Wiring (No Dead Code)**: If you implement a new package or manager (e.g., a command parser), you **must** immediately wire it into the `app` orchestration layer and the `coordinator` lifecycle. Code that exists but is never invoked is technical debt.
3. **Single Source of Truth**: Identify overlapping architectures and merge them. (e.g., If the TUI receives alerts via both a direct event stream and an external pubsub bus, consolidate them into a single stream).
4. **Follow the Command/Event Boundary**:
   - All input from the user/TUI goes through `tauchat.ChatCommand` interfaces.
   - All output to the TUI comes back through `tauchat.ChatEvent` interfaces on the event bus. Do not circumvent this architecture.

---

## Where to Look (By Task)

Use this section to quickly find the right files for a given change.

### Changing the ChatRuntime / Command & Event Contract

- `internal/chat/types.go` — All public types:
  - **Commands** (TUI → Runtime): `ChatCommand` interface, `StartChatSessionCommand`, `SubmitChatPromptCommand`, `SteerChatPromptCommand`, `UpdateChatSessionCommand`, `CancelChatRequestCommand`, `ResetChatSessionCommand`, `CloseChatSessionCommand`, `ReloadExtensionsCommand`, `RunExtensionCommandCommand`, `RespondInteractivePromptCommand`, `ListSessionsCommand`, `LoadSessionCommand`, `DeleteSessionCommand`, `ExportSessionCommand`
  - **Events** (Runtime → TUI): `ChatEvent` interface, `ChatSessionSnapshotEvent`, `ChatResponseStartedEvent`, `ChatResponseDeltaEvent`, `ChatReasoningDeltaEvent`, `ChatToolCallDeltaEvent`, `ChatToolOutputEvent`, `ChatToolExecutionStartedEvent`, `ChatToolExecutionCompletedEvent`, `ChatResponseCompletedEvent`, `ChatResponseCancelledEvent`, `ChatRuntimeErrorEvent`, `ChatNotificationEvent`, `ExtensionsReloadedEvent`, `ExtensionCommandsChangedEvent`, `CommandsChangedEvent`, `ExtensionCommandResultEvent`, `InteractivePromptRequestedEvent`, `SessionsListedEvent`, `SessionLoadedEvent`, `SessionDeletedEvent`, `SessionExportedEvent`
  - **Non-ChatEvent bus types** (same file): `ScheduleTickEvent`, `PluginLifecycleEvent`
  - **ChatRuntime** interface: `Send(cmd ChatCommand) error`, `Close()`
  - **Core types**: `ChatMessage`, `ChatRole`, `ChatSessionState`, `ChatSessionConfig`, `ChatSessionPatch`, `ChatSessionStatus`, `ChatParameters`, `ChatModelRef`, `ChatToolCall`, `ChatToolDef`, `ChatUsage`, `CommandRef`, `ExtensionCommand`, `ExtensionReloader`, `SessionSummary`, `ChatCompletionRequest`, `ChatCompletionResponse`, `ChatCompletionChunk`, `ChatToolCallDelta`, etc.
- `internal/chat/stream.go` — `CompletionResult`, `StreamCallbacks` (OnDelta, OnReasoningDelta, OnToolCallDelta), `CloneChatSessionState`

### Changing the Agent Coordinator (Turn Loop)

- `internal/agent/coordinator.go` — `Coordinator` struct (the agent runtime that implements `ChatRuntime`):
  - **Config**: `CoordinatorConfig` — TokenSource, Streamer, Registry, MaxToolIterations, ParallelToolCalls, ShowReasoning, ExtensionReloader, SessionStore, OnPluginEvent, ScheduleInterval
  - **Lifecycle**: `NewCoordinator()`, `Send()`, `Close()`, `loop()`, `runTurn()`
  - **Command handlers**: `handleStart()`, `handleSubmit()`, `handleSteer()`, `handleUpdate()`, `handleCancel()`, `handleReset()`, `handleClose()`, `handleReloadExtensions()`, `handleRunExtensionCommand()`, `handleListSessions()`, `handleLoadSession()`, `handleDeleteSession()`, `handleExportSession()`, `handleInteractiveResponse()`
  - **Tool execution**: `executeToolsParallel()`, `mergeToolCallDelta()`
  - **Event emission**: `emit()` (non-blocking), `emitMustDeliver()` (bounded-blocking for terminal events)
  - **Plugin dispatch**: `dispatchPluginEvent()`, `broadcastTurnLifecycle()`, `applyPluginMessageModifications()`
- `internal/agent/prompt.go` — System prompt construction: `PromptConfig`, `BuildSystemPrompt()`, `DiscoverContextFiles()`, `BuildPrompt()` (for built-in command templates)
- `internal/agent/ui_bridge.go` — `coordinatorUIBridge` implementing `tools.UIBridge` (Confirm, Select, Input, Notify)
- `internal/agent/templates/*.md.tpl` — Go text/template prompt templates

### Changing the Tool Registry / Adding Tools

- `internal/agent/tools/registry.go` — `Registry` struct, `Tool` struct, `Schema`, `Result`, `UIBridge` interface:
  - **Registry methods**: `Register()`, `Replace()`, `Unregister()`, `Get()`, `All()`, `Schemas()`, `Names()`, `Count()`
  - **Plugin tools**: `RegisterPluginTool()`, `UnregisterPluginTools()`, `SetPluginToolExecutor()`, `PluginToolDef`, `PluginToolExecutor`
- `internal/agent/tools/builtin.go` — `RegisterBuiltins()` — registers all built-in tools
- `internal/agent/tools/read.go` — File reading tool
- `internal/agent/tools/write.go` — File writing tool (queued via MutationQueue)
- `internal/agent/tools/edit.go` — File editing tool (queued via MutationQueue)
- `internal/agent/tools/patch.go` — File patching tool (queued via MutationQueue)
- `internal/agent/tools/shell.go` — Shell command execution tool
- `internal/agent/tools/find.go` — File finding tool
- `internal/agent/tools/grep.go` — Content searching tool
- `internal/agent/tools/ls.go` — Directory listing tool
- `internal/agent/tools/docs.go` — tau documentation search/read tools (SearchDocs, ReadDoc)
- `internal/agent/tools/mutation.go` — `MutationQueue` for write/edit/patch operations
- `internal/agent/tools/fsutil.go` — Filesystem utilities
- `internal/agent/tools/pathutil.go` — Path resolution utilities
- `internal/agent/tools/truncate.go` — Content truncation utilities
- `internal/agent/tools/bridge.go` — UIBridge implementation for interactive prompts

### Changing the TUI (Interactive Chat UI)

- `internal/tui/inline_chat.go` — `inlineChat` struct (root taui component):
  - **State**: `provider`, `modelName`, `debug`, `sessionID`, `showReasoning`, `reasoningEffort`, `availableModels`, `registryCommands`, `extensionCommands`, `sessionSummaries`, `turnText`, `turnReasoning`, `activeTools`, `working`, `running`
  - **Lifecycle**: `newInlineChat()`, `close()`, `eventLoop()`, `spinnerLoop()`, `statusLoop()`
  - **Event handling**: `onRuntimeEvent()` — routes all `ChatEvent` types to state updates and rendering
  - **Notification handling**: queue-based notification system
  - **Rendering**: taui `Completions`, `LineInput`, `Paragraph`, `Container`, `Text`, `ToolRow`, `Box` widgets
- `internal/tui/run.go` — `Run()` entry point, delegates to `RunInline()`
- `internal/tui/run_taui.go` — `RunInline()` entry point for taui-based inline rendering
- `internal/tui/api.go` — `TUIConfig` struct, `ModelRefresher` type
- `internal/tui/inline_commands.go` — Slash command table: `/model`, `/system`, `/temperature`, `/max-tokens`, `/reset`, `/reasoning`, `/refresh`, `/sessions`, `/export`, `/help`, `/debug`, `/quit`, and extension commands. Structured as `slashCommand` entries with `run` and `complete` closures.
- `internal/tui/inline_completions.go` — Dynamic completion provider: `completionSet()` returns `*taui.CompletionSet` for the inline input. Resolves command names first, then per-command argument completions (models, sessions, boolean toggles, etc.).

### Changing the Command Registry

- `internal/registry/registry.go` — `Registry` struct, `Command` struct:
  - **Lifecycle**: `New()`, `Discover()`, `Commands()`, `Register()`, `Unregister()`, `Publish()`
  - **Sources**: `sources.go` — Built-in command definitions
  - **Bus integration**: Publishes `chat.CommandsChangedEvent` so TUI updates completions

### Changing Custom Commands

- `internal/chat/commands/commands.go` — `CustomCommand` struct, `Argument` struct:
  - **Loading**: `LoadCustomCommands()`, `LoadSkillCommands()`, `LoadProjectSkillCommands()`
  - **Parsing**: `loadCommand()`, `extractArgNames()`, `buildCommandID()`
  - **Command sources**: user-level (`~/.config/tau/commands/`) and project-level (`.tau/commands/`)
  - **Naming**: `"user:"` and `"project:"` prefixes, colon-separated path segments

### Changing Skills (Discovery & Lifecycle)

- `internal/skills/skills.go` — `Skill` struct, `Source`, `Scope`, `Diagnostic`:
  - **Discovery**: `Discover()`, `UserSources()`, `ProjectSources()`, `DefaultSources()`
  - **Parsing**: `Parse()`, `splitFrontmatter()`, `unmarshalFrontmatter()`
  - **Filtering**: `FilterDisabled()`, `FilterUserInvocable()`, `HasErrors()`
  - **Rendering**: `ToPromptIndex()`, `ToPromptXML()`
- `internal/skills/manager.go` — Runtime skill lifecycle management
- `internal/skills/tracker.go` — Skill activation tracking

### Changing Configuration

- `internal/config/config.go` — `Config` struct, `ProviderConfig`, `AuthConfig`, `ModelConfig`, `UIConfig`:
  - **Loading**: `LoadConfig()`, `LoadConfigFrom()`, `mergeConfigs()`, `Validate()`
  - **Paths**: `Dir()`, `GlobalPath()`, `LocalPath()`, `SessionsDir()`, `SessionsDBPath()`
  - **Selection**: `ResolveProvider()`, `ProviderNames()`
  - **YAML unmarshaling**: Supports both kebab-case and camelCase variants for all fields
- Config file: `~/.config/tau/config.yaml` (global), `.tau.yaml` (project-local)

### Changing the Event Bus

- `internal/eventbus/bus.go` — `Bus` (single bus for all event types), `Client`, `PublishedEvent`, `DeliveredEvent`:
  - `New()` — creates a bus with a single pump goroutine
  - `bus.Client(name)` — creates a named handle; a client owns its publishers and subscribers
  - `bus.Close()` — stops the pump, closes all clients
  - **Routing**: Events are routed by `reflect.Type` — `PublishedEvent.Type` carries the publisher's declared type parameter, so interface-typed publishers (`Publisher[ChatEvent]`) route correctly to subscribers of the same interface
  - **Internal primitives**: `worker` (goroutine lifecycle), `stopFlag` (one-way shutdown signal), `clientSet`/`publisherSet` (map-based sets)
- `internal/eventbus/publish.go` — `Publisher[T]`, `publisherCore`, `Client`, `Publish[T]()`:
  - `Publish[T](client)` — returns a typed publisher; create one per event type per client
  - `pub.Publish(event)` — blocks briefly if the bus write channel is full (backpressure)
  - `pub.ShouldPublish()` — check if any subscriber is interested (skip expensive event construction)
  - `pub.Close()` — stops the publisher
  - **Design**: `Publisher[T]` is a thin typed facade over non-generic `publisherCore` so the per-Client publisher set doesn't pay per-T itab/dictionary cost
- `internal/eventbus/subscribe.go` — `Subscriber[T]`, `SubscriberFunc[T]`, `subscriberCore`, `subscribeState`, `Subscribe[T]()`:
  - `Subscribe[T](client)` — returns a typed subscriber; one per type per client
  - `SubscribeFunc[T](client, fn)` — callback-based subscriber (fn called synchronously)
  - `sub.Events()` — returns `<-chan T` for receiving events
  - `sub.Close()` — stops the subscriber and unregisters from the bus
  - **Design**: `Subscriber[T]` is a thin typed facade over non-generic `subscriberCore`. The `dispatchTyped` method is generic because the typed channel send must appear lexically inside the select; a bridge goroutine would cost ~2.7x throughput
  - **Slow subscriber detection**: logs a warning if a subscriber blocks for >5 seconds
- `internal/eventbus/queue.go` — Generic bounded ring buffer used internally by the bus pump and per-client dispatch pumps

### Event Bus Usage Table

| Client | Publisher Type | Subscriber Type | Where Created | Where Subscribed |
| ------ | -------------- | --------------- | ------------- | ---------------- |
| `"coordinator"` | `ChatEvent` | — | `agent.NewCoordinator` | — |
| `"tui"` | — | `ChatEvent` | `tui.RunInline` | `inlineChat.eventLoop` |
| `"skills"` | `skills.Event` | — | `skills.NewManager` | (nothing yet) |
| `"registry"` | `chat.CommandsChangedEvent` | — | `registry.New` | — |
| `"coordinator"` | `chat.PluginLifecycleEvent` | — | `agent.NewCoordinator` | — |

### When to Use the Event Bus

- **DO** use the event bus when one subsystem needs to broadcast typed events to one or more unknown subscribers (e.g., coordinator publishes `ChatEvent`, TUI subscribes).
- **DO** create a new `Client` for each subsystem. Give it a short, unique name for debugging.
- **DO** define event types in a shared package (like `chat`), not in the publishing or subscribing package.
- **DON'T** use the event bus for point-to-point communication (like `ChatCommand` → coordinator). Use a direct channel or method call for that.
- **DON'T** create multiple subscribers for the same type on the same client — it will panic.
- **DON'T** block for extended periods in a `SubscribeFunc` callback — it blocks the client's dispatch goroutine.

### Changing the App Orchestration Layer

- `internal/app/chat.go` — `ChatOptions`, `buildCoordinator()`, `buildSessionConfig()`, `buildAgentSystemPrompt()`, `pickModel()`, `buildModelRefresher()`, `buildStreamer()`, `printExitSummary()`
- `internal/app/run.go` — `RunChat()` (interactive entry point), wires config → coordinator → TUI
- `internal/app/stdin.go` — `RunStdIn()` (headless/stdin entry point)
- `internal/app/streamer.go` — `Streamer` struct (ai-sdk adapter implementing `agent.Streamer`)
- `internal/app/platform.go` — `ResolveToken()`, `ModelsOptions`, model discovery via ai-sdk runtime
- `internal/app/id.go` — Session ID generation
- `internal/app/doc.go` — Package-level documentation

### Changing the CLI

- `internal/cli/root.go` — `NewRootCommand()`, flag definitions, provider/model resolution
- `internal/cli/commands.go` — Subcommands (`token`, `models`, `sessions`)
- `internal/cli/plugin_source.go` — Plugin source configuration

### Changing Session Persistence

- `internal/store/sqlite_store.go` — `SQLiteStore` implementing `SessionStore`
- `internal/store/session.go` — `SessionStore` interface: `Save()`, `Load()`, `List()`, `Delete()`, `ExportMessages()`
- `internal/store/migrate.go` — Schema migrations
- `internal/store/jsonl_export.go` — JSONL export: `ExportSessionAsJSONL()`
- `internal/sessions/manager.go` — `Manager` struct: session lifecycle (create, update, close, branch), wraps coordinator and store

### Changing the Plugin/Extension System

- `internal/plugin/manager.go` — `Manager` struct, `Load()`, `Unload()`, `ReloadExtensions()`, `ExtensionCommands()`, `RunExtensionCommand()`, `DispatchEvent()`, `ExecutePluginTool()`
- `internal/plugin/exec_unix.go` / `exec_windows.go` — Platform-specific execution helpers
- `pkg/plugin/api/plugin.go` — `EventPayload`, `EventResponse`, plugin lifecycle events
- `pkg/plugin/api/adapters.go` — Adapters for plugin integration
- `pkg/plugin/api/extension.pb.go` / `extension_grpc.pb.go` — gRPC protocol definitions (generated)
- `plugins/mcp/main.go` — MCP plugin

### Changing Provider/API Integration

LLM provider integration is handled through the external [`github.com/samcharles93/ai-sdk`](https://github.com/samcharles93/ai-sdk) library:

- `internal/app/streamer.go` — `Streamer` struct adapting ai-sdk `chat.Provider` into `agent.Streamer` interface
- `internal/app/platform.go` — Token resolution (`ResolveToken`), model discovery, runtime construction via ai-sdk `runtime.Runtime`
- Model metadata is sourced from the [models.dev](https://models.dev) catalog, cached at `~/.config/tau/models.json`, with optional overrides at `~/.config/tau/api.overrides.json`
- See [`docs/ai-sdk.md`](docs/ai-sdk.md) for full integration documentation

### Changing Search / Indexing

- `internal/indexing/indexing.go` — Full-text search index backed by Bleve. `SearchIndex` wraps bleve with document serialization, indexing, and search operations.

---

## Key Interfaces

### ChatRuntime (the command/event boundary)

```go
// internal/chat/types.go
type ChatRuntime interface {
    Send(cmd ChatCommand) error
    Close()
}
```

Implemented by `agent.Coordinator`. The TUI sends commands through this interface and subscribes to events directly on the event bus (see below).

### EventBus — Publisher / Subscriber (Bus → Subsystems)

```go
// internal/eventbus/bus.go
type Bus struct { ... }
func New() *Bus
func (b *Bus) Client(name string) *Client
func (b *Bus) Close()

// internal/eventbus/publish.go
func Publish[T any](c *Client) *Publisher[T]

type Publisher[T any] struct { ... }
func (p *Publisher[T]) Publish(v T)
func (p *Publisher[T]) ShouldPublish() bool
func (p *Publisher[T]) Close()

// internal/eventbus/subscribe.go
func Subscribe[T any](c *Client) *Subscriber[T]
func SubscribeFunc[T any](c *Client, f func(T)) *SubscriberFunc[T]

type Subscriber[T any] struct { ... }
func (s *Subscriber[T]) Events() <-chan T
func (s *Subscriber[T]) Done() <-chan struct{}
func (s *Subscriber[T]) Close()
```

Events are routed by Go type, not string topics. `Publisher[ChatEvent]` delivers to all `Subscriber[ChatEvent]` instances. The bus serializes all publications through a single pump goroutine, establishing total order. Each `Client` gets its own dispatch goroutine so clients progress independently.

### ChatCommand (TUI → Runtime)

```go
type ChatCommand interface{ IsChatCommand() }
// Concrete commands: StartChatSessionCommand, SubmitChatPromptCommand,
// SteerChatPromptCommand, UpdateChatSessionCommand, CancelChatRequestCommand,
// ResetChatSessionCommand, CloseChatSessionCommand, ReloadExtensionsCommand,
// RunExtensionCommandCommand, RespondInteractivePromptCommand,
// ListSessionsCommand, LoadSessionCommand, DeleteSessionCommand, ExportSessionCommand
```

### ChatEvent (Runtime → TUI)

```go
type ChatEvent interface{ IsChatEvent() }
// Concrete events: ChatSessionSnapshotEvent, ChatResponseStartedEvent,
// ChatResponseDeltaEvent, ChatReasoningDeltaEvent, ChatToolCallDeltaEvent,
// ChatToolOutputEvent, ChatToolExecutionStartedEvent,
// ChatToolExecutionCompletedEvent, ChatResponseCompletedEvent,
// ChatResponseCancelledEvent, ChatRuntimeErrorEvent, ChatNotificationEvent,
// ExtensionsReloadedEvent, ExtensionCommandsChangedEvent,
// CommandsChangedEvent, ExtensionCommandResultEvent,
// InteractivePromptRequestedEvent, SessionsListedEvent,
// SessionLoadedEvent, SessionDeletedEvent, SessionExportedEvent
```

### Non-ChatEvent Bus Types

```go
// Published on the event bus at a configurable interval for background work.
type ScheduleTickEvent struct { OccurredAt time.Time }

// Published for plugin lifecycle notifications (separate topic from ChatEvent).
type PluginLifecycleEvent struct {
    Event     string
    SessionID string
    Payload   any // *api.EventPayload at rest
}
```

### Streamer (LLM API calls)

```go
// internal/agent/coordinator.go
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

### TokenSource (Bearer token resolution)

```go
type TokenSource func(ctx context.Context, provider config.ProviderConfig) (string, error)
```

### UIBridge (Tool ↔ TUI interaction)

```go
// internal/agent/tools/registry.go
type UIBridge interface {
    Confirm(ctx context.Context, title, description string) (bool, error)
    Select(ctx context.Context, title string, options []string) (string, error)
    Input(ctx context.Context, title, placeholder string) (string, error)
    Notify(title, level string)
}
```

### SessionStore (Persistence)

```go
// internal/store/session.go
type SessionStore interface {
    Save(ctx context.Context, state chat.ChatSessionState, duration time.Duration) error
    Load(ctx context.Context, sessionID string) (chat.ChatSessionState, error)
    List(ctx context.Context, limit int, cursor string) ([]SessionSummary, string, error)
    Delete(ctx context.Context, sessionID string) error
    ExportMessages(ctx context.Context, sessionID string) (<-chan []byte, <-chan error)
    Close() error
}
```

### ExtensionReloader (Plugin management)

```go
// internal/chat/types.go
type ExtensionReloader interface {
    ReloadExtensions(ctx context.Context, idle bool) (ExtensionReloadResult, error)
    ExtensionCommands() []ExtensionCommand
    RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error)
}
```

---

## Design Patterns

- **Functional Options**: taui `TUI` and widgets use option functions for configuration
- **Command/Event Boundary**: TUI sends `ChatCommand` (point-to-point); Coordinator publishes `ChatEvent` on the event bus (broadcast). TUI subscribes directly to the bus — neither side imports the other, only `eventbus` and shared types in `chat`
- **Type-as-Topic**: The event bus routes by `reflect.Type`, not string constants. `Publisher[ChatEvent]` delivers to all `Subscriber[ChatEvent]`. Interface types work: routing uses the publisher's declared type parameter, not the concrete value type
- **Non-generic Core + Typed Facade**: Internal plumbing (`publisherCore`, `subscriberCore`) is non-generic to avoid per-T itab/dictionary/stencil costs; user-facing types (`Publisher[T]`, `Subscriber[T]`) are thin generic wrappers
- **Client Lifecycle**: Each subsystem gets a named `Client`. `Client.Close()` cascades to all publishers and subscribers created through it. `Bus.Close()` closes all clients
- **Inline Rendering**: The TUI renders inline (scrolls into terminal scrollback) rather than using an alternate screen for the main chat. Alternate screen is not used.
- **taui Widget Tree**: The UI is built as a tree of taui widgets (`TUI` → `Box` → `Text`/`LineInput`/`Completions`/`Paragraph`/`ToolRow`). Widgets implement `taui.Element` and the tree is re-rendered on each frame.
- **Reactive State via closure**: State changes trigger `c.engine.RequestRender()` to schedule the next frame. No reactive state framework — rendering pulls state from the `inlineChat` struct on each frame.
- **Channel Watchers**: `eventbus.Subscriber.Events()` channels are received in the event loop goroutine (`eventLoop()`), which dispatches to state mutations and requests re-renders.
- **Completions as a taui Widget**: Tab-completions are a `taui.Completions` widget that takes a `CompletionSet` function and fuzzy-filters against the current token under the cursor.
- **Slash Command Table**: All slash commands are defined in a single table (`slashCommands` slice) with `name`, `aliases`, `usage`, `description`, `run`, and `complete` fields. Completions and help are derived from this table — single source of truth.
- **Backpressure as Feature**: The bus pump's internal queue is bounded (16 events). Slow subscribers cause backpressure — this is by design; slow subscribers are bugs that must be fixed, not worked around
- **Leaf Infrastructure**: `config`, `eventbus` have zero internal imports — safe for any package to use
- **API-first Events**: All TUI communication through typed events, not direct function calls across layers

---

## TUI Architecture

### File Layout

```tree
internal/tui/
├── inline_chat.go       # inlineChat — root component, event loop, rendering, tool display
├── run.go               # Run() entry point, delegates to RunInline
├── run_taui.go          # RunInline — taui bootstrap, event subscription, cleanup
├── api.go               # TUIConfig, ModelRefresher
├── inline_commands.go   # Slash command table (/model, /system, /session, /debug, etc.)
├── inline_completions.go # Tab-completion engine (commands, models, args)
├── notify/              # Queue-based notification system
│   └── notify.go        # Notification, Queue (FIFO with expiry)
```

### Lifecycle

1. `app.RunChat()` creates the coordinator and TUIConfig, then calls `tui.Run()`.
2. `Run()` delegates to `RunInline()`, which creates the `taui.TUI` engine, subscribes to `ChatEvent` on the event bus, creates an `inlineChat` instance, and enters the render loop.
3. `inlineChat` starts three goroutines: `eventLoop()` (event dispatch), `spinnerLoop()` (working indicator), and `statusLoop()` (notification display).
4. `eventLoop()` receives `ChatEvent` values from the bus subscriber and calls `onRuntimeEvent()` to update state and request re-renders.
5. `onRuntimeEvent()` handles each event type — appending messages, updating tool status, managing streaming state.

### Command Handling

User input is processed by `inlineChat.onSubmit()`:
1. If input starts with `/`, look up the command in `slashByName` and call its `run` closure.
2. Otherwise, send a `SubmitChatPromptCommand` to the coordinator.

---

## Event Flow (End to End)

```flow
User types "/model gpt-4" → Tab
        │
        ▼
inlineChat.onSubmit() → inline_commands: handleModelCommand()
        │
        ▼
runtime.Send(UpdateChatSessionCommand{SessionID, Patch: {Model: &ref}})
        │
        ▼
Coordinator.handleUpdate() → session.state.ApplyPatch() → emit(ChatSessionSnapshotEvent)
        │
        ▼
inlineChat.eventLoop() → onRuntimeEvent(ChatSessionSnapshotEvent) → update state → engine.RequestRender()
        │
        ▼
taui re-renders status line with new model name
```

## Running Tests

```bash
go test ./...                        # Run all tests
go test -race ./...                  # Run all tests with race detector
go test ./internal/agent/...         # Run agent tests
go test ./internal/chat/...          # Run chat tests
go test ./internal/skills/...        # Run skill tests
go test ./internal/eventbus/...      # Run event bus tests
go test ./internal/plugin/...        # Run plugin tests
go test ./internal/store/...         # Run store tests
go test ./internal/agent/tools/...   # Run tool tests
go test ./internal/tui/...           # Run TUI tests
go test -run TestCoordinator ./...   # Run specific test
```

## Building

```bash
go build -o tau ./cmd/tau            # Build CLI binary
go install ./cmd/tau                 # Install to $GOPATH/bin
```
