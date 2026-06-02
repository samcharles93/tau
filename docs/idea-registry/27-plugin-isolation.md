# 59. Plugin Isolation Guarantees

## Status: Design constraint

### Motivation

Users can install any number of plugins. They must be guaranteed that plugins won't interfere with each other unless explicitly designed to do so. This isn't just about process crashes — it's about namespace collision, event ordering, resource contention, and pipeline composability.

go-plugin provides process isolation for free (separate binaries, separate OS processes). But we need additional guarantees at the host level.

### Isolation Dimensions

#### 1. Process Isolation (already provided by go-plugin)

- Each plugin is a separate OS process
- One plugin crashing does not affect others or the host
- Memory, file descriptors, network sockets are per-process
- Plugin A cannot access Plugin B's memory
- Host can detect crashed plugins via health check and restart them

#### 2. Namespace Isolation — Commands and Tools

**Problem**: Two plugins register `/export` command. Which one runs?

**Rule**: Command names and tool names are **plugin-scoped**. The host prepends the plugin name as a namespace:

```shell
Plugin "maas-auth" registers command "login"     → /maas-auth:login
Plugin "slack-bot" registers command "login"     → /slack-bot:login

No collision. Both can coexist.
```

For built-in commands that plugins shadow (e.g., a plugin wants to override `/export`), the user configures it explicitly:

```yaml
plugins:
  shadow:
    export: my-exporter  # route /export to plugin "my-exporter"
```

**Rule**: A plugin can only deregister its own commands/tools. Plugin A cannot remove Plugin B's commands.

#### 3. Pipeline Ordering — Explicit, Configurable

**Problem**: User installs PII Redactor and Model Router. Both are pipeline plugins. Which runs first? Does Redactor see the original prompt or the routed one?

**Rule**: Pipeline plugins receive events in a user-configured order. The order is explicit, not implicit:

```yaml
plugins:
  pipeline:
    request:
      - pii-redactor    # runs first — sees original user prompt
      - model-router    # runs second — sees redacted prompt, chooses model
      - audit-logger    # runs third — logs the final request
    response:
      - pii-redactor    # runs first — restores PII in the response
      - translator      # runs second — translates to user's language
      - audit-logger    # runs third — logs final response
```

**Rule**: Each pipeline plugin sees the output of the previous plugin in the chain. The first plugin sees the raw host output. No plugin can skip or remove another plugin from the chain — that's host configuration, not plugin behavior.

#### 4. Event Dispatch — Parallel by Default

**Problem**: A slow audit plugin should not delay the streaming plugin.

**Rule**: Event dispatch (lifecycle events, tool call notifications) fans out to all plugins **in parallel**. Each plugin's handler runs in its own goroutine. A slow plugin blocks only itself.

**Exception**: Pipeline processing is sequential (see #3 above) because middleware order matters.

**Timeout**: Each plugin gets a configurable timeout for event handling. If a plugin exceeds the timeout, the host logs a warning and moves on. The plugin is not killed — it may just be slow. Configurable via:

```yaml
plugins:
  timeout:
    event_dispatch: 5s
    pipeline_step: 30s
    command_execution: 60s
```

#### 5. Observation Isolation — Plugins Don't See Each Other

**Problem**: An audit plugin should see what the user sees, not what other plugins see.

**Rule**: Audit plugins receive events **after** pipeline processing. They see the final request (going to the LLM) and the final response (shown to the user), not intermediate pipeline states. This also means audit plugins cannot observe each other's processing.

**Rule**: No plugin can subscribe to another plugin's internal events. Plugin A cannot observe Plugin B's DispatchEvent calls.

**Rule**: If a plugin needs to communicate with another plugin, it must be explicit — they share a pub/sub topic that both subscribe to. This is opt-in, not default.

#### 6. Resource Limits

**Problem**: A plugin allocates 32GB of memory or opens 10,000 file descriptors.

**Rule**: Resource limits are configurable per-plugin or globally. Implemented via OS-level cgroups/rlimits where available:

```yaml
plugins:
  limits:
    max_memory_mb: 512
    max_file_descriptors: 256
    max_cpu_seconds_per_request: 30
```

On platforms without cgroups (macOS, Windows), best-effort with `runtime.SetMemoryLimit` and `os/rlimit`.

#### 7. Plugin Identity — Immutable After Load

**Problem**: A plugin changes its name or commands after reload.

**Rule**: Plugin identity (name, capabilities, command names) is established at load time. If a reload changes the identity, the host treats it as a new plugin — old commands are deregistered, new ones registered. Users are notified via a diagnostic.

This prevents a plugin from silently replacing another plugin's commands by changing its own name.

### Isolation Matrix

| Concern | Default | Override |
| ------- | ------- | -------- |
| Process crashes | Plugin dies, others unaffected | Health check + auto-restart |
| Command name collision | Plugin-scoped namespacing | Explicit shadow config |
| Tool name collision | Plugin-scoped namespacing | Explicit shadow config |
| Pipeline ordering | Declared by user config | Required — no implicit ordering |
| Event dispatch ordering | Parallel (no ordering guarantee) | Sequential via pipeline config |
| Slow plugin blocks others | No — timeout + parallel dispatch | Timeout per plugin |
| Plugin A observes Plugin B | No — audit sees final output only | Explicit pub/sub topic |
| Resource exhaustion | Per-plugin limits | Configurable |
| Identity change on reload | Treated as new plugin | Diagnostic notification |

### What This Means for the Extension Surface

These isolation guarantees are enforced by the **host** (the plugin manager), not by individual plugins. The proto interface doesn't need to change — isolation is a host-side concern. The manager:

1. Prefixes command/tool names with plugin namespace before registering
2. Routes pipeline events through the configured chain in order
3. Dispatches events to plugins via goroutines with timeouts
4. Wraps plugin processes with resource limits (where OS supports it)
5. Validates plugin identity on reload and handles changes gracefully

**The key principle**: plugins are guests in the host's house. The host sets the rules. Plugins can't negotiate around them.
