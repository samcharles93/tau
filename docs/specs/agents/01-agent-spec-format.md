# Agent Spec Format

The spec file format is unchanged in shape: YAML frontmatter plus a Go text/template prompt body, in a file named `<name>.agent.md`. This page is the complete field reference after the agent-process changes, including which fields gain enforcement and which are new.

Parsing lives in `internal/agent/spec` (`Parse`, `Builtins`, `Resolve`, `DiscoverFromDisk`). Discovery roots and precedence are unchanged: embedded built-ins are reachable by bare name; `~/.agents/agents/` (user scope) and `<project>/.agents/agents/` (project scope) are reachable via `user:` and `project:` prefixes, with project winning name collisions.

## Field reference

| Field | Type | Default | Status | Semantics |
|-------|------|---------|--------|-----------|
| `name` | string | required | existing | Agent name; bare-name resolution only matches built-ins |
| `description` | string | required | existing | Shown in /help and used by the model to choose spawn targets, so write it for both audiences |
| `tools` | string list | nil (= all) | existing, semantics extended | Restricts the tool registry for this agent. For process instances this is the spec's contribution to the attenuation intersection (see 02). Omitting the `agent` tool makes the whole subtree spawn-free |
| `model` | string | unset | **newly enforced** | A tier name (`fast`, `smart`, `deep`, or any key of config `model_modes`) or a concrete model string. Tier lookup is tried first. Unset inherits the invoker's already-resolved provider/model pair; for the root process, the global defaults |
| `provider` | string | unset | **new** | Optional explicit provider, only meaningful alongside a concrete `model`. Tiers carry their own provider |
| `max-turns` | int | unset (= config `agents.default_max_turns`) | **new** | Structural cap on agentic-loop iterations per assigned task when running as a process. A safety trait of the agent kind, not a task budget (budgets are per-spawn, see 02) |
| `timeout` | duration string | unset (= config `agents.default_timeout`) | **new** | Default wall-clock limit per assigned task; a per-spawn deadline overrides it |
| `disable-model-invocation` | bool | false | **newly enforced** | When true, the `agent` tool may not target this spec. The spec remains user-invocable and CLI-startable. Mirrors the skills field semantics |
| `user-invocable` | bool | true | existing | Offered as a slash command (mode entry) |
| `mode-switcher` | bool | = user-invocable | existing | Appears in the Shift-Tab mode cycle |
| `argument-hint` | string | empty | existing | Shown next to the command in /help |
| `display-name` | string | title-cased name | existing | Input-mode indicator label |
| `color` | string (xterm-256 index) | empty | existing | Accent colour for the agent's UI affordances, including the child state block |
| `metadata` | string map | empty | existing | Free-form; not interpreted by the runtime |

No new field governs invocation style. The matrix is:

- Mode entry via slash command: `user-invocable`
- Mode entry via Shift-Tab: `mode-switcher`
- Process spawn via the `agent` tool: `disable-model-invocation` (inverted)
- Process start from the CLI (`tau`, `tau --agent <name>`): always permitted; the human at the CLI outranks the spec

## Model resolution

Config gains a `model_modes` map. Sam defines the initial tiers; users can add or retune their own (eventually from the TUI).

```yaml
# config.yaml
model_modes:
  fast:  { provider: "...", model: "..." }
  smart: { provider: "...", model: "..." }
  deep:  { provider: "...", model: "..." }

agents:
  default_max_depth: 2    # spawn-tree depth when the spec doesn't say
  depth_ceiling: 4        # hard maximum a spec may raise itself to
  default_max_turns: 30   # per assigned task, when the spec doesn't say
  default_timeout: 10m
```

Resolution order for the model an instance actually runs, first match wins:

1. Spawn-call `model` parameter (tier or concrete)
2. Spec `model` (tier or concrete, with spec `provider` for concrete strings)
3. The invoking instance's already-resolved provider/model pair
4. Config `default_provider` / `default_model`

Tier names resolve through `model_modes` at instantiation time and the resolved concrete pair is what gets snapshotted and inherited. Children are therefore deterministic even if tiers are redefined while a tree is running. A string that misses the tier map is treated as a concrete model name.

## The root spec: tau.agent.md

A new embedded built-in gives the interactive entry point its identity. Draft content, to be refined during implementation:

```markdown
---
name: tau
description: >-
  Tau's default interactive agent. General-purpose software engineering
  across the whole toolset. This is the identity of the process the user
  talks to unless another agent is named at startup.
user-invocable: false   # /tau as a slash command is meaningless
mode-switcher: false    # it IS the default mode; the cycle returns to it implicitly
# no tools restriction: full registry
# no model: resolves to global defaults (or tier once Sam maps one)
---

{{/* Body: tau's existing base system prompt template, relocated so the
     root identity is expressed as a spec like everything else. */}}
```

Notes:

- `tau` is deliberately spawnable (`disable-model-invocation` unset): a general-purpose child worker is a legitimate delegation target, matching `task.agent.md`'s original intent, and the two should be rationalised during implementation (task may become a thin restriction of tau, or be retired).
- A user or project `tau.agent.md` overrides the built-in through the existing precedence scheme, which is the sanctioned way to customise the root agent per project. This makes prefix-less resolution of the name `tau` a special case to handle carefully: today bare names only match built-ins; the root startup path should resolve `tau` through full discovery (project > user > built-in) so overrides work. This is the one place bare-name resolution differs, and it must be documented in code.

## Built-in inventory after the change

`init`, `plan`, `research`, `rubber-duck`, `compact`, `summarise`, `task`, `tau`.

Existing built-ins are untouched by this design except:

- `task.agent.md`: review against the new spawn semantics (it was authored for delegation before processes existed); likely gains `model: fast` or similar once tiers exist.
- `compact.agent.md`, `summarise.agent.md`: remain mode/one-shot templates; nothing about them changes. They demonstrate why modes coexist with processes.

## Snapshot semantics

When a process instantiates its spec (root at startup, child at spawn), the fully resolved definition is serialised into `agent_instances.spec_snapshot`: name, description, effective frontmatter after defaulting, the resolved provider/model pair, and the raw body. The running instance never re-reads the spec file. Modes, by contrast, keep today's live-resolution behaviour and run under the process's existing identity; entering a mode never creates an instance.