# Skills System

Tau discovers and activates "skills" — markdown files with YAML frontmatter that provide instructions, context, and tool definitions to the agent. Skills are project-local or user-global and are injected into the system prompt.

## Architecture

```
internal/skills/
├── skills.go    — Skill type, discovery, parsing, rendering
├── manager.go   — Runtime skill lifecycle management
└── tracker.go   — Skill activation tracking
```

## Skill Format

A skill file is a markdown document with YAML frontmatter:

```markdown
---
name: my-skill
description: A concise description of what this skill does
source: project
scope: project
enabled: true
---

# My Skill

Instructions and context for the agent...
```

### Frontmatter Fields

| Field | Type | Description |
| ----- | ---- | ----------- |
| `name` | string | Unique skill identifier |
| `description` | string | Human-readable description |
| `source` | string | Source location (e.g., "project", "user") |
| `scope` | string | "project" or "user" |
| `enabled` | bool | Whether the skill is active (default: true) |
| `user_invocable` | bool | Whether the user can invoke this skill directly |
| `priority` | int | Ordering priority (lower = earlier in prompt) |

The frontmatter is separated from the body by `---` delimiters and parsed as YAML.

## Skill Type

```go
type Skill struct {
    Name         string
    Description  string
    Source       string
    Scope        Scope       // "project" or "user"
    Path         string      // filesystem path to the SKILL.md file
    Body         string      // markdown body (after frontmatter)
    Enabled      bool
    UserInvocable bool
    Priority     int
    Diagnostics  []Diagnostic
}
```

## Discovery

`skills.Discover()` walks configured source directories:

- **User sources**: `~/.config/tau/skills/` — user-global skills.
- **Project sources**: `.tau/skills/` — project-local skills.
- **Default sources**: Built-in skill directories.

Each directory is walked recursively. Files named `SKILL.md` or `*.skill.md` are parsed as skill files. Directories containing a `SKILL.md` are treated as single skills (the directory name becomes the skill name).

Discovery is triggered:
- On startup by `skills.Manager`.
- On `/refresh` (reloads all skills).
- When the `skills.Manager` publishes `skills.Event` on the event bus.

## Filtering

Skills can be filtered:

- **`FilterDisabled()`** — Excludes skills with `enabled: false`.
- **`FilterUserInvocable()`** — Excludes skills that cannot be invoked by the user.
- **`HasErrors()`** — Checks for parsing errors in any skill's `Diagnostics`.

## Prompt Rendering

Skills are rendered into the system prompt in two formats:

### ToPromptIndex

```go
func (s Skill) ToPromptIndex() string
```

Returns a compact index line: `- **`name`**: description`

This is used in the system prompt to list available skills without bloating the context window.

### ToPromptXML

```go
func (s Skill) ToPromptXML() string
```

Returns the full skill body wrapped in XML tags:

```xml
<skill name="my-skill">
Body content...
</skill>
```

This is used when a skill is activated and its full instructions need to be injected into the prompt.

### Skill Activation

When the agent determines a skill is relevant (based on the index), the skill's full body is injected into the prompt for that turn. The `tracker.go` module records which skills have been activated.

## Manager

The `skills.Manager` provides runtime lifecycle:

```go
type Manager struct {
    // ...
}

func NewManager(bus *eventbus.Bus, sources []Source) *Manager
func (m *Manager) Refresh() error
func (m *Manager) Skills() []Skill
func (m *Manager) ActiveSkills() []Skill
func (m *Manager) Close() error
```

- **`Refresh()`** — Re-discover skills from all sources and publish `skills.Event` on the bus.
- **`Skills()`** — Return all discovered skills.
- **`ActiveSkills()`** — Return skills that have been activated in the current session.

The manager is a bus client named `"skills"` and publishes `skills.Event` when the catalog is refreshed. The TUI and Web UI can subscribe to this event to update their skill displays.

## Tracker

`internal/skills/tracker.go` records skill activation:

- When the agent activates a skill (injects its full body into the prompt), the tracker records the activation.
- Activation records include the skill name, activation time, and turn number.
- The tracker supports querying "what was activated this session" and "has this skill been used".

## Event

```go
type Event struct {
    Skills     []Skill
    Diagnostics []Diagnostic
    Timestamp  time.Time
}
```

Published on the event bus by the manager after each `Refresh()`.

## Configuration

Skill sources can be configured:

- **User skills directory**: `~/.config/tau/skills/` (default).
- **Project skills directory**: `.tau/skills/` (project-local).
- Custom sources can be added programmatically.

Skills can be disabled individually via `enabled: false` in frontmatter or filtered at the manager level.

## Integration with Agent

The coordinator integrates skills into the prompt:

1. On session start, the full skill index is appended to the system prompt (via `ToPromptIndex()`).
2. During a turn, if the agent activates a skill, the full body is injected (via `ToPromptXML()`).
3. The `DiscoverContextFiles()` function in `internal/agent/prompt.go` also discovers project-level context files like `AGENTS.md`.
