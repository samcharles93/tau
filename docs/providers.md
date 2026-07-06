# Provider System

Tau's provider system (`internal/providers/`) manages the lifecycle of LLM providers — a built-in catalog of well-known OpenAI-compatible providers, user-managed state (which providers are enabled, OAuth credentials), and resolution that merges hand-written config, managed state, and environment variables into the effective set of providers tau uses.

## Architecture

```
internal/providers/
├── catalog.go    — Built-in catalog of known OpenAI-compatible providers
├── state.go      — Writable provider/auth state file (~/.config/tau/providers.json)
├── resolve.go    — Resolves effective provider set from config + state + env
└── effective.go  — Merges resolved providers into tau config
```

## Built-in Catalog

`catalog.go` defines a hardcoded catalog of well-known providers that speak OpenAI-compatible APIs. Each entry includes:

- Provider ID and display name
- Default base URL
- Supported auth types (api_key, oauth)
- OAuth endpoints (authorization URL, token URL)
- Environment variable hints for API keys

This catalog means users don't need to specify `base_url` for well-known providers — tau fills in the defaults.

## Provider State

`state.go` manages a writable state file (`~/.config/tau/providers.json`) that records:

- Which providers the user has enabled
- OAuth credentials (access tokens, refresh tokens, expiry)
- Provider metadata overrides

The state file is managed by the `tau login` command and the provider runtime in `internal/app/`.

### State Operations

```go
func LoadState() (*State, error)
func (s *State) Save() error
func (s *State) IsEnabled(providerID string) bool
func (s *State) Enable(providerID string) error
func (s *State) Disable(providerID string) error
func (s *State) SetToken(providerID, accessToken, refreshToken string, expiry time.Time) error
func (s *State) GetToken(providerID string) (*OAuthToken, error)
```

## Resolution

`resolve.go` merges three sources to determine the effective providers:

1. **Hand-written config** (`config.yaml` / `.tau.yaml`) — Provider names, base URLs, API key env vars.
2. **Managed state** (`providers.json`) — OAuth tokens, enabled/disabled flags.
3. **Environment variables** — API keys from configured env vars.

Resolution order:
1. For each provider in config, resolve its auth: API key from env var, or OAuth token from state.
2. Filter out providers that have no valid auth.
3. Merge in model metadata from the models.dev catalog.

## Effective Provider Set

`effective.go` produces the final set of usable providers:

```go
func ResolveEffectiveProviders(config *Config, state *State) ([]EffectiveProvider, error)
```

Each `EffectiveProvider` has:
- Fully resolved auth (token or API key)
- Base URL (from config or catalog default)
- Available models (from models.dev catalog or manual config)
- Model metadata (context windows, pricing, capabilities)

## AI SDK Integration

The dynamic streamer (`internal/app/streamer.go`) maps provider names to ai-sdk Go provider constructors:

| Provider | ai-sdk Package |
| -------- | -------------- |
| `openai` | `pkg/provider/openai` |
| `anthropic` | `pkg/provider/anthropic` |
| `deepseek` | `pkg/provider/deepseek` |
| `groq` | `pkg/provider/groq` |
| `mistral` | `pkg/provider/mistral` |
| `gemini` | `pkg/provider/gemini` |
| `ollama` | `pkg/provider/ollama` |
| `xai` | `pkg/provider/xai` |
| `perplexity` | `pkg/provider/perplexity` |
| `cohere` | `pkg/provider/cohere` |
| `azure` | `pkg/provider/azure` |
| `openrouter` | `pkg/provider/openai` (OpenAI-compatible) |

If a provider has no matching ai-sdk constructor, tau falls back to its built-in OpenAI-compatible streamer.

## Model Discovery

Models are discovered from the models.dev catalog:

1. On startup, `internal/app/platform.go` loads the cached catalog from `~/.config/tau/models.json`.
2. If the cache doesn't exist or is stale (>24h), it downloads a fresh copy from models.dev.
3. User overrides (`~/.config/tau/api.overrides.json`) are merged in.
4. Models are filtered to the configured provider.

The `/refresh` command forces a catalog re-download.

### Catalog Format

The catalog is JSON with a `providers` map:

```json
{
  "providers": {
    "openai": {
      "id": "openai",
      "name": "OpenAI",
      "api": "https://api.openai.com/v1",
      "env": ["OPENAI_API_KEY"],
      "models": {
        "gpt-5.5": {
          "id": "gpt-5.5",
          "name": "GPT-5.5",
          "context": 128000,
          "output": 16384,
          "input": 2.50,
          "output_cost": 10.00,
          "tool_call": true,
          "reasoning": false
        }
      }
    }
  }
}
```

## Dynamic Provider Switching

The provider runtime (`internal/app/provider_runtime.go`) wraps multiple providers and allows cross-provider model switching within a single session:

```go
type ProviderRuntime struct {
    rt        *Runtime       // loaded providers
    providers []ProviderConfig
    insecure  bool
}
```

When the user switches models (via `/model` or the Web UI), the dynamic streamer picks the correct ai-sdk provider per turn from the session state's `Provider` and `Model.ID` fields. This means a single session can use OpenAI, then switch to Anthropic, then to DeepSeek — all without restarting.

## OAuth Login

The `/provider login <name>` command starts an OAuth 2.0 flow:

1. Opens the provider's authorization URL in the default browser.
2. Starts a local HTTP server to receive the callback.
3. Exchanges the authorization code for tokens.
4. Stores tokens in `providers.json`.
5. The provider is now usable with the OAuth-authenticated session.

OAuth providers are defined in the built-in catalog and include endpoints for GitHub, Google, and other identity providers that tau integrates with.

## Adding a Custom Provider

For providers not in the built-in catalog, add configuration manually:

```yaml
providers:
  - name: my-custom-provider
    base_url: https://llm.example.com/v1
    auth:
      type: api_key
      api_key_env: MY_API_KEY
```

If the provider speaks an OpenAI-compatible API, tau's fallback streamer handles it. For non-OpenAI APIs, a new ai-sdk provider may be needed.
