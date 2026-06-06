# 34. Replace provider `/v1/models` with models.dev as the canonical catalog

## Status

Proposed replacement for `provider.DiscoverModels` that drops the runtime
HTTP call to each provider's `/v1/models` endpoint and makes
[models.dev](https://models.dev) the single source of truth for model
metadata. Adds a `~/.config/tau/api.overrides.json` file for enterprise
and customisation use cases.

## Summary

Today `provider.DiscoverModels` does this for every chat start:

```
GET {base_url}/v1/models  →  parse { data: [{id: "..."}] }
```

This is fragile:

- **Anthropic, Google Vertex, and many others do not expose `/v1/models`.**
  They return 404 or 405 and we silently fall back to the empty list, then
  to the manually-configured `provider.Models` list — which is a footgun
  the user has to remember to maintain.
- **No cost, no capabilities, no context limits.** Just IDs. The agent has
  to guess what it can and can't do, and the TUI can't show live cost.
- **No provider metadata.** No `env:` var name, no `api:` URL, no SDK
  hint (`@ai-sdk/anthropic` vs `@ai-sdk/openai-compatible`).

models.dev is the open, community-maintained database of AI models. It
already has every provider we care about, with the metadata we lack. The
shape is richer than what we extract from `/v1/models`:

```json
{
  "anthropic": {
    "id": "anthropic",
    "env": ["ANTHROPIC_API_KEY"],
    "npm": "@ai-sdk/anthropic",
    "api": "https://api.anthropic.com/v1",
    "models": {
      "claude-sonnet-4-5": {
        "id": "claude-sonnet-4-5",
        "attachment": true,
        "reasoning": true,
        "tool_call": true,
        "temperature": true,
        "context": 200000,
        "output": 64000,
        "cost": { "input": 3, "output": 15 }
      }
    }
  }
}
```

The plan is to make this the only catalog, cached locally, with a
`api.overrides.json` escape hatch for enterprise.

## Decisions

### 1. Drop the runtime `/v1/models` call

`internal/provider/models.go`'s `DiscoverModels` becomes a pure data
function — no HTTP, no bearer token, no client. The function signature
shrinks to:

```go
// Before
func DiscoverModels(ctx context.Context, provider config.ProviderConfig,
    bearerToken string, insecure bool) ([]Model, error)

// After
func DiscoverModels(catalog Catalog, p config.ProviderConfig) ([]Model, error)
```

Where `Catalog` is a value type populated from disk at startup. The
bearer token and `insecure` flags disappear from the discovery call
entirely (the streaming layer keeps them).

**Migration**: mark the old HTTP path as legacy for one release behind a
config flag (`provider.use_legacy_models_endpoint: true`), then remove.

### 2. Add `internal/provider/modelsdev.go`

A single file that owns all models.dev interaction:

```go
package provider

type Catalog struct {
    providers map[string]modelsdev.Provider // from api.json
    overrides modelsdev.OverrideFile         // from api.overrides.json
    fetchedAt time.Time
    source    string                         // "cache", "network", "merged"
}

type Provider struct {
    ID, NPM, API string
    Env          []string
    Models       map[string]Model
}

type Model struct {
    ID, Name                            string
    Attachment, Reasoning, ToolCall, Temp bool
    Context, Output                      int
    Cost                                 Cost
}

type Cost struct {
    Input, Output         float64
    CacheRead, CacheWrite float64
}

type OverrideFile struct {
    Providers map[string]Provider `toml:"providers"`
}
```

Methods:

```go
// Load attempts cache first; falls back to network if missing or stale.
func (c *Catalog) Load(ctx context.Context, cachePath, overridePath string) error

// Fetch forces a network refresh and writes to disk.
func (c *Catalog) Fetch(ctx context.Context, cachePath string) error

// Resolve merges models.dev with overrides for one provider. Overrides win.
func (c *Catalog) Resolve(providerName string) (Provider, error)

// ModelsFor is the public API for the rest of tau.
func (c *Catalog) ModelsFor(p config.ProviderConfig) ([]Model, error)

// CostOf is the public API for cost tracking.
func (c *Catalog) CostOf(modelID string) (Cost, bool)
```

The merge order (lowest → highest priority):

1. models.dev `api.json` — base
2. `~/.config/tau/api.overrides.json` — user overrides win

No third layer. If a provider or model exists only in overrides, it's
added. If it exists in both, overrides win. The result is one normalised
view per provider.

### 3. Cache layout

`~/.config/tau/` already holds `config.yaml` and the session DB. Add
two siblings:

```
~/.config/tau/
  config.yaml
  api.overrides.json   # optional, user-edited
  models.json          # cached models.dev api.json, auto-managed
```

`models.json` format is a verbatim copy of models.dev's `api.json` —
no transformation. This way:

- The user can inspect `~/.config/tau/models.json` and recognise it.
- A future `tau models info <name>` command can pretty-print from it.
- We never have to maintain a conversion between models.dev's wire
  format and an internal one.

The cache write is atomic: write to `models.json.tmp`, then `os.Rename`.

### 4. Refresh strategy

Three triggers:

1. **Cold start, no cache** — fetch synchronously. Without models.dev,
   the user has *no* models to pick from, so the network call is
   blocking. Show a one-line progress in the TUI: `Loading model
   catalog…`. Hard cap: 5 seconds, then fail loud (no silent
   empty catalog).
2. **Warm start, cache > 24h old** — fire a background goroutine
   that refreshes the cache. Don't block startup. The current session
   uses the stale catalog; next session gets fresh.
3. **Explicit** — `tau refresh` CLI subcommand and the in-TUI
   `/reload` slash command both call `Catalog.Fetch` synchronously and
   report `"models updated, +N new, -M removed"`.

The 24h TTL is `internal/provider/modelsdev.go`'s default. `config.yaml`
can override it under `models_catalog.ttl`:

```yaml
models_catalog:
  ttl: 12h
  url: https://models.dev/api.json  # overridable for mirrors
```

### 5. `api.overrides.json` schema

Matches models.dev's provider/model shape exactly so the merge is
trivial. Users override the fields they care about:

```json
{
  "$schema": "https://models.dev/api.json",
  "providers": {
    "acme-corp": {
      "id": "acme-corp",
      "env": ["ACME_LLM_API_KEY"],
      "api": "https://llm.acme.example.com/v1",
      "npm": "@ai-sdk/openai-compatible",
      "models": {
        "acme-custom-70b": {
          "id": "acme-custom-70b",
          "name": "ACME Custom 70B",
          "attachment": false,
          "reasoning": true,
          "tool_call": true,
          "context": 32000,
          "output": 4096,
          "cost": { "input": 0, "output": 0 }
        }
      }
    },
    "anthropic": {
      "models": {
        "claude-sonnet-4-5": {
          "api": "https://acme-proxy.example.com/anthropic/v1"
        }
      }
    }
  }
}
```

Both new-provider (acme-corp) and override-existing-field
(anthropic.api override) work. The file is optional. If it doesn't
exist, we use the cached models.dev data unchanged.

### 6. Cost tracking falls out for free

`Catalog.CostOf(modelID)` answers "what does it cost to call this
model". That unlocks three features that are already on the wishlist
but blocked on data:

- **`tau stats <session-id>`** — sum the per-token costs from the
  session's token-usage records. Currently tau records token counts
  but has no way to attach a price.
- **TUI live cost line** — during a chat, show `$0.042 so far, $0.058
  budgeted (4.2k in / 1.1k out, gpt-5.4 at $2.5/$15)`. The streaming
  layer already reports `ChatResponseDeltaEvent` with token counts; we
  just multiply by the cached cost.
- **Pre-flight guard** — `tau --budget 1.00` rejects running a session
  whose estimated cost exceeds the budget. Estimate is `(context_size *
  input_cost) + (max_output * output_cost)`.

The cost-tracking work is a follow-up — this plan is just about getting
the data into tau. Cost tracking is Idea 35 (next in the queue).

### 7. Auto provider config falls out for free

models.dev's `env` field already says which environment variable holds
the API key for each provider. Today tau makes the user spell this out
in `config.yaml`:

```yaml
providers:
  - name: anthropic
    api_key_env: ANTHROPIC_API_KEY
    base_url: https://api.anthropic.com/v1
```

After this plan, the user only needs the name:

```yaml
providers:
  - name: anthropic
  - name: openai
  - name: google
```

tau reads `models.dev["anthropic"].env[0]`, `.api`, and `.npm` to
configure the streaming layer. The `api_key_env` field in
`config.yaml` still works for users who want a non-default env var
name (e.g. `ACME_ANTHROPIC_KEY`).

This is the auto-config part the user asked for. It removes the biggest
piece of config boilerplate.

## File layout

```
internal/provider/
  models.go              # Model type (existing, extends with Cost field)
  modelsdev.go           # NEW: Catalog, Provider, Load, Fetch, Resolve
  modelsdev_test.go      # NEW: fixture-driven table tests
  discover.go            # NEW: thin wrapper that Catalog.ModelsFor calls
  models_test.go         # MODIFIED: existing tests keep working

internal/config/
  config.go              # MODIFIED: deprecate provider.Models field
  config_test.go         # MODIFIED: new defaults

internal/app/
  chat.go                # MODIFIED: load Catalog at startup
  singleshot.go          # MODIFIED: same
  refresh.go             # NEW: `tau refresh` subcommand

internal/cli/
  root.go                # MODIFIED: wire up `tau refresh`

cmd/tau/
  main.go                # no changes

docs/
  api-overrides.md       # NEW: user-facing schema docs
  models-catalog.md      # NEW: how the catalog works, refresh behaviour
```

## Migration path

Three phases, each shippable independently:

**Phase 1 — Add `modelsdev.go`, read-only**
- `Catalog.Load` reads cache, falls back to network on miss
- `provider.DiscoverModels` accepts an injected `*Catalog`
- The old HTTP call still runs as a fallback path when the catalog is
  empty (so existing behaviour is preserved)
- `api.overrides.json` is read but not yet exposed in the TUI

**Phase 2 — Make models.dev the default**
- Drop the HTTP call from the default code path
- Move `/v1/models` to a config flag (`provider.use_legacy_models_endpoint`)
- TUI shows a one-line notice when legacy mode is on
- Refresh UI: a new `tau refresh` subcommand + `/reload` slash

**Phase 3 — Hard cut**
- Remove the legacy flag and the HTTP code path
- Remove the `provider.Models` config field (one-cycle deprecation)
- `models.json` is the only catalog

Phases 1 and 2 can ship in the same release. Phase 3 ships after one
release cycle of users seeing the new behaviour.

## Test strategy

- **Pure unit tests** for `Catalog.Resolve`: given a fixture `api.json`
  and a fixture `api.overrides.json`, the merge is deterministic. Use
  table tests with 8-10 cases covering: new provider, override existing
  model, override env, partial model fields, etc.
- **HTTP fetcher test** against a `httptest.Server` that serves a
  fixture `api.json`. Verify atomic write (`models.json.tmp` →
  `os.Rename`), mtime update, network error handling.
- **Cache test**: write a fake `models.json`, verify `Load` reads it
  without network. Verify mtime > TTL triggers a background refresh
  (use a short TTL like 1ms in tests).
- **End-to-end**: start a `chat` session with an empty cache and the
  network blocked. Verify the session starts with a clear error and
  no silent empty catalog.
- **Backwards-compat**: existing `TestDiscoverModels_*` tests
  continue to pass when the fixture `models.json` is pre-populated.

No new types of failure mode for users in the happy path. The 24h TTL
means users in air-gapped environments can ship a pre-built
`models.json` and never touch the network.

## Out of scope for this pass

- **Cost tracking UI** — Idea 35 (depends on this).
- **Auto provider SDK selection** (using models.dev's `npm` field to pick
  `@ai-sdk/anthropic` vs `@ai-sdk/openai-compatible`) — separate work.
- **Model popularity rankings** — models.dev doesn't expose this; we'd
  need our own telemetry.
- **Embedding/multimodal routing** — `attachment` and `tool_call` are
  useful but a full routing decision tree is its own design.
- **Writing a public mirror of models.dev** — out of scope, the user
  can set `models_catalog.url` to a private mirror.

## Why this is safe

The /v1/models call is **not load-bearing** in any way that models.dev
doesn't cover. models.dev is more comprehensive than any single provider's
endpoint (it knows about all of Anthropic, Vertex, Bedrock, etc., which
have no `/v1/models`). The only thing we lose is real-time "is this
model currently responding" liveness, which models.dev doesn't tell us
either — so it's no worse than what we have.

The cache+TTL design means the worst case is "tau starts 5 seconds
slower on a cold cache" — same as the current `/v1/models` call. The
warm case is *faster* (disk read vs. HTTP round-trip).

The override file means enterprise users keep full control. A user who
needs a model that models.dev doesn't know about writes 10 lines of
JSON and it works.
