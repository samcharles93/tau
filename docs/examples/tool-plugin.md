# Example: Building a Custom Tool Plugin

The [hello plugin](https://github.com/samcharles93/tau/blob/main/examples/plugins/hello/main.go)
covered in [Quick Start](/plugins#quick-start) shows the minimum shape of a
tool. This page builds a more realistic one — a `weather` plugin that wraps a
third-party HTTP API, reads an API key from tau's config, and caches results
via `HostService.SetConfig` — the pattern you'd actually use for "call some
external API" plugins.

## What it adds over the hello plugin

- Reading a secret (API key) from `config.yaml` via `host.GetConfig`, instead
  of hardcoding it.
- Making a real outbound HTTP call from `ExecuteTool`.
- Persisting a small cache across calls with `host.SetConfig`/`GetConfig`.
- A tighter `InputSchema` following the best-practice checklist from the
  [Plugin SDK](/plugins#tool-inputschema-best-practices) guide.

## Configuration

```yaml
# ~/.config/tau/config.yaml
plugins:
  weather:
    api_key_env: WEATHER_API_KEY
    cache_ttl_seconds: 300
```

```bash
export WEATHER_API_KEY=your-key-here
```

Following the convention used elsewhere in tau's own config (providers'
`auth.api_key_env`), the plugin stores the *name* of an environment variable
in config rather than the key itself — the key never touches disk.

## Reading config in SetHost / on demand

```go
type WeatherPlugin struct {
    host   pluginapi.Host
    logger *slog.Logger
}

func (p *WeatherPlugin) SetHost(h pluginapi.Host) { p.host = h }

func (p *WeatherPlugin) apiKey(ctx context.Context) (string, error) {
    if p.host == nil {
        return "", fmt.Errorf("weather plugin: host unavailable")
    }
    envVar, found, err := p.host.GetConfig(ctx, "api_key_env")
    if err != nil {
        return "", err
    }
    if !found || envVar == "" {
        return "", fmt.Errorf("weather plugin: plugins.weather.api_key_env not set in config.yaml")
    }
    key := os.Getenv(envVar)
    if key == "" {
        return "", fmt.Errorf("weather plugin: env var %s is not set", envVar)
    }
    return key, nil
}
```

`host.GetConfig(ctx, "api_key_env")` reads a single key out of the plugin's
`plugins.weather` block. Passing `""` instead would return the whole block as
a JSON string — useful when a plugin has several related settings to parse at
once (see [`cmdReconnect`'s config
read](https://github.com/samcharles93/tau/blob/main/plugins/mcp/main.go) in
the MCP plugin for that pattern).

## Declaring a tight tool schema

```go
func (p *WeatherPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    schema, _ := json.Marshal(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{
                "type":        "string",
                "description": "City name, e.g. 'Melbourne' or 'London,UK'",
            },
            "units": map[string]any{
                "type":        "string",
                "description": "Temperature units",
                "enum":        []string{"metric", "imperial"},
                "default":     "metric",
            },
        },
        "required": []string{"city"},
    })

    return []*pluginapi.ToolDefinition{{
        Name:        "weather_lookup",
        Description: "Get the current weather for a city. Returns JSON with keys: city, tempC or tempF, condition, humidity.",
        InputSchema: string(schema),
    }}, nil
}
```

Two things worth calling out against the [best-practices
checklist](/plugins#tool-inputschema-best-practices): `units` uses `"enum"`
to stop the model from inventing invalid values, and the description states
the exact return shape so the model doesn't have to guess how to parse the
result.

## Calling the API and caching the result

```go
func (p *WeatherPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    if toolName != "weather_lookup" {
        return "", true, fmt.Errorf("weather plugin: unknown tool %q", toolName)
    }

    var args struct {
        City  string `json:"city"`
        Units string `json:"units"`
    }
    if err := json.Unmarshal([]byte(arguments), &args); err != nil {
        return "", true, fmt.Errorf("weather plugin: parse arguments: %w", err)
    }
    if args.Units == "" {
        args.Units = "metric"
    }

    cacheKey := "cache." + args.City + "." + args.Units
    if cached, found, _ := p.host.GetConfig(ctx, cacheKey); found && cached != "" {
        var entry cacheEntry
        if json.Unmarshal([]byte(cached), &entry) == nil && time.Since(entry.At) < 5*time.Minute {
            return entry.Body, false, nil
        }
    }

    key, err := p.apiKey(ctx)
    if err != nil {
        return "", true, err
    }

    body, err := fetchWeather(ctx, key, args.City, args.Units)
    if err != nil {
        return "", true, fmt.Errorf("weather plugin: fetch failed: %w", err)
    }

    entry := cacheEntry{Body: body, At: time.Now()}
    if raw, err := json.Marshal(entry); err == nil {
        _ = p.host.SetConfig(ctx, cacheKey, string(raw)) // best-effort; a cache miss just refetches
    }

    return body, false, nil
}

type cacheEntry struct {
    Body string    `json:"body"`
    At   time.Time `json:"at"`
}
```

`fetchWeather` is an ordinary `net/http` call — nothing plugin-specific about
it. The interesting part is that the cache lives in
`~/.config/tau/plugin-state.json` via `host.SetConfig`, keyed by
`weather.cache.<city>.<units>` (state keys are namespaced by plugin name
automatically), so it survives `/reload` and tau restarts without the plugin
managing its own file on disk.

## Build and install

```bash
mkdir -p plugins/weather && cd plugins/weather
go mod init github.com/you/tau-plugin-weather
go get github.com/samcharles93/tau/pkg/plugin/api github.com/hashicorp/go-plugin github.com/hashicorp/go-hclog

go build -o tau-plugin-weather .
mkdir -p ~/.config/tau/plugins
cp tau-plugin-weather ~/.config/tau/plugins/
```

This is the **standalone plugin** layout (own `go.mod`, no `replace`
directive needed since it only imports the public `pkg/plugin/api` package) —
see [Standalone Plugin (External
Repo)](/plugins#standalone-plugin-external-repo) for when to prefer this over
building inside the tau repo.

## Try it

```
tau
```

```
What's the weather in Melbourne?
```

The agent calls `weather_lookup`, gets back the JSON result described in the
tool's `Description`, and reports it back in natural language. A second
identical question within 5 minutes is served from the cache — no repeat API
call, no extra latency.
