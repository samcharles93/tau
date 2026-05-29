# AIM Project Guidelines

## Strict Requirements

- Never hard code any values, paths, or configurations in the code. Always use environment variables or configuration files to manage such information.
- Ensure that all sensitive information like API keys, tokens or passwords are stored securely and not exposed in the codebase or logs and anything in the project repository must be ignored via .gitignore.
- This codebase uses gofumpt for formatting. All code must be formatted using gofumpt to maintain consistency and readability.
- All code must pass `golangci-lint` and `go fix ./...` checks before being committed to the repository. This ensures that the code adheres to best practices and is free of common issues.
- Never hard code colour values (hex literals like `"#DA1710"`) or define local colour variables. Always import and use the shared `internal/theme` package for all colours and semantic styles. If a new colour or style is needed, add it to `internal/theme/theme.go` first, then reference it from consuming code.

## Project Organisation

The project follows a **layered architecture** with a command/event boundary between the TUI and chat runtime. All internal packages live under `internal/`.

### Layers (top → bottom)

| Layer | Packages | Role |
| ----- | -------- | ---- |
| Entry point | `cmd/aim/` | Binary bootstrap; delegates to `cli` |
| CLI | `internal/cli/` | Command definitions & flag parsing (thin handlers) |
| Orchestration | `internal/app/` | Wires subsystems together for each use case (chat, token, models) |
| Domain | `internal/chat/`, `internal/skills/`, `internal/agent/` | Core business logic, commands, events |
| Presentation | `internal/tui/` | go-tui interactive terminal UI |
| Infrastructure | `internal/platform/`, `internal/maas/`, `internal/config/`, `internal/pubsub/`, `internal/theme/`, `internal/store/` | Auth, HTTP, API clients, config, event bus, theming, persistence |

### Package Responsibilities

- **`app`** — Service/orchestration layer. Resolves tokens, discovers models, creates the chat runtime, and launches the TUI or one-shot stream. CLI commands call `app.*` functions.
- **`cli`** — Thin command definitions using urfave/cli. Parses flags and delegates to `app`.
- **`chat`** — Chat runtime: session lifecycle, streaming, command dispatch, event publishing via pubsub.
- **`tui`** — go-tui interactive terminal UI. Consumes `chat.Runtime` events, sends commands.
- **`platform`** — Endpoint resolution, OAuth PKCE flow, token caching, HTTP client factory.
- **`maas`** — MaaS API integration: model discovery and token exchange.
- **`config`** — Loads `~/.config/aim/config.yaml`; foundation package with no internal imports.
- **`pubsub`** — Generic typed in-process pub/sub event bus (`Bus[T]`).
- **`theme`** — Shared brand colour palette and semantic go-tui styles. Leaf dependency with zero internal imports. All UI code must import colours and styles from here — never define local colour hex literals.
- **`skills`** — Skill discovery from YAML files, lifecycle management, activation tracking.
- **`agent`** — Agent behaviour, decision-making, notifications (the `agent/notify` sub-package is a pure domain layer with zero internal imports).
- **`store`** — Future persistence layer (SQLite + sqlc).

### Dependency Rules

1. **CLI → App → Domain/Infra** — never the reverse.
2. **Domain packages** (`chat`, `skills`, `agent`) may import infrastructure (`platform`, `pubsub`, `config`) but never `cli`, `app`, or `tui`.
3. **TUI** imports `chat`, `pubsub`, `theme`, and TUI-local packages only — never `app`, `cli`, `maas`, or `platform` directly.
4. **Infrastructure** packages (`config`, `pubsub`, `theme`) have zero internal imports — they are leaf dependencies.
5. **`app`** is the only package that may import both domain and infrastructure to wire them together.

### Communication Pattern

- TUI sends `ChatCommand` through the runtime's command channel.
- Runtime publishes `ChatEvent` through the pubsub bus.
- TUI subscribes to events and renders updates.
- No external message broker; in-process channels provide sufficient decoupling.
