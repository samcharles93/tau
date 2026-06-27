# Tau CLI Reference

Tau is a provider-agnostic, coding agent with an interactive terminal UI.
`tau` provides a highly extensible and personalised environment for working with AI,
featuring an interactive TUI, an optional Web UI, session history, token
tracking, and plugin integration.

## Usage

```bash
tau [global flags] [command]
```

By default, running `tau` starts an interactive chat session using your default
provider and model, and also starts a local Web UI on `127.0.0.1`.

## Global Flags

The following flags can be passed to the root `tau` command:

* **`--provider` `<name>`**  
    Specify the configured provider name to use (e.g., `openai`, `deepseek`, `openrouter`). Can also be set via the `TAU_PROVIDER` environment variable.
* **`--model` `<model-id>`**  
    Specify the model ID to use for the chat session (e.g., `gpt-5.5`, `claude-4-6-sonnet`). You can also specify the provider and model together in the format `--model provider:model-id` (e.g. `--model openrouter:nvidia/nemotron-3-ultra`) or the legacy `provider/model-id` form.
* **`--max-tokens` `<number>`**  
    Set the maximum completion tokens per response.
* **`--temperature` `<float>`**  
    Set the sampling temperature for model responses (controls creativity/randomness).
* **`--resume`, `-r` `<session-id>|latest`**  
    Resume a saved chat session. Provide a specific session UUID, or `latest` to resume the most recent session.
* **`--prompt`, `-p` `<prompt>`**  
    Run Tau in single-shot mode: process the prompt, print the model's response to stdout, and exit. The web UI is not started in this mode.
* **`--web`**  
    Start the Web UI and open it in the default browser.
* **`--port` `<number>`**  
    HTTP port for the Web UI. Use `0` (the default) to let the OS assign a free port.
* **`--no-web`**  
    Do not start the Web UI. Only the TUI is launched.
* **`--insecure`**  
    Skip TLS certificate verification. Can also be set via the `TAU_INSECURE` environment variable.
* **`--verbose`**  
    Show progress and debug messages on `stderr`. Can also be set via the `TAU_VERBOSE` environment variable.

## Web UI

When Tau starts interactively, it also binds a local HTTP/WebSocket server on localhost.

The URL is printed in the TUI status bar, launching the Web UI in the browser connects to `/ws` and receives the same `ChatEvent` stream that the TUI receives; messages sent from the browser are forwarded as `ChatCommand` values to the coordinator. The protocol is documented in [`docs/asyncapi/tau.yaml`](asyncapi/tau.yaml).

```bash
# Start TUI + Web UI, print URL in the terminal
./tau

# Start and immediately open the browser
./tau --web

# TUI only
./tau --no-web

# Use a fixed port
./tau --port 9343
```

## Subcommands

### `models`

List all available models from the configured provider.

```bash
tau models [flags]
```

**Flags:**

* **`--json`**  
    Output the list of models as JSON instead of a formatted table.

### `refresh`

Force a refresh of the models.dev catalog cache and list available models.

```bash
tau refresh
```

This downloads the latest catalog to `~/.config/tau/models.json`, merges any
`~/.config/tau/api.overrides.json`, and prints the models for the configured
provider.

### `sessions`

Manage and list saved chat sessions.

```bash
tau sessions
```

Shows a summary table of saved sessions including ID, model, message count,
tokens used, cost, and date.

### `token`

Print the resolved bearer token for the selected provider to standard output.

```bash
tau token
```

## Configuration

Tau loads its configuration from `~/.config/tau/config.yaml` and an optional
project-local `.tau.yaml`. Example configurations can be found in
[`config-example.yaml`](config-example.yaml) and
[`config-deepseek-example.yaml`](config-deepseek-example.yaml).

## See also

* [Plugin SDK](plugins.md) — build custom tools and slash commands for Tau.
