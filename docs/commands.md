# Command Registry

The command registry (`internal/registry/`) manages built-in slash commands, custom user commands, and skill commands. It publishes `CommandsChangedEvent` on the event bus so the TUI and Web UI can keep their autocomplete lists in sync.

## Architecture

```
internal/registry/
├── registry.go  — Registry struct, Command type, discovery, bus integration
└── sources.go   — Built-in command definitions

internal/chat/commands/
└── commands.go  — Custom commands loaded from markdown files
```

## Registry

```go
type Registry struct {
    // ...
}

func New(bus *eventbus.Bus) *Registry
func (r *Registry) Discover() error
func (r *Registry) Commands() []Command
func (r *Registry) All() []Command
func (r *Registry) Register(cmd Command) error
func (r *Registry) Unregister(name string)
func (r *Registry) Publish()
func (r *Registry) Close()
```

### Command Type

```go
type Command struct {
    Name        string   // e.g., "/model", "/skill:my-skill"
    Label       string   // human-readable label
    Description string
    AcceptsArgs bool
    Source      string   // "builtin", "user", "project", "skill", "extension"
}
```

### Lifecycle

1. **`New(bus)`** — Creates the registry with a bus client named `"registry"`.
2. **`Discover()`** — Discovers all command sources: built-in, custom, skills, extensions.
3. **`Publish()`** — Publishes `CommandsChangedEvent` on the bus so clients update completions.
4. **`Register(cmd)`** — Manually register a command (used by plugins).
5. **`Unregister(name)`** — Remove a command (used when plugins unload).

## Built-in Commands

Defined in `internal/registry/sources.go`. These are the core slash commands:

| Command | Description |
| ------- | ----------- |
| `/model` | Switch the active model |
| `/system` | View or edit the system prompt |
| `/temperature` | Set sampling temperature |
| `/max-tokens` | Set max completion tokens |
| `/reset` | Reset the session |
| `/reasoning` | Toggle reasoning visibility or set effort |
| `/refresh` | Refresh the models.dev catalog |
| `/sessions` | List saved sessions |
| `/export` | Export session as JSONL |
| `/help` | Show command help |
| `/debug` | Toggle debug mode |
| `/login` | Start OAuth login for a provider |
| `/quit` | Exit tau |

## Custom Commands

Custom commands are loaded from markdown files in two locations:

- **User commands**: `~/.config/tau/commands/` — user-global custom commands.
- **Project commands**: `.tau/commands/` — project-local custom commands.

### Command File Format

Custom commands are defined in `.md` files with YAML frontmatter:

```markdown
---
name: my-command
description: Does something useful
arguments:
  - name: target
    description: What to operate on
    required: true
---

# My Command

Command body with instructions for the agent...
```

Commands are loaded by `LoadCustomCommands()` in `internal/chat/commands/commands.go` and registered in the registry with `"user:"` or `"project:"` prefixes.

### Naming Convention

- User commands: `"user:path/to/command"` (colon-separated path segments).
- Project commands: `"project:path/to/command"`.
- Skill commands: `"skill:<skill-name>"`.

## Skill Commands

Skills that declare `command` frontmatter are automatically registered as commands with the `"skill:"` prefix. See [Skills](skills.md) for details.

## Extension Commands

Plugin slash commands are registered by the plugin manager and prefixed with `"plugin:<name>:"`. See [Plugin SDK](plugins.md).

## Events

### CommandsChangedEvent

```go
type CommandsChangedEvent struct {
    Commands []CommandRef
}
```

Published on the event bus whenever the command set changes (plugins loaded/unloaded, commands discovered/refreshed). The TUI and Web UI subscribe to this event to update their autocomplete lists.

## App Integration

In `internal/app/run.go`, the registry is created and wired:

```go
commandRegistry := commandreg.New(bus)
commandRegistry.Discover()

// Build initial command refs for the TUI and Web UI
initialCommands := commandRefsFromRegistry(commandRegistry.All())
```

The registry is passed to the coordinator, which uses it for command routing (e.g., `/model` → `handleUpdate()`).
