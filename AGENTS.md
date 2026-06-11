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
- Never hard code colour values (hex literals like `"#DA1710"`) or define local colour variables. Always import and use the shared `internal/theme` package for all colours and semantic styles. If a new colour or style is needed, add it to `internal/theme/theme.go` first, then reference it from consuming code.

## Project Overview

Tau is a provider-agnostic, OpenAI-compatible chat client with an interactive terminal UI. It features an agentic tool-calling loop, plugin/extension system, skill discovery, session persistence, and a go-tui-based reactive UI.

## Architecture

```
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
        │       ├─► Client("tui")         — tui.ChatPanel
        │       ├─► Client("skills")      — skills.Manager
        │       └─► (extensible: any subsystem can become a Client)
        │
        ├─► internal/agent/coordinator.go (agentic turn loop)
        │       │
        │       ├─► internal/agent/tools/ (built-in tools + registry)
        │       ├─► internal/plugin/ (extension loading + execution)
        │       ├─► internal/skills/ (SKILL.md discovery)
        │       └─► internal/chat/ (types, commands, events)
        │
        ├─► internal/provider/ (API clients, model discovery, token resolution)
        │
        └─► internal/tui/ (go-tui interactive terminal UI)
                │
                ├─► internal/tui/views/ (fullscreen screens: Settings, Sessions, Debug)
                ├─► internal/tui/components/ (reusable widgets: lists, selects, toggles)
                ├─► internal/tui/layouts/ (structural frames, flex blocks, grids)
                └─► internal/tui/notify/ (queue-based notification system)
```

### Communication Flow

```
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
| Presentation | `internal/tui/` | go-tui interactive terminal UI |
| Infrastructure | `internal/platform/`, `internal/config/`, `internal/eventbus/`, `internal/theme/`, `internal/store/` | Auth, HTTP, API clients, config, event bus, theming, persistence |

### Package Responsibilities

- **`app`** — Service/orchestration layer. Resolves tokens, discovers models, creates the chat runtime, and launches the TUI or one-shot stream. CLI commands call `app.*` functions.
- **`cli`** — Thin command definitions using urfave/cli. Parses flags and delegates to `app`.
- **`chat`** — Chat types and contracts. Defines `ChatCommand`, `ChatEvent`, `ChatRuntime`, `ChatSessionState`, and all concrete event/command types. Consumed by every other package; exports only types (no behaviour).
- **`eventbus`** — Central in-process event bus. Routes events by Go type (`Publisher[ChatEvent]` → `Subscriber[ChatEvent]`) rather than string topics. Adapted from Tailscale's `util/eventbus` (BSD-3-Clause). The bus enforces total ordering of published events via a single pump goroutine. Clients are named handles that own their publishers and subscribers; `Client.Close()` cascades cleanup. Designed so that subsystems communicate through the bus without importing each other.
- **`agent`** — Coordinator runtime: agentic turn loop, tool execution, plugin lifecycle dispatch, session persistence. Receives `ChatCommand` via its command channel and publishes `ChatEvent` via the event bus.
- **`tui`** — go-tui interactive terminal UI. Consumes `chat.Runtime` events, sends commands. The UI presentation is divided into:
  - **Core (`internal/tui/`)**: Handles app bootstrapping, event watchers, state lifecycle, and main layout structure.
  - **Views (`internal/tui/views/`)**: Fully-encapsulated fullscreen screens/modals (SettingsView, SessionTreeView, SessionListView, DebugView) that react to state changes and receive delegated keyboard/mouse inputs.
  - **Components (`internal/tui/components/`)**: Small, reusable UI elements/widgets (e.g. lists, select dropdowns, toggles) that can be imported and composed into views.
  - **Layouts (`internal/tui/layouts/`)**: Shared layouts defining structural frames and spacing (flex blocks, grids) for terminal rendering.
- **`platform`** — Endpoint resolution, OAuth PKCE flow, token caching, HTTP client factory.
- **`config`** — Loads `~/.config/tau/config.yaml`; foundation package with no internal imports.
- **`theme`** — Shared brand colour palette and semantic go-tui styles. Leaf dependency with zero internal imports. All UI code must import colours and styles from here — never define local colour hex literals.
- **`skills`** — Skill discovery from YAML files, lifecycle management, activation tracking. Publishes `skills.Event` on the event bus when the catalog is refreshed.
- **`store`** — Session persistence layer (SQLite + sqlc).

### Dependency Rules

1. **CLI → App → Domain/Infra** — never the reverse.
2. **Domain packages** (`chat`, `skills`, `agent`) may import infrastructure (`platform`, `eventbus`, `config`) but never `cli`, `app`, or `tui`.
3. **TUI** imports `chat`, `eventbus`, `theme`, and TUI-local packages only — never `app`, `cli`, or `platform` directly.
4. **Infrastructure** packages (`config`, `eventbus`, `theme`) have zero internal imports — they are leaf dependencies.
5. **`app`** is the only package that may import both domain and infrastructure to wire them together.
6. **TUI Subpackage Dependencies**: Core TUI imports `views`, `components`, and `layouts`. Views import/compose `components` and `layouts`. Components and layouts must remain leaf dependencies within the TUI package (i.e. never import `views` or core TUI files) to ensure they are fully reusable across different pages and views.

### Communication Pattern

- TUI sends `ChatCommand` through the runtime's command channel (point-to-point).
- Coordinator publishes `ChatEvent` via `eventbus.Publisher[ChatEvent]` on the bus.
- TUI subscribes to events via `eventbus.Subscriber[ChatEvent]` and renders updates.
- Skills manager publishes `skills.Event` via `eventbus.Publisher[skills.Event]`.
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
   - All output to the TUI comes back through `tauchat.ChatEvent` interfaces over the single `ChatRuntime` subscription. Do not circumvent this architecture.

---

## Where to Look (By Task)

Use this section to quickly find the right files for a given change.

### Changing the ChatRuntime / Command & Event Contract

- `internal/chat/types.go` — All public types:
  - **Commands** (TUI → Runtime): `ChatCommand` interface, `StartChatSessionCommand`, `SubmitChatPromptCommand`, `UpdateChatSessionCommand`, `CancelChatRequestCommand`, `ResetChatSessionCommand`, `CloseChatSessionCommand`, `ReloadExtensionsCommand`, `RunExtensionCommandCommand`, `RespondInteractivePromptCommand`, `ListSessionsCommand`, `LoadSessionCommand`, `DeleteSessionCommand`, `ExportSessionCommand`
  - **Events** (Runtime → TUI): `ChatEvent` interface, `ChatSessionSnapshotEvent`, `ChatResponseStartedEvent`, `ChatResponseDeltaEvent`, `ChatReasoningDeltaEvent`, `ChatToolCallDeltaEvent`, `ChatToolExecutionStartedEvent`, `ChatToolExecutionCompletedEvent`, `ChatResponseCompletedEvent`, `ChatResponseCancelledEvent`, `ChatRuntimeErrorEvent`, `ChatNotificationEvent`, `ExtensionsReloadedEvent`, `ExtensionCommandsChangedEvent`, `ExtensionCommandResultEvent`, `InteractivePromptRequestedEvent`, `SessionsListedEvent`, `SessionLoadedEvent`, `SessionDeletedEvent`, `SessionExportedEvent`
  - **ChatRuntime** interface: `Send(cmd ChatCommand) error`, `SubscribeEvents(buffer int) (*pubsub.Subscription[ChatEvent], error)`, `Close()`
  - **Core types**: `ChatMessage`, `ChatRole`, `ChatSessionState`, `ChatSessionConfig`, `ChatSessionStatus`, `ChatParameters`, `ChatModelRef`, `ChatToolCall`, `ChatToolDef`, `ChatUsage`, `ExtensionCommand`, `ExtensionReloader`
- `internal/chat/stream.go` — `CompletionResult`, `StreamCallbacks` (OnDelta, OnReasoningDelta, OnToolCallDelta), `CloneChatSessionState`

### Changing the Agent Coordinator (Turn Loop)

- `internal/agent/coordinator.go` — `Coordinator` struct (the agent runtime that implements `ChatRuntime`):
  - **Config**: `CoordinatorConfig` — TokenSource, Streamer, Registry, MaxToolIterations, ParallelToolCalls, ShowReasoning, ExtensionReloader, SessionStore, OnPluginEvent, ScheduleInterval
  - **Lifecycle**: `NewCoordinator()`, `Send()`, `SubscribeEvents()`, `Close()`, `loop()`, `runTurn()`
  - **Command handlers**: `handleStart()`, `handleSubmit()`, `handleUpdate()`, `handleCancel()`, `handleReset()`, `handleClose()`, `handleReloadExtensions()`, `handleRunExtensionCommand()`, `handleListSessions()`, `handleLoadSession()`, `handleDeleteSession()`, `handleExportSession()`
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
- `internal/agent/tools/builtin.go` — `RegisterBuiltins()` — registers all built-in tools into a registry
- `internal/agent/tools/edit.go` — File editing tool
- `internal/agent/tools/find.go` — File finding tool
- `internal/agent/tools/grep.go` — Content searching tool
- `internal/agent/tools/ls.go` — Directory listing tool
- `internal/agent/tools/patch.go` — Patching tool
- `internal/agent/tools/mutation.go` — Mutation utilities
- `internal/agent/tools/fsutil.go` — Filesystem utilities

### Changing the TUI (Interactive Chat UI)

- `internal/tui/chatui.go` — `ChatPanel` struct (root go-tui component):
  - **Reactive state**: `messages`, `streamingContent`, `streamingReasoning`, `inputValue`, `status`, `lastError`, `notice`, `showHelp`, `showReasoning`, `modelName`, `availableModels`, `extensionCommands`, `activeRequestID`, `completions`, `completionIndex`, `showSettings`, `showDebug`, `showSessionList`, `showSessionTree`, `sessionSummaries`
  - **Lifecycle**: `NewChatPanel()`, `BindApp()`, `Watchers()`, `Render()`
  - **Event handling**: `handleRuntimeEvent()` — routes all `ChatEvent` types to state updates and rendering
  - **Notification handling**: `handleNotification()`
  - **State sync**: `syncState()`, `scrollToBottom()`, `appendMessage()`
- `internal/tui/run.go` — `Run()` entry point, `showSplash()`, `TUIConfig` creation from app layer
- `internal/tui/api.go` — `TUIConfig` struct, `ModelRefresher` type
- `internal/tui/commands.go` — Slash command handling: `handleSlashCommand()` (giant switch), `handleModelCommand()`, `handleSystemCommand()`, `handleReasoningCommand()`, `handleSessionCommand()`, `handleDebugCommand()`, `refreshModels()`, `handleSubmit()` / `handleSubmitWithDepth()`
- `internal/tui/completions.go` — Tab-completion system: `completionItem`, `completionTextArea`, `syncCompletions()`, `completionItems()`, `commandCompletions()`, `modelCompletions()`, `reasoningCompletions()`, `sessionCompletions()`, `debugCompletions()`, `builtinCompletionItems()`
- `internal/tui/keymap.go` — `KeyMap()` — app-level keyboard shortcuts (Escape, Tab, Ctrl+C, Ctrl+R, Enter, arrow keys)
- `internal/tui/render.go` — Content rendering: `printMessage()`, `printStyledAbovef()`, `userMessageBlock()`, `printSessionSummaries()`, `writeAssistantDelta()`, `writeReasoningDelta()`, `closeStream()`

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

- `internal/config/config.go` — `Config` struct, `ProviderConfig`, `AuthConfig`, `ModelConfig`, `UIConfig`, `CompatConfig`, `ThinkingConfig`, `CostConfig`:
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
|--------|---------------|-----------------|---------------|------------------|
| `"coordinator"` | `ChatEvent` | — | `agent.NewCoordinator` | — |
| `"tui"` | — | `ChatEvent` | `tui.Run` | `tui.ChatPanel.Watchers` |
| `"skills"` | `skills.Event` | — | `skills.NewManager` | (nothing yet) |
| `"test"` | — | `ChatEvent` | `chatui_test.go` | — |

### When to Use the Event Bus

- **DO** use the event bus when one subsystem needs to broadcast typed events to one or more unknown subscribers (e.g., coordinator publishes `ChatEvent`, TUI subscribes).
- **DO** create a new `Client` for each subsystem. Give it a short, unique name for debugging.
- **DO** define event types in a shared package (like `chat`), not in the publishing or subscribing package.
- **DON'T** use the event bus for point-to-point communication (like `ChatCommand` → coordinator). Use a direct channel or method call for that.
- **DON'T** create multiple subscribers for the same type on the same client — it will panic.
- **DON'T** block for extended periods in a `SubscribeFunc` callback — it blocks the client's dispatch goroutine.

### Changing the Theme / Colours

- `internal/theme/theme.go` — All brand colours and semantic styles:
  - **Colours**: `ColorDarkNavy`, `ColorWhite`, `ColorRed`, `ColorNavyBlue`, `ColorPurple`, `ColorLightGray`, `ColorDimGray`, `ColorGray800`, `ColorGreen`
  - **Splash palette**: `ColorCoral`, `ColorCoralDeep`, `ColorCoralDim`, `ColorLilac`, `ColorLilacMid`, `ColorLilacSoft`, `ColorLilacDim`
  - **Styles**: `BrandStyle()`, `SelectedStyle()`, `BodyStyle()`, `DimStyle()`, `ErrorStyle()`, `ReadyStyle()`, `BorderStyle()`
  - Hex constants (`HexDarkNavy`, etc.) are package-internal; consuming code uses the `Color*` vars and `*Style()` functions

### Changing the App Orchestration Layer

- `internal/app/chat.go` — `ChatOptions`, `buildCoordinator()`, `buildSessionConfig()`, `buildAgentSystemPrompt()`, `pickModel()`, `buildModelRefresher()`, `newCoordinator()`, `printExitSummary()`
- `internal/app/run.go` — `RunChat()` (interactive entry point), wires config → coordinator → TUI
- `internal/app/stdin.go` — `RunStdIn()` (headless/stdin entry point)
- `internal/app/platform.go` — Token resolution, platform integration
- `internal/app/id.go` — Session ID generation

### Changing the CLI

- `internal/cli/root.go` — `NewRootCommand()`, flag definitions, provider/model resolution
- `internal/cli/commands.go` — Subcommands (`token`, `models`, `sessions`)
- `internal/cli/plugin_source.go` — Plugin source configuration

### Changing Session Persistence

- `internal/store/sqlite_store.go` — `SQLiteStore` implementing `SessionStore`
- `internal/store/session.go` — `SessionStore` interface: `Save()`, `Load()`, `List()`, `Delete()`, `ExportMessages()`
- `internal/store/migrate.go` — Schema migrations
- `internal/store/jsonl_export.go` — JSONL export: `ExportSessionAsJSONL()`

### Changing the Plugin/Extension System

- `internal/plugin/manager.go` — `Manager` struct, `Load()`, `Unload()`, `ReloadExtensions()`, `ExtensionCommands()`, `RunExtensionCommand()`, `DispatchEvent()`, `ExecutePluginTool()`
- `pkg/plugin/api/plugin.go` — `EventPayload`, `EventResponse`, plugin lifecycle events
- `pkg/plugin/api/adapters.go` — Adapters for plugin integration
- `pkg/plugin/api/extension.pb.go` / `extension_grpc.pb.go` — gRPC protocol definitions (generated)
- `plugins/mcp/main.go` — MCP plugin

### Changing Provider/API Integration

- `internal/provider/openai.go` — `OpenAIStreamer` implementing `Streamer` interface
- `internal/provider/models.go` — `DiscoverModels()`, `Model` struct
- `internal/provider/modelsdev.go` — Development-mode model discovery
- `internal/provider/token.go` — `TokenSource` type, `ResolveBearerToken()`

---

## Key Interfaces

### ChatRuntime (the command/event boundary)

```go
// internal/chat/types.go
type ChatRuntime interface {
    Send(cmd ChatCommand) error
    SubscribeEvents(buffer int) (*pubsub.Subscription[ChatEvent], error)
    Close()
}
```

Implemented by `agent.Coordinator`. The TUI sends commands through this interface but subscribes to events directly on the event bus (see below).

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
// UpdateChatSessionCommand, CancelChatRequestCommand, ResetChatSessionCommand,
// CloseChatSessionCommand, ReloadExtensionsCommand, RunExtensionCommandCommand,
// RespondInteractivePromptCommand, ListSessionsCommand, LoadSessionCommand,
// DeleteSessionCommand, ExportSessionCommand
```

### ChatEvent (Runtime → TUI)

```go
type ChatEvent interface{ IsChatEvent() }
// Concrete events: ChatSessionSnapshotEvent, ChatResponseStartedEvent,
// ChatResponseDeltaEvent, ChatReasoningDeltaEvent, ChatToolCallDeltaEvent,
// ChatToolExecutionStartedEvent, ChatToolExecutionCompletedEvent,
// ChatResponseCompletedEvent, ChatResponseCancelledEvent,
// ChatRuntimeErrorEvent, ChatNotificationEvent, ExtensionsReloadedEvent,
// ExtensionCommandsChangedEvent, ExtensionCommandResultEvent,
// InteractivePromptRequestedEvent, SessionsListedEvent,
// SessionLoadedEvent, SessionDeletedEvent, SessionExportedEvent
```

### Streamer (LLM API calls)

```go
// internal/agent/coordinator.go
type Streamer interface {
    StreamChatCompletionFull(ctx context.Context, session chat.ChatSessionState,
        bearerToken string, extraHeaders map[string]string,
        cb chat.StreamCallbacks) (chat.CompletionResult, error)
}
```

### TokenSource (Bearer token resolution)

```go
type TokenSource = provider.TokenSource
// func(ctx context.Context, cfg config.ProviderConfig) (string, error)
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
    List(ctx context.Context, limit int, cursor string) ([]store.SessionSummary, string, error)
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

- **Functional Options**: go-tui `Element` and `App` use `Option`/`AppOption` funcs for configuration
- **Command/Event Boundary**: TUI sends `ChatCommand` (point-to-point); Coordinator publishes `ChatEvent` on the event bus (broadcast). TUI subscribes directly to the bus — neither side imports the other, only `eventbus` and shared types in `chat`
- **Type-as-Topic**: The event bus routes by `reflect.Type`, not string constants. `Publisher[ChatEvent]` delivers to all `Subscriber[ChatEvent]`. Interface types work: routing uses the publisher's declared type parameter, not the concrete value type
- **Non-generic Core + Typed Facade**: Internal plumbing (`publisherCore`, `subscriberCore`) is non-generic to avoid per-T itab/dictionary/stencil costs; user-facing types (`Publisher[T]`, `Subscriber[T]`) are thin generic wrappers
- **Client Lifecycle**: Each subsystem gets a named `Client`. `Client.Close()` cascades to all publishers and subscribers created through it. `Bus.Close()` closes all clients
- **Reactive State**: go-tui `State[T]` with `Bind()` callbacks and `OnChange()` watchers
- **Channel Watchers**: `eventbus.Subscriber.Events()` channels bridged into go-tui's event loop via `gt.Watch()`
- **Component Caching**: go-tui mount system with `MountPersistent()` — components are cached by (parent, index) key and reused across renders
- **Preemptive Key Dispatch**: `OnPreemptStop()` for modal/overlay key bindings that block parent handlers
- **Inline + Alternate Screen**: Inline mode for chat (conversation scrolls into terminal scrollback), alternate screen for Settings/SessionTree modals
- **Double Buffering**: go-tui `Buffer` maintains front/back grids with diff-based rendering
- **Backpressure as Feature**: The bus pump's internal queue is bounded (16 events). Slow subscribers cause backpressure — this is by design; slow subscribers are bugs that must be fixed, not worked around
- **Leaf Infrastructure**: `config`, `eventbus`, `theme` have zero internal imports — safe for any package to use
- **API-first Events**: All TUI communication through typed events, not direct function calls across layers

---

## TUI Subpackage Architecture

```
internal/tui/
├── chatui.go          # ChatPanel — root component, event routing, state management
├── run.go             # Run() entry point, splash display
├── api.go             # TUIConfig, ModelRefresher
├── commands.go        # Slash command dispatch (/model, /system, /session, /debug, etc.)
├── completions.go     # Tab-completion engine (commands, models, args)
├── keymap.go          # App-level keyboard shortcuts
├── render.go          # Content printing, message formatting, stream writing
├── util.go            # Utility helpers
├── views/             # Fullscreen alternate-screen views
│   ├── settings.go    # SettingsView — provider, model, reasoning toggles
│   ├── sessions.go    # SessionListView — flat session list
│   ├── sessiontree.go # SessionTreeView — tree-based session dashboard
│   └── debug.go       # DebugView, DebugListView — component introspection
├── components/        # Reusable leaf widgets
│   ├── list.go        # Selectable list
│   └── ...
├── layouts/           # Shared structural layouts
├── notify/            # Queue-based notification system
│   └── notify.go      # Notification, Queue (FIFO with expiry)
└── splash/            # τ glyph splash animation
```

### View Lifecycle Pattern

1. View is created with `gt.State[bool]` controlling visibility
2. `show*State.Set(true)` triggers `handle*VisibilityChanged()` watcher
3. Watcher calls `app.EnterAlternateScreen()` (or `ExitAlternateScreen()` on close)
4. `Render()` returns the view's element tree when `show*State` is true and app is in alternate screen
5. View receives keyboard/mouse events via go-tui's key dispatch system

---

## Event Flow (End to End)

```
User types "/model gpt-4" → Tab
        │
        ▼
ChatPanel.handleSubmit() → handleSlashCommand() → handleModelCommand()
        │
        ▼
runtime.Send(UpdateChatSessionCommand{SessionID, Patch: {Model: &ref}})
        │
        ▼
Coordinator.handleUpdate() → session.state.ApplyPatch() → emit(ChatSessionSnapshotEvent)
        │
        ▼
ChatPanel.handleRuntimeEvent(ChatSessionSnapshotEvent) → syncState() → modelName.Set()
        │
        ▼
go-tui re-renders status bar with new model name
```

## Running Tests

```bash
go test ./...                        # Run all tests
go test -race ./...                  # Run all tests with race detector
go test ./internal/agent/...         # Run agent tests
go test ./internal/chat/...          # Run chat tests
go test ./internal/skills/...        # Run skill tests
go test ./internal/pubsub/...        # Run pubsub tests
go test ./internal/plugin/...        # Run plugin tests
go test -run TestCoordinator ./...   # Run specific test
```

## Building

```bash
go build -o tau ./cmd/tau            # Build CLI binary
go install ./cmd/tau                 # Install to $GOPATH/bin
```
