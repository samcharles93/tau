# Tau AI SDK Integration

Tau delegates its LLM provider protocol handling to
[`github.com/samcharles93/ai-sdk`](https://github.com/samcharles93/ai-sdk),
a Go re-interpretation of the Vercel AI SDK. This document explains how the
integration works, where the boundary is, and how provider/model discovery is
configured.

## Why an external SDK?

Keeping the LLM protocol layer in a dedicated library means:

* Provider-specific quirks (DeepSeek reasoning, Anthropic thinking, Gemini
  native API, etc.) live in one place.
* Tau is insulated from provider wire-format churn.
* The same SDK can be reused by other Earendil projects (e.g. `pi`, `crush`,
  `fantasy`) without copying code into each repo.

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────────────────┐
│   Tau CLI   │────▶│   pkg/ai    │────▶│ github.com/samcharles93/ai- │
│  TUI / App  │     │  adapter    │     │ sdk                         │
│             │◀────│             │◀────│ (providers + core.Stream)   │
└─────────────┘     └─────────────┘     └─────────────────────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ models.dev  │
                    │  catalog    │
                    └─────────────┘
```

`pkg/ai` is a thin adapter. It does **not** replace tau's coordinator, event
bus, tool registry, or session store. It only replaces the raw
provider-protocol streaming implementation.

## Components

### `pkg/ai/catalog`

A `Catalog` loads the canonical [models.dev](https://models.dev) `api.json`
catalog from disk cache (`~/.config/tau/models.json`) or network. It merges in
an optional user overrides file (`~/.config/tau/api.overrides.json`) and
exposes provider metadata:

* `API(provider)` — default base URL.
* `NPM(provider)` — the ai-sdk package name (e.g. `@ai-sdk/deepseek`).
* `APIKeyEnv(provider)` — the environment variable that holds the API key.
* `Models(provider)` / `Model(provider, id)` — model metadata.

Environment variables:

* `TAU_MODELS_CATALOG_URL` — override the catalog URL.
* `TAU_MODELS_CATALOG_TTL` — override the 24h default TTL.

### `pkg/ai/provider`

`ResolveChatProvider` maps an npm package name to the matching ai-sdk Go
provider constructor. Supported packages include:

| npm package | ai-sdk Go package |
|-------------|-------------------|
| `@ai-sdk/openai` | `pkg/provider/openai` |
| `@ai-sdk/anthropic` | `pkg/provider/anthropic` |
| `@ai-sdk/azure` | `pkg/provider/azure` |
| `@ai-sdk/cohere` | `pkg/provider/cohere` |
| `@ai-sdk/deepseek` | `pkg/provider/deepseek` |
| `@ai-sdk/gemini` | `pkg/provider/gemini` |
| `@ai-sdk/groq` | `pkg/provider/groq` |
| `@ai-sdk/mistral` | `pkg/provider/mistral` |
| `@ai-sdk/ollama` | `pkg/provider/ollama` |
| `@ai-sdk/perplexity` | `pkg/provider/perplexity` |
| `@ai-sdk/xai` | `pkg/provider/xai` |

If a provider has no npm mapping, or the SDK provider cannot be constructed,
tau falls back to its built-in OpenAI-compatible streamer.

### `pkg/ai/streamer`

`Streamer` adapts an ai-sdk `chat.Provider` into tau's `agent.Streamer`
interface (`StreamChatCompletionFull`). The coordinator turn loop, event
emission, tool execution, and session persistence remain in tau.

## Configuration

### Recommended minimal `.tau.yaml`

```yaml
default_provider: deepseek
default_model: deepseek-v4-flash

providers:
  - name: deepseek
    base_url: https://api.deepseek.com
    auth:
      type: api_key
      api_key_env: DEEPSEEK_API_KEY
```

The `type`, `api`, `models.*` context/output/reasoning/cost/compat fields
are no longer required because models.dev and the ai-sdk provider supply them.
You only need `base_url` and the auth details if they differ from the
catalog defaults.

### Overriding catalog data

Create `~/.config/tau/api.overrides.json`:

```json
{
  "providers": {
    "deepseek": {
      "models": {
        "deepseek-v4-flash": {
          "output": 16384
        }
      }
    }
  }
}
```

The overrides file uses the same schema as `models.dev/api.json`. Override
values win over catalog values; new providers or models in overrides are
added to the catalog view.

### Refreshing the catalog

```bash
tau refresh
```

This fetches a fresh `api.json`, writes it to `~/.config/tau/models.json`,
and lists the models for the configured provider.

## Provider-specific notes

### DeepSeek

The ai-sdk `deepseek` provider handles `reasoning_content` replay and the
`max_tokens` field automatically. Tau still controls whether reasoning is
shown via `ui.show_reasoning` in config.

### Azure

Because the ai-sdk Azure provider needs an `Endpoint` rather than a `BaseURL`,
tau passes `base_url` from config as the endpoint. Make sure your config
`base_url` is the full Azure resource endpoint.

### Ollama

Ollama has no required API key. Only `base_url` is needed.

## Migration from pre-ai-sdk tau

Old config files that spell out every model field still load, but most model
metadata is now ignored unless the catalog is unavailable. To migrate:

1. Remove `type` and `api` from provider entries.
2. Remove model metadata (`context_window`, `default_max_tokens`,
   `max_tokens`, `input`, `reasoning`, `thinking`, `compat`, `cost`) unless
   you need to override a specific value.
3. If you do need an override, prefer `~/.config/tau/api.overrides.json` over
   inline config so it follows the same schema as the upstream catalog.

## Adding a new provider

If models.dev already lists the provider, no code change is usually needed.
If the provider is not in models.dev, or you want to use a local/enterprise
endpoint, add an entry to `~/.config/tau/api.overrides.json` with at least
`id`, `npm`, `api`, and `env`:

```json
{
  "providers": {
    "acme": {
      "id": "acme",
      "npm": "@ai-sdk/openai",
      "api": "https://llm.acme.example.com/v1",
      "env": ["ACME_API_KEY"],
      "models": {
        "acme-70b": {
          "id": "acme-70b",
          "context": 128000,
          "output": 4096,
          "tool_call": true
        }
      }
    }
  }
}
```

Then reference it in `.tau.yaml`:

```yaml
default_provider: acme
providers:
  - name: acme
    base_url: https://llm.acme.example.com/v1
    auth:
      type: api_key
      api_key_env: ACME_API_KEY
```
