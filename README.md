# Tau

[![Release](https://github.com/samcharles93/tau/actions/workflows/release.yml/badge.svg)](https://github.com/samcharles93/tau/actions/workflows/release.yml)

Tau is a provider-agnostic, OpenAI-compatible coding agent with an interactive
terminal UI, a built-in Web UI, agentic tool-calling, session persistence,
skills, and a plugin system.

- **Provider-agnostic** — works with any OpenAI-compatible API (DeepSeek,
  OpenRouter, Ollama, self-hosted, etc.); config or env vars, no code changes.
- **Terminal + Web UI** — a fast inline TUI by default, with an optional
  browser-based UI as a first-class peer over the same event stream.
- **Agentic tool use** — file read/write/edit/glob/grep, shell execution, and
  a plugin SDK for adding your own tools.
- **Skills** — drop a `SKILL.md` into a project or your user config to give
  the agent reusable, project-specific instructions and context.
- **Session persistence** — resume, list, and export past sessions.

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
# Create a project config (or ~/.config/tau/config.yaml for a global default)
cat > .tau.yaml <<EOF
default_provider: deepseek
default_model: deepseek-v4-flash

providers:
  - name: deepseek
    base_url: https://api.deepseek.com
    auth:
      type: api_key
      api_key_env: DEEPSEEK_API_KEY
EOF

export DEEPSEEK_API_KEY=sk-...

tau                                    # TUI + Web UI, URL in status bar
tau --web                              # also opens the browser automatically
tau --no-web                           # TUI only
tau -p "Explain the architecture of this codebase"   # single-shot, no UI
```

No config file yet? Run `tau` and use `/provider <name>` in the chat input to
enable a provider — it's saved to `~/.config/tau/auth.yaml`.
Full flag reference: [`docs/README.md`](docs/README.md).

## Web UI

Running `tau` starts a local HTTP/WebSocket server on `127.0.0.1` alongside
the terminal UI. The URL is printed at startup and shown in the TUI status
bar. Pass `--web` to open it in your default browser automatically.

The browser is a first-class peer to the TUI: it receives the same streaming
events and can send the same commands. Everything uses the existing
`ChatEvent`/`ChatCommand` contract over a WebSocket, documented in
[`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml).

## Configuration

Tau uses a YAML config file. By default, it looks for `.tau.yaml` in the
current directory, then `~/.config/tau/config.yaml`. Providers can also be
configured entirely via environment variables — no file required.

Reference examples:
- [`docs/config-example.yaml`](docs/config-example.yaml) — full annotated example
- [`docs/config-deepseek-example.yaml`](docs/config-deepseek-example.yaml) — minimal single-provider example
- [`docs/configuration.md`](docs/configuration.md) — all config keys explained

### Windows notes
- Bleve (the search backend) uses mmap, which locks index files on Windows.
  Avoid deleting or renaming opened index files to prevent "sharing
  violation" errors.
- Always use forward slashes (`/`) for paths in configuration and code.

## Documentation

Browse the docs at **[docs.tau-ai.dev](https://docs.tau-ai.dev)**, or read the
source directly in [`docs/`](docs):

| Topic | Doc |
| ----- | --- |
| CLI reference (flags, commands, providers, Web UI) | [`docs/README.md`](docs/README.md) |
| Architecture overview | [`docs/architecture.md`](docs/architecture.md) |
| Configuration reference | [`docs/configuration.md`](docs/configuration.md) |
| Provider system | [`docs/providers.md`](docs/providers.md) |
| Agent / coordinator turn loop | [`docs/agent.md`](docs/agent.md) |
| Built-in tools | [`docs/tools.md`](docs/tools.md) |
| Skills system | [`docs/skills.md`](docs/skills.md) |
| Plugin SDK | [`docs/plugins.md`](docs/plugins.md) |
| Slash commands | [`docs/commands.md`](docs/commands.md) |
| Session persistence | [`docs/sessions.md`](docs/sessions.md) |
| Event bus design | [`docs/eventbus.md`](docs/eventbus.md) |
| Chat event/command types | [`docs/chat-types.md`](docs/chat-types.md) |
| Terminal UI (taui) toolkit | [`docs/taui.md`](docs/taui.md) |
| TUI implementation | [`docs/tui.md`](docs/tui.md) |
| Web UI protocol (AsyncAPI) | [`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml) |
| Web UI technical spec | [`docs/specs/web-ui.md`](docs/specs/web-ui.md) |
| Web UI implementation | [`docs/webui.md`](docs/webui.md) |
| HTTP server | [`docs/server.md`](docs/server.md) |
| AI SDK integration & model catalog | [`docs/ai-sdk.md`](docs/ai-sdk.md) |

## Working on Tau with an AI agent

If you're an AI coding agent (or a human) making changes to this repository,
read [`AGENTS.md`](AGENTS.md) first — it covers commit conventions (required
for automated releases via release-please), code style/linting rules, and
project-specific guidelines enforced by `task check`. Start with
[`docs/architecture.md`](docs/architecture.md) for a map of how the packages
fit together before making structural changes.

## Development

```bash
task           # build the Go binary
task all       # build web UI SPA + Go binary (full build)
task check     # gofumpt, go vet, go fix, golangci-lint, go test — run before every commit
task test:race # run the test suite with the race detector
task build:all # cross-compile all release targets
```

See [`Taskfile.yaml`](Taskfile.yaml) for the full list of tasks (plugin
builds, per-platform cross-compiles, dead code detection, etc).
