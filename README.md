# Tau

[![Release](https://github.com/samcharles93/tau/actions/workflows/release.yml/badge.svg)](https://github.com/samcharles93/tau/actions/workflows/release.yml)

Tau is a provider-agnostic, OpenAI-compatible coding agent with an interactive terminal UI, agentic tool-calling,
session persistence, skills, and a plugin system.

- **Provider-agnostic** - works with any OpenAI-compatible API (DeepSeek, OpenRouter, Ollama, self-hosted, etc.); config
  or env vars, no code changes.
- **Terminal-first** - a fast, keyboard-driven TUI built for long agentic sessions.
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

# Start chatting
tau
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

API keys entered through setup are stored securely in `~/.config/tau/auth.yaml` —
environment variables override them when both are present.

Full flag reference: [`docs/README.md`](docs/README.md).

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
| CLI reference (flags, commands, providers)         | [`docs/README.md`](docs/README.md)                 |
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
| Wire protocol (AsyncAPI)                           | [`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml) |
| AI SDK integration & model catalog                 | [`docs/ai-sdk.md`](docs/ai-sdk.md)                 |

## Working on Tau with an AI agent

If you're an AI coding agent (or a human) making changes to this repository, read [`AGENTS.md`](AGENTS.md) first - it
covers commit conventions (required for automated releases via release-please), code style/linting rules, and
project-specific guidelines enforced by `task check`. Start with [`docs/architecture.md`](docs/architecture.md) for a
map of how the packages fit together before making structural changes.

## Development

```bash
task           # build the Go binary
task check     # gofumpt, go vet, go fix, golangci-lint, go test - run before every commit
task test:race # run the test suite with the race detector
task build:all # cross-compile all release targets
```

See [`Taskfile.yaml`](Taskfile.yaml) for the full list of tasks (plugin builds, per-platform cross-compiles, dead code
detection, etc).
