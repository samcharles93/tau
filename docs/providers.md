# Provider System

Tau's provider system (`internal/providers/`) manages the lifecycle of LLM providers - a built-in catalog of well-known
OpenAI-compatible providers, user-managed state (which providers are enabled, OAuth credentials), and resolution that
merges hand-written config, managed state, and environment variables into the effective set of providers tau uses.

## Architecture

```tree
internal/providers/
├── catalog.go    - Built-in catalog of known OpenAI-compatible providers
├── oauth.go      - OAuth device-code login and refresh handlers
├── state.go      - Writable provider/auth state file (~/.config/tau/auth.yaml)
├── resolve.go    - Resolves effective provider set from config + state + env
└── effective.go  - Merges resolved providers into tau config
```

## Built-in Catalog

`catalog.go` defines a hardcoded catalog of well-known providers that speak OpenAI-compatible APIs. Each entry includes:

- Provider ID and display name
- Default base URL
- Supported auth types (api_key, oauth)
- OAuth handler ID for device-code providers
- Static request headers required by the provider runtime
- Environment variable hints for API keys

This catalog means users don't need to specify `base_url` for well-known providers - tau fills in the defaults.

## Provider State

`state.go` manages a writable state file (`~/.config/tau/auth.yaml`) that records:

- Which providers the user has enabled
- OAuth credentials (access tokens, refresh tokens, expiry)
- Provider-specific OAuth extras, such as Copilot `base_url` and `available_model_ids`, or Codex `account_id`

The state file is managed by `/provider` commands and the provider runtime in `internal/app/`. Tau does not write OAuth
secrets to `config.yaml`.

### State Operations

```go
func LoadState() (State, error)
func (s *State) Save() error
func (s *State) IsEnabled(providerID string) bool
func (s *State) Enable(providerID string)
func (s *State) Disable(providerID string)
func (s *State) SetOAuth(providerID string, creds OAuthCredentials)
func (s *State) OAuthFor(providerID string) (OAuthCredentials, bool)
func (s *State) RemoveOAuth(providerID string)
```

## Resolution

`resolve.go` merges three sources to determine the effective providers:

1. **Hand-written config** (`config.yaml` / `.tau.yaml`) - Provider names, base URLs, API key env vars.
2. **Managed state** (`auth.yaml`) - OAuth tokens, enabled/disabled flags.
3. **Environment variables** - API keys from configured env vars.

Resolution order:

1. Hand-written config providers are used first and are never rewritten by Tau.
2. Catalog API-key providers are enabled from environment variables or `/provider <name>`.
3. OAuth providers with stored credentials are refreshed before use; failed refresh makes the provider unavailable and
   asks for re-login instead of using stale credentials.

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

| Provider         | ai-sdk Package                                              |
| ---------------- | ----------------------------------------------------------- |
| `openai`         | `pkg/provider/openai`                                       |
| `anthropic`      | `pkg/provider/anthropic`                                    |
| `deepseek`       | `pkg/provider/deepseek`                                     |
| `groq`           | `pkg/provider/groq`                                         |
| `mistral`        | `pkg/provider/mistral`                                      |
| `gemini`         | `pkg/provider/gemini`                                       |
| `ollama`         | `pkg/provider/ollama`                                       |
| `xai`            | `pkg/provider/xai`                                          |
| `perplexity`     | `pkg/provider/perplexity`                                   |
| `cohere`         | `pkg/provider/cohere`                                       |
| `azure`          | `pkg/provider/azure`                                        |
| `openrouter`     | `pkg/provider/openai` (OpenAI-compatible)                   |
| `github-copilot` | `pkg/provider/openai` (OpenAI-compatible + Copilot headers) |
| `openai-codex`   | Tau `openai-codex` class (ChatGPT backend Responses SSE)    |

If a provider has no matching ai-sdk constructor, tau falls back to its built-in OpenAI-compatible streamer.

## Model Discovery

Most hosted providers use Tau's embedded models.dev snapshot. Dynamic providers are queried live:

1. `ollama` calls the local `/models` endpoint.
2. `github-copilot` uses account-available model IDs from the Copilot token exchange.
3. `openai-codex` calls the ChatGPT backend Codex model endpoint at refresh/startup time, so current slugs such as
   `gpt-5.5` are not hard-coded in Tau.

The `/refresh` command rebuilds the runtime from current provider state and re-runs model discovery.

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
          "input": 2.5,
          "output_cost": 10.0,
          "tool_call": true,
          "reasoning": false
        }
      }
    }
  }
}
```

## Dynamic Provider Switching

The provider runtime (`internal/app/provider_runtime.go`) wraps multiple providers and allows cross-provider model
switching within a single session:

```go
type ProviderRuntime struct {
    rt        *Runtime       // loaded providers
    providers []ProviderConfig
    insecure  bool
}
```

When the user switches models (via `/model` or the Web UI), the dynamic streamer picks the correct ai-sdk provider per
turn from the session state's `Provider` and `Model.ID` fields. This means a single session can use OpenAI, then switch
to Anthropic, then to DeepSeek - all without restarting.

## OAuth Login

The `/provider login <name>` command starts an OAuth device-code flow:

1. Requests a device code from the provider.
2. Attempts to open the verification URL in the default browser.
3. Attempts to copy the user code to the clipboard.
4. Prints a spaced URL/code fallback in the TUI, then polls until browser authorization completes or the context is
   cancelled.
5. Exchanges/stores tokens in `~/.config/tau/auth.yaml` with mode `0600`.
6. Refreshes the model list and enables the provider.

Supported OAuth providers:

- `/provider login github-copilot [enterprise-domain]`
- `/provider login openai-codex`

`/provider logout <name>` removes stored OAuth credentials and disables the provider.

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

If the provider speaks an OpenAI-compatible API, tau's fallback streamer handles it. For non-OpenAI APIs, a new ai-sdk
provider may be needed.
