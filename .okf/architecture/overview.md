---
description: Root architecture and project structure for tau.
resource: /work/apps/tau
tags:
    - overview
    - architecture
    - codebase
timestamp: "2026-07-21T18:36:12Z"
title: tau Overview
type: Architecture
---

# Overview

# Tau

[![Release](https://github.com/samcharles93/tau/actions/workflows/release.yml/badge.svg)](https://github.com/samcharles93/tau/actions/workflows/release.yml)

Tau is a provider-agnostic, OpenAI-compatible coding agent with an interactive terminal UI, a built-in Web UI, agentic
tool-calling, session persistence, skills, and a plugin system.

- **Provider-agnostic** - works with any OpenAI-compatible API (DeepSeek, OpenRouter, Ollama, self-hosted, etc.); config
  or env vars, no code changes.
- **Terminal + Web UI** - a fast inline TUI by default, with an optional browser-based UI as a first-class peer over the
  same event stream.
- **Agentic tool use** - file read/write/edit/glob/grep, shell execution, and a plugin SDK for adding your own tools.
- **Skills** - drop a `SKILL.md` into a project or your user config to give the agent reusable, project-specific
  instructions and context.
- **Session persistence** - resume, list, and export past sessions.

## Install

**Linux / macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/samcharles93/tau/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/samcharles93/tau/main/install.ps1 | iex
```

**Go toolchain:**

```bash
go install github.com/samcharles93/tau/cmd/tau@latest
```

**From source** (see [Development](#development) below):

```bash
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d   # install Task
task                                                                # build Go binary
```

## Quick start

```bash
# First run — interactive setup walks you through provider auth and model selection
tau

# Or use the setup command directly
tau setup

# Start chatting (TUI + Web UI, URL in status bar)
tau
tau --web                              # also opens the browser automatically
tau --no-web                           # TUI only
tau -p "Explain the architecture of this codebase"   # single-shot, no UI
```

No config file yet? Run `tau setup` or just `tau` — the first-run experience walks you through selecting a provider
and authenticating. Managed credentials are stored in `~/.config/tau/auth.yaml`.

### Managing providers

```bash
tau provider list                      # show all providers and auth state
tau provider login openai-codex        # start OAuth device-code flow
tau provider login                     # interactive selector on TTY
tau provider logout openai             # disable and clear credentials
```

Full flag reference: [`docs/README.md`](docs/README.md).

## Web UI

Running `tau` starts a local HTTP/WebSocket server on `127.0.0.1` alongside the terminal UI. The URL is printed at
startup and shown in the TUI status bar. Pass `--web` to open it in your default browser automatically.

The browser is a first-class peer to the TUI: it receives the same streaming events and can send the same commands.
Everything uses the existing `ChatEvent`/`ChatCommand` contract over a WebSocket, documented in
[`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml).

## Configuration

Tau uses a YAML config file. By default, it looks for `.tau.yaml` in the current directory, then
`~/.config/tau/config.yaml`. Providers can also be configured entirely via environment variables - no file required.

Reference examples:

- [`docs/config-example.yaml`](docs/config-example.yaml) - full annotated example
- [`docs/config-deepseek-example.yaml`](docs/config-deepseek-example.yaml) - minimal single-provider example
- [`docs/configuration.md`](docs/configuration.md) - all config keys explained

### Windows notes

- Bleve (the search backend) uses mmap, which locks index files on Windows. Avoid deleting or renaming opened index
  files to prevent "sharing violation" errors.
- Always use forward slashes (`/`) for paths in configuration and code.

## Documentation

Browse the docs at **[docs.tau-ai.dev](https://docs.tau-ai.dev)**, or read the source directly in [`docs/`](docs):

| Topic                                              | Doc                                                |
| -------------------------------------------------- | -------------------------------------------------- |
| CLI reference (flags, commands, providers, Web UI) | [`docs/README.md`](docs/README.md)                 |
| Architecture overview                              | [`docs/architecture.md`](docs/architecture.md)     |
| Configuration reference                            | [`docs/configuration.md`](docs/configuration.md)   |
| Provider system                                    | [`docs/providers.md`](docs/providers.md)           |
| Agent / coordinator turn loop                      | [`docs/agent.md`](docs/agent.md)                   |
| Built-in tools                                     | [`docs/tools.md`](docs/tools.md)                   |
| Skills system                                      | [`docs/skills.md`](docs/skills.md)                 |
| Plugin SDK                                         | [`docs/plugins.md`](docs/plugins.md)               |
| Slash commands                                     | [`docs/commands.md`](docs/commands.md)             |
| Session persistence                                | [`docs/sessions.md`](docs/sessions.md)             |
| Event bus design                                   | [`docs/eventbus.md`](docs/eventbus.md)             |
| Chat event/command types                           | [`docs/chat-types.md`](docs/chat-types.md)         |
| Terminal UI (taui) toolkit                         | [`docs/taui.md`](docs/taui.md)                     |
| TUI implementation                                 | [`docs/tui.md`](docs/tui.md)                       |
| Web UI protocol (AsyncAPI)                         | [`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml) |
| Web UI technical spec                              | [`docs/specs/web-ui.md`](docs/specs/web-ui.md)     |
| Web UI implementation                              | [`docs/webui.md`](docs/webui.md)                   |
| HTTP server                                        | [`docs/server.md`](docs/server.md)                 |
| AI SDK integration & model catalog                 | [`docs/ai-sdk.md`](docs/ai-sdk.md)                 |

## Working on Tau with an AI agent

If you're an AI coding agent (or a human) making changes to this repository, read [`AGENTS.md`](AGENTS.md) first - it
covers commit conventions (required for automated releases via release-please), code style/linting rules, and
project-specific guidelines enforced by `task check`. Start with [`docs/architecture.md`](docs/architecture.md) for a
map of how the packages fit together before making structural changes.

## Development

```bash
task           # build the Go binary
task all       # build web UI SPA + Go binary (full build)
task check     # gofumpt, go vet, go fix, golangci-lint, go test - run before every commit
task test:race # run the test suite with the race detector
task build:all # cross-compile all release targets
```

See [`Taskfile.yaml`](Taskfile.yaml) for the full list of tasks (plugin builds, per-platform cross-compiles, dead code
detection, etc).


# Codebase Navigation

* [main.go](/codebase/cmd/tau/main.md) - `cmd/tau/main.go`
* [main_test.go](/codebase/cmd/tau/main_test.md) - `cmd/tau/main_test.go`
* [version.go](/codebase/cmd/tau/version.md) - `cmd/tau/version.go`
* [docs.go](/codebase/docs/docs.md) - `docs/docs.go`
* [docs_test.go](/codebase/docs/docs_test.md) - `docs/docs_test.go`
* [compact.go](/codebase/internal/agent/compact.md) - `internal/agent/compact.go`
* [compact_test.go](/codebase/internal/agent/compact_test.md) - `internal/agent/compact_test.go`
* [coordinator.go](/codebase/internal/agent/coordinator.md) - `internal/agent/coordinator.go`
* [coordinator_agent_tools_test.go](/codebase/internal/agent/coordinator_agent_tools_test.md) - `internal/agent/coordinator_agent_tools_test.go`
* [coordinator_bash.go](/codebase/internal/agent/coordinator_bash.md) - `internal/agent/coordinator_bash.go`
* [coordinator_bash_test.go](/codebase/internal/agent/coordinator_bash_test.md) - `internal/agent/coordinator_bash_test.go`
* [coordinator_budget_test.go](/codebase/internal/agent/coordinator_budget_test.md) - `internal/agent/coordinator_budget_test.go`
* [coordinator_cancel_test.go](/codebase/internal/agent/coordinator_cancel_test.md) - `internal/agent/coordinator_cancel_test.go`
* [coordinator_extensions.go](/codebase/internal/agent/coordinator_extensions.md) - `internal/agent/coordinator_extensions.go`
* [coordinator_lifecycle_test.go](/codebase/internal/agent/coordinator_lifecycle_test.md) - `internal/agent/coordinator_lifecycle_test.go`
* [coordinator_persist.go](/codebase/internal/agent/coordinator_persist.md) - `internal/agent/coordinator_persist.go`
* [coordinator_persist_crud_test.go](/codebase/internal/agent/coordinator_persist_crud_test.md) - `internal/agent/coordinator_persist_crud_test.go`
* [coordinator_persist_test.go](/codebase/internal/agent/coordinator_persist_test.md) - `internal/agent/coordinator_persist_test.go`
* [coordinator_plugin.go](/codebase/internal/agent/coordinator_plugin.md) - `internal/agent/coordinator_plugin.go`
* [coordinator_skills.go](/codebase/internal/agent/coordinator_skills.md) - `internal/agent/coordinator_skills.go`
* [coordinator_turn.go](/codebase/internal/agent/coordinator_turn.md) - `internal/agent/coordinator_turn.go`
* [instantiate.go](/codebase/internal/agent/instantiate.md) - `internal/agent/instantiate.go`
* [instantiate_test.go](/codebase/internal/agent/instantiate_test.md) - `internal/agent/instantiate_test.go`
* [prompt.go](/codebase/internal/agent/prompt.md) - `internal/agent/prompt.go`
* [prompt_test.go](/codebase/internal/agent/prompt_test.md) - `internal/agent/prompt_test.go`
* [discover.go](/codebase/internal/agent/spec/discover.md) - `internal/agent/spec/discover.go`
* [discover_test.go](/codebase/internal/agent/spec/discover_test.md) - `internal/agent/spec/discover_test.go`
* [identity.go](/codebase/internal/agent/spec/identity.md) - `internal/agent/spec/identity.go`
* [resolve_test.go](/codebase/internal/agent/spec/resolve_test.md) - `internal/agent/spec/resolve_test.go`
* [spec.go](/codebase/internal/agent/spec/spec.md) - `internal/agent/spec/spec.go`
* [spec_test.go](/codebase/internal/agent/spec/spec_test.md) - `internal/agent/spec/spec_test.go`
* [transport.go](/codebase/internal/agent/stdio/transport.md) - `internal/agent/stdio/transport.go`
* [transport_test.go](/codebase/internal/agent/stdio/transport_test.md) - `internal/agent/stdio/transport_test.go`
* [tool_loop_breaker_test.go](/codebase/internal/agent/tool_loop_breaker_test.md) - `internal/agent/tool_loop_breaker_test.go`
* [tool_sanitize_test.go](/codebase/internal/agent/tool_sanitize_test.md) - `internal/agent/tool_sanitize_test.go`
* [agent.go](/codebase/internal/agent/tools/agent.md) - `internal/agent/tools/agent.go`
* [agent_session_id_test.go](/codebase/internal/agent/tools/agent_session_id_test.md) - `internal/agent/tools/agent_session_id_test.go`
* [agent_test.go](/codebase/internal/agent/tools/agent_test.md) - `internal/agent/tools/agent_test.go`
* [bridge.go](/codebase/internal/agent/tools/bridge.md) - `internal/agent/tools/bridge.go`
* [bridge_test.go](/codebase/internal/agent/tools/bridge_test.md) - `internal/agent/tools/bridge_test.go`
* [builtin.go](/codebase/internal/agent/tools/builtin.md) - `internal/agent/tools/builtin.go`
* [child_prompt.go](/codebase/internal/agent/tools/child_prompt.md) - `internal/agent/tools/child_prompt.go`
* [docs.go](/codebase/internal/agent/tools/docs.md) - `internal/agent/tools/docs.go`
* [docs_test.go](/codebase/internal/agent/tools/docs_test.md) - `internal/agent/tools/docs_test.go`
* [edit.go](/codebase/internal/agent/tools/edit.md) - `internal/agent/tools/edit.go`
* [edit_test.go](/codebase/internal/agent/tools/edit_test.md) - `internal/agent/tools/edit_test.go`
* [find.go](/codebase/internal/agent/tools/find.md) - `internal/agent/tools/find.go`
* [find_test.go](/codebase/internal/agent/tools/find_test.md) - `internal/agent/tools/find_test.go`
* [fsutil.go](/codebase/internal/agent/tools/fsutil.md) - `internal/agent/tools/fsutil.go`
* [grep.go](/codebase/internal/agent/tools/grep.md) - `internal/agent/tools/grep.go`
