# Tau CLI Reference

Tau is a provider-agnostic, OpenAI-compatible interactive chat client for the terminal. It provides a highly extensible and playful environment for working with LLMs, featuring interactive TUI sessions, session history, token tracking, and plugin integration.

## Usage

```bash
tau [global flags] [command]
```

By default, running `tau` starts an interactive chat session using your default provider and model.

## Global Flags

The following flags can be passed to the root `tau` command:

* **`--provider`, `-p` `<name>`**  
    Specify the configured provider name to use (e.g., `openai`, `openrouter`). Can also be set via the `TAU_PROVIDER` environment variable.
* **`--model` `<model-id>`**  
    Specify the model ID to use for the chat session (e.g., `gpt-5.5`, `claude-4.6-sonnet`). You can also specify the provider and model together in the format `--model provider/model-id` (e.g. `--model openrouter/nvidia/nemotron-3-ultra-550b-a55b`).
* **`--system-prompt` `<prompt>`**  
    Override the default system prompt for this chat session.
* **`--max-tokens` `<number>`**  
    Set the maximum completion tokens per response.
* **`--temperature` `<float>`**  
    Set the sampling temperature for model responses (controls creativity/randomness).
* **`--resume`, `-r` `<session-id>|latest`**  
    Resume a saved chat session. Provide a specific session UUID, or `latest` to resume the most recent session.
* **`--prompt` `<prompt>`**  
    Run Tau in single-shot mode: process the prompt, print the model's response to stdout, and exit.
* **`--insecure`**  
    Skip TLS certificate verification. Can also be set via the `TAU_INSECURE` environment variable.
* **`--verbose`**  
    Show progress and debug messages on `stderr`. Can also be set via the `TAU_VERBOSE` environment variable.

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

This downloads the latest `api.json` to `~/.config/tau/models.json`, merges
any `~/.config/tau/api.overrides.json`, and prints the models for the
configured provider.

### `sessions`

Manage and list saved chat sessions.

```bash
tau sessions
```

Shows a summary table of saved sessions including ID, model, message count, tokens used, cost, and date.

### `token`

Print the resolved bearer token for the selected provider to standard output.

```bash
tau token
```

## Configuration

Tau loads its configuration from `~/.config/tau/config.yaml`. An example configuration structure can be found in `config-example.yaml`.
