# Configuration

Tau loads configuration from two YAML files, merged in order (later wins):

1. **Global**: `~/.config/tau/config.yaml`
2. **Project-local**: `.tau.yaml` (in the current working directory)

## Config Structure

```go
type Config struct {
    DefaultProvider string                    `yaml:"default_provider"`
    DefaultModel    string                    `yaml:"default_model"`
    Providers       []ProviderConfig          `yaml:"providers"`
    UI              UIConfig                  `yaml:"ui"`
    Debug           bool                      `yaml:"debug"`
    Plugins         map[string]map[string]any `yaml:"plugins"`
}
```

## ProviderConfig

```go
type ProviderConfig struct {
    Name    string     `yaml:"name"`
    BaseURL string     `yaml:"base_url"`
    Auth    AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
    Type      string `yaml:"type"`       // "api_key", "oauth"
    APIKeyEnv string `yaml:"api_key_env"` // env var for API key
}
```

### Auth Types

| Type | Description |
| ---- | ----------- |
| `api_key` | Static API key from environment variable (`api_key_env`) |
| `oauth` | OAuth 2.0 flow (managed via `tau login`) |

## UIConfig

```go
type UIConfig struct {
    ShowReasoning bool `yaml:"show_reasoning"` // default: false
}
```

Controls terminal UI presentation:
- `show_reasoning: true` — display reasoning/chain-of-thought content.

## Plugin Config

The `plugins` section is free-form YAML passed through to plugins by name:

```yaml
plugins:
  my-plugin:
    api_key_env: MY_PLUGIN_API_KEY
    endpoint: https://api.example.com
```

Tau does not validate or interpret plugin config — each plugin parses its own section.

## Example Configurations

### Minimal (API Key)

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

### Multiple Providers

```yaml
default_provider: openai
default_model: gpt-5.5

providers:
  - name: openai
    auth:
      type: api_key
      api_key_env: OPENAI_API_KEY

  - name: anthropic
    auth:
      type: api_key
      api_key_env: ANTHROPIC_API_KEY

  - name: ollama
    base_url: http://localhost:11434/v1

ui:
  show_reasoning: true
```

### With Plugin Config

```yaml
default_provider: openrouter
default_model: openai/gpt-5.5

providers:
  - name: openrouter
    base_url: https://openrouter.ai/api/v1
    auth:
      type: api_key
      api_key_env: OPENROUTER_API_KEY

plugins:
  github:
    token_env: GITHUB_TOKEN
    poll_interval: 5m
```

## Environment Variables

| Variable | Purpose |
| -------- | ------- |
| `TAU_PROVIDER` | Default provider (overrides config) |
| `TAU_INSECURE` | Skip TLS verification |
| `TAU_VERBOSE` | Enable verbose logging |
| `TAU_MODELS_CATALOG_URL` | Override models.dev catalog URL |
| `TAU_MODELS_CATALOG_TTL` | Override catalog cache TTL |
| `TAU_SCHEDULE_INTERVAL` | Set plugin schedule tick interval |
| Provider-specific `*_API_KEY` | API key for each provider (configured in `api_key_env`) |

## CLI Flags

CLI flags override configuration:

| Flag | Config Equivalent |
| ---- | ----------------- |
| `--provider <name>` | `default_provider` |
| `--model <id>` | `default_model` |
| `--max-tokens <n>` | Session parameter |
| `--temperature <f>` | Session parameter |
| `--web` | Start web UI + open browser |
| `--port <n>` | Web UI port (0 = auto) |
| `--no-web` | Disable web UI |
| `--insecure` | `TAU_INSECURE` |
| `--verbose` | `TAU_VERBOSE` |
| `--prompt <text>` | One-shot stdin mode |
| `--resume <id>` | Resume saved session |

## Config Resolution

1. Load `~/.config/tau/config.yaml`.
2. If `./.tau.yaml` exists, merge it (project values override global).
3. Apply environment variable overrides.
4. Apply CLI flag overrides.

The merge is a shallow merge at the top level. Provider lists are merged by name — a provider in `.tau.yaml` with the same name as one in `config.yaml` replaces it entirely.

## Models.dev Catalog

Model metadata (context windows, pricing, capabilities) comes from the [models.dev](https://models.dev) catalog, cached at `~/.config/tau/models.json`. See [Providers](providers.md) for details.

### Catalog Overrides

Create `~/.config/tau/api.overrides.json` to override model metadata:

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

Refresh the catalog with `tau refresh` or `/refresh` in the TUI.

## Config Directory

Tau's config directory is `~/.config/tau/`:

```
~/.config/tau/
├── config.yaml           # Global config
├── models.json           # models.dev catalog cache
├── api.overrides.json    # Model metadata overrides (optional)
├── sessions.db           # SQLite session store
├── plugins/              # Plugin binaries
├── commands/             # User custom commands
├── skills/               # User skills
└── tau.log               # Application logs
```

## Programmatic Access

The config package (`internal/config`) provides:

```go
func Dir() string                           // ~/.config/tau
func GlobalPath() string                    // ~/.config/tau/config.yaml
func LocalPath() string                     // ./.tau.yaml
func SessionsDir() string                   // ~/.config/tau/sessions
func SessionsDBPath() string                // ~/.config/tau/sessions.db
func LoadConfig() (*Config, error)          // Load global config
func LoadConfigFrom(paths ...string) (*Config, error) // Load from custom paths
func ResolveProvider(config, name) (ProviderConfig, error)
func ProviderNames(config) []string
func (c *Config) Validate() error
```

YAML field names support both kebab-case and camelCase variants.
