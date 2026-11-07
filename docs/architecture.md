# Tau Architecture

Tau is a provider-agnostic, coding agent with an interactive terminal UI (TUI), session persistence,
plugin system, and skill discovery. This document describes the high-level architecture - how the pieces fit together,
the communication patterns, and the design principles.

## Architecture Diagram

```
cmd/tau/main.go (binary entry point)
        │ delegates to cli
        ▼
internal/cli/ (thin command definitions, flag parsing)
        │ delegates to app
        ▼
internal/app/ (orchestration: creates eventbus.Bus, wires subsystems as Clients)
        │
        ├─► Creates eventbus.Bus (central event router - type-based, no string topics)
        │       │
        │       ├─► Client("coordinator") - agent.Coordinator
        │       ├─► Client("tui")         - tui client (event subscription)
        │       ├─► Client("web")         - bridge client (WebSocket broadcast)
        │       ├─► Client("skills")      - skills.Manager
        │       ├─► Client("registry")    - command registry
        │       └─► (extensible: any subsystem can become a Client)
        │
        ├─► internal/agent/coordinator.go (agentic turn loop)
        │       │
        │       ├─► internal/agent/tools/ (built-in tools + registry)
        │       ├─► internal/plugin/ (extension loading + execution)
        │       ├─► internal/skills/ (SKILL.md discovery)
        │       └─► internal/chat/ (types, commands, events)
        │
        ├─► internal/app/streamer.go (dynamic provider adapter)
        ├─► internal/app/platform.go (token resolution via providers)
        │
        ├─► internal/tui/ (taui-based inline terminal UI)
        │       │
        │       └─► internal/tui/notify/ (queue-based notification system)
        │
        ├─► internal/bridge/ (JSON wire types for agent processes and JSONL output)
        │
        └─► internal/sessions/ (session lifecycle management)
                │
                └─► internal/store/ (SQLite persistence)
```

## Communication Flow

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

## Layer Map

| Layer           | Packages                                                                                                                       | Role                                                              |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------- |
| Entry point     | `cmd/tau/`                                                                                                                     | Binary bootstrap; delegates to `cli`                              |
| CLI             | `internal/cli/`                                                                                                                | Command definitions & flag parsing (thin handlers)                |
| Orchestration   | `internal/app/`                                                                                                                | Wires subsystems together for each use case (chat, token, models) |
| Domain          | `internal/chat/`, `internal/skills/`, `internal/agent/`                                                                        | Core business logic, commands, events                             |
| Presentation    | `internal/tui/`, `internal/tui2/`                                                                                              | Terminal UI                                                       |
| Infrastructure  | `internal/config/`, `internal/eventbus/`, `internal/store/`, `internal/sessions/`, `internal/indexing/`, `internal/providers/` | Config, event bus, persistence, search, provider management       |
| Public packages | `pkg/taui/`, `pkg/plugin/`                                                                                                     | Standalone TUI framework, plugin API                              |

## Dependency Rules

1. **CLI → App → Domain/Infra** - never the reverse.
2. **Domain packages** (`chat`, `skills`, `agent`) may import infrastructure (`config`, `eventbus`) but never `cli`,
   `app`, or `tui`.
3. **TUI** imports `chat`, `eventbus`, and TUI-local packages only - never `app`, `cli`, or `config` directly.
4. **Infrastructure** packages (`config`, `eventbus`) have zero internal imports - they are leaf dependencies.
5. **`app`** is the only package that may import both domain and infrastructure to wire them together.
6. **`pkg/taui`** has zero tau-internal imports - it is a standalone TUI framework dependency.
7. **`pkg/plugin`** is the public plugin API surface. Its gRPC/proto types (`pkg/plugin/api`) have zero tau-internal
   imports, but its adapter layer (`adapters.go`) does import `internal/chat` to map proto types (`Command`, `View`,
   `Widget`, ...) onto `internal/chat`'s domain types - this is intentional and predates the plugin-views feature (see
   `protoCommandsToChat`), not a violation of this rule's spirit.

## Core Principles

Tau is built on three pillars:

- **Extensible** - Tau provides primitives, not products. The session tree, event dispatch, schedule hook, tool
  registry, and plugin API are the platform. Everything else (GitHub polling, code review, deployment workflows) belongs
  in plugins or extensions.
- **Playful** - The tool should be a joy to use. Design interactions that delight. Error messages should be helpful, not
  cryptic. The TUI should feel responsive and alive.
- **Constant** - Using tau should produce strong, reliable outputs every time. Sessions are deterministic within their
  context. The agent loop is predictable and debuggable. Prompts are versioned templates, not ad-hoc strings.

## Subsystem Reference

| Subsystem         | Key Package                             | Documentation                     |
| ----------------- | --------------------------------------- | --------------------------------- |
| Agent Coordinator | `internal/agent/`                       | [Agent](agent.md)                 |
| Chat Types        | `internal/chat/`                        | [Chat Types](chat-types.md)       |
| Event Bus         | `internal/eventbus/`                    | [Event Bus](eventbus.md)          |
| TUI               | `internal/tui/`                         | [TUI](tui.md)                     |
| Wire Protocol     | `internal/bridge/`                      | [Chat Types](chat-types.md)       |
| Tools             | `internal/agent/tools/`                 | [Tools](tools.md)                 |
| Plugins           | `internal/plugin/`                      | [Plugin SDK](plugins.md)          |
| Skills            | `internal/skills/`                      | [Skills](skills.md)               |
| Commands          | `internal/registry/`                    | [Commands](commands.md)           |
| Sessions          | `internal/sessions/`, `internal/store/` | [Sessions](sessions.md)           |
| Configuration     | `internal/config/`                      | [Configuration](configuration.md) |
| Providers         | `internal/providers/`                   | [Providers](providers.md)         |
| TUI Framework     | `pkg/taui/`                             | [taui](taui.md)                   |
| Plugin API        | `pkg/plugin/api/`                       | [Plugin SDK](plugins.md)          |
| AI SDK            | `github.com/samcharles93/ai-sdk`        | [AI SDK](ai-sdk.md)               |
