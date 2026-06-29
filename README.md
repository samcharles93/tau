# Tau

[![Release](https://git.catlow.cloud/sam/tau/actions/workflows/release.yml/badge.svg)](https://git.catlow.cloud/sam/tau/actions/workflows/release.yml)

Provider-agnostic, OpenAI-compatible chat client with an interactive terminal
UI, a built-in Web UI, agentic tool-calling, and session persistence.

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

## Quick start

```bash
# Create a project config
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

# Chat (TUI + Web UI)
tau

# Open the browser automatically
tau --web

# TUI only
tau --no-web

# Single-shot
tau -p "Explain the architecture of this codebase"
```

## Web UI

Running `tau` starts a local HTTP/WebSocket server on `127.0.0.1` alongside
the terminal UI. The URL is printed at startup and shown in the TUI status
bar. Pass `--web` to open it in your default browser automatically.

The browser is a first-class peer to the TUI: it receives the same streaming
events and can send the same commands. Everything uses the existing
`ChatEvent`/`ChatCommand` contract over a WebSocket documented in
[`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml).

## Documentation

* [CLI reference](docs/README.md)
* [Plugin SDK](docs/plugins.md)
* [AI SDK integration and model catalog](docs/ai-sdk.md)
* [Event bus design](docs/eventbus.md)
* [Web UI protocol](docs/asyncapi/tau.yaml)
* [Web UI technical specification](docs/specs/web-ui.md)
* Example configs:
  * [Generic](docs/config-example.yaml)
  * [DeepSeek](docs/config-deepseek-example.yaml)

## Development

```bash
task          # build Web UI + Go binary
task check    # lint + format + test
task build:all # cross-compile all release targets
```
