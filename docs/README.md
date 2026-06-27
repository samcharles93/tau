# Tau CLI Reference

Tau is a provider-agnostic coding agent with an interactive terminal UI.
It provides an extensible, personalised environment for working with AI models,
featuring an interactive TUI, an optional Web UI, session history, token
tracking, and plugin integration.

## Usage

```bash
tau [global flags] [command]
```

Running `tau` with no arguments starts an interactive chat session and also
starts a local Web UI on `127.0.0.1`. You do not need a `--model` flag; if no
model is configured the session starts unselected and you pick one with `/model`.

## Global Flags

| Flag | Description |
| ---- | ----------- |
| `--provider <name>` | Provider name to use (e.g. `deepseek`, `openrouter`). Env: `TAU_PROVIDER` |
| `--model <id>` | Model ID to use. Also accepts `provider:model-id` or `provider/model-id` |
| `--max-tokens <n>` | Maximum completion tokens per response |
| `--temperature <f>` | Sampling temperature (0–2) |
| `--resume`, `-r <id\|latest>` | Resume a saved session by ID, or `latest` |
| `--prompt`, `-p <text>` | Single-shot mode: process prompt, print response, exit (no Web UI) |
| `--web` | Start the Web UI and open it in the default browser |
| `--port <n>` | HTTP port for the Web UI. Default `0` = OS-assigned ephemeral port |
| `--no-web` | Do not start the Web UI. TUI only |
| `--insecure` | Skip TLS certificate verification. Env: `TAU_INSECURE` |
| `--verbose` | Show debug messages on stderr. Env: `TAU_VERBOSE` |

## Enabling Providers

Tau discovers providers automatically. To enable a provider:

1. **Environment variable (simplest):** Export the provider's API key env var
   (e.g. `DEEPSEEK_API_KEY=sk-...`). Tau detects it at startup.
2. **TUI login:** Run `tau` and type `/login` in the chat input. Tau walks you
   through entering the key and saves it to `~/.config/tau/auth.yaml`.
3. **config.yaml:** Add a provider block (see Configuration below).

Run `/model` in the TUI to pick from all enabled providers' models.

## Web UI

When tau starts interactively, it binds a local HTTP/WebSocket server on
`127.0.0.1`. The URL is shown in the TUI status bar.

```bash
./tau              # TUI + Web UI, URL in status bar
./tau --web        # TUI + Web UI, also opens the browser
./tau --no-web     # TUI only
./tau --port 9343  # Fixed port
```

The browser connects to `/ws` and receives the same event stream as the TUI.
Commands sent from the browser are forwarded to the coordinator identically to
TUI commands. Multiple browser tabs may connect to the same session.

## Slash Commands (TUI)

| Command | Description |
| ------- | ----------- |
| `/model [id]` | Switch model; opens picker when called without an ID |
| `/login [provider]` | Enable a provider; lists all providers when called without an argument |
| `/logout <provider>` | Disable a provider / remove its saved key |
| `/refresh` | Re-discover models from all enabled providers |
| `/effort [level]` | Set reasoning effort (`off`, `low`, `medium`, `high`, `max`) |
| `/reasoning [on\|off]` | Toggle display of model reasoning content |
| `/system <prompt>` | Set the session system prompt |
| `/session [list\|info\|export\|delete\|<id>]` | Manage saved sessions |
| `/resume [id]` | Resume a saved session |
| `/new` | Start a fresh conversation (aliases: `/clear`, `/reset`) |
| `/reload` | Reload extensions/plugins |
| `/help` | Show all available commands |
| `/exit` | Quit tau |

## Subcommands

### `tau models`

List available models from the configured provider.

```bash
tau models [--json]
```

Uses the live models.dev catalog (network). For the model list used in the
interactive TUI, see the embedded snapshot (`internal/providers/snapshot/`).

### `tau refresh`

Force a refresh of the models.dev network catalog and list models.

```bash
tau refresh
```

Downloads the latest catalog for the configured provider. This affects the
`tau models` output and the snapshot generator, but interactive sessions use
the embedded offline snapshot (`internal/providers/snapshot/models.json`).

### `tau sessions`

List saved chat sessions.

```bash
tau sessions
```

Shows a table of sessions with ID, model, message count, tokens, cost, and
date. Use `/session` inside the TUI for richer session management.

### `tau token`

Print the resolved API key / bearer token for the selected provider.

```bash
tau token
```

Useful for debugging auth configuration.

## Configuration

Tau loads `~/.config/tau/config.yaml` (global) and `.tau.yaml` (project-local).
Example configs are in [`config-example.yaml`](config-example.yaml) and
[`config-deepseek-example.yaml`](config-deepseek-example.yaml).

### Minimal configuration

```yaml
# ~/.config/tau/config.yaml
default_provider: deepseek
default_model: deepseek-chat

providers:
  - name: deepseek
    base_url: https://api.deepseek.com/v1
    auth:
      type: api_key
      api_key_env: DEEPSEEK_API_KEY
```

Model metadata (context window, pricing, reasoning) is loaded from the embedded
snapshot — you do not need to repeat it in config. See
[`docs/ai-sdk.md`](ai-sdk.md) for full integration details.

### `ProviderConfig` fields

| Field | Description |
| ----- | ----------- |
| `name` | Provider identifier (matches tau's catalog ID) |
| `base_url` | API endpoint. Must include `/v1` when the provider uses it |
| `type` | ai-sdk class override (`anthropic` for Anthropic native API; omit for OpenAI-compatible) |
| `auth.type` | `api_key`, `none`, or `oauth_pkce` |
| `auth.api_key_env` | Environment variable name for the API key |
| `auth.api_key` | Literal API key (not recommended; prefer `api_key_env`) |
| `models` | Optional list of model overrides (see below) |

### Model overrides

Tau's embedded snapshot provides model metadata. Override individual fields
only when you need to customise them:

```yaml
providers:
  - name: openai
    auth:
      type: api_key
      api_key_env: OPENAI_API_KEY
    models:
      - id: gpt-5.4
        context_window: 200000
        default_max_tokens: 16384
```

## Documentation Index

### Architecture & Design
- [Architecture Overview](architecture.md) — system design, communication patterns, layer map
- [Event Bus Architecture](eventbus.md) — type-based event routing, publishers, subscribers

### Core Systems
- [Agent Coordinator](agent.md) — turn loop, command handlers, tool execution, prompts
- [Chat Types](chat-types.md) — complete command/event reference, core types
- [Tools](tools.md) — registry, built-in tools, mutation queue, UIBridge
- [Commands](commands.md) — slash command registry, custom commands

### UI Systems
- [Terminal UI (TUI)](tui.md) — inline rendering, widget tree, slash commands, completions, notifications
- [Web UI](webui.md) — Vue 3 SPA, state management, components, protocol, build
- [Server & Bridge](server.md) — HTTP/WebSocket server, event fan-out, wire format
- [taui Framework](taui.md) — standalone TUI rendering engine, widgets, color system

### Infrastructure
- [Configuration](configuration.md) — YAML config format, env vars, CLI flags
- [Providers](providers.md) — provider catalogue, auth state, model discovery, dynamic switching
- [AI SDK Integration](ai-sdk.md) — embedded snapshot, dynamic streamer, URL rules, reasoning
- [Sessions](sessions.md) — SQLite persistence, session lifecycle, JSONL export
- [Skills](skills.md) — SKILL.md discovery, frontmatter parsing, prompt rendering

### Extensibility
- [Plugin SDK](plugins.md) — gRPC extension system, tools, slash commands, lifecycle events
