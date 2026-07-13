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

### Root-spec override trust

A project-level `tau.agent.md` replaces the root agent's identity — its prompt, rules, and behavior — while the root retains the full tool registry. This is a privilege escalation vector: cloning an untrusted repository and running `tau` in it should not silently grant the repository control over the root agent.

**Trust-on-first-use with content binding:**

1. When tau starts and discovers a project-level `tau.agent.md`, it computes `sha256(file_bytes)` of the spec file.
2. It checks `~/.config/tau/trust.yaml` for a trust entry matching the project path + spec hash.
3. If no matching entry exists, tau displays the resolved scope, source path, spec hash, and a summary of the override (what changes: tools list, model, prompt body summary). It prompts: `"Project /path/to/.tau/agents/tau.agent.md overrides the root agent. Trust this override? [y/N/a]"`
   - `y`: trust this specific hash. Persist in `trust.yaml`.
   - `N`: reject. Fall back to the built-in `tau` spec.
   - `a`: trust this project directory permanently (any future hash changes will still prompt).
4. If a matching entry exists but the hash has changed (the spec file was modified), tau treats it as untrusted: displays a diff summary and prompts again.
5. If no project-level `tau.agent.md` exists or the user rejects it, tau uses the built-in `tau` spec.

**Trust store** (`~/.config/tau/trust.yaml`):

```yaml
trusted_specs:
  - project_path: "/home/user/work/my-project"
    spec_hash: "sha256:abc123def456..."
    trusted_at: "2026-07-13T01:00:00Z"
    trust_mode: "hash"          # "hash" = only this hash; "path" = any hash in this project dir
```

The trust store lives in tau's config directory, NOT in the project repository. A project cannot self-approve by shipping a trust entry.

**Headless mode** (`tau --agent <name>`, `--prompt`, `--no-tui`, CI/non-interactive):

- If a project root-spec override requires approval and there is no TUI to prompt, tau fails with exit code 1 and a clear message: `"project root-spec override at /path/to/tau.agent.md requires trust approval; run interactively first or add to ~/.config/tau/trust.yaml"`.
- A `--trust-project-root-spec` CLI flag bypasses the prompt and trusts the override immediately for that invocation (useful in CI where the project is known).
- A `TAU_TRUST_PROJECT_ROOT_SPEC=1` env var provides the same bypass.
- If the override is already trusted (hash match in `trust.yaml`), headless mode proceeds without prompting.

**Display before execution:**

Every time the root spec is resolved from a non-built-in source (even when trusted), the startup log and TUI status area display:

```
Resolved root spec: project:/home/user/work/my-project/.tau/agents/tau.agent.md (sha256:abc123de, trusted 2026-07-13)
```

This makes the override visible throughout the session, not just at startup.

## Built-in inventory after the change

`init`, `plan`, `research`, `rubber-duck`, `compact`, `summarise`, `tau`.

Existing built-ins are untouched by this design except:

- `task.agent.md`: **retired (P0.4).** Before process agents existed, task was authored as a delegation target. Now that `tau` is a spawnable general-purpose child worker with the full personality, task's role is filled. The file remains in `templates/` for reference but is not loaded by `Builtins()`. A purpose-specific, tightly scoped replacement can be authored as a user or project spec.
- `compact.agent.md`, `summarise.agent.md`: remain mode/one-shot templates; nothing about them changes. They demonstrate why modes coexist with processes.

## Snapshot semantics

When a process instantiates its spec (root at startup, child at spawn), the fully resolved definition is serialised into `agent_instances.spec_snapshot`. The running instance never re-reads the spec file. Modes, by contrast, keep today's live-resolution behaviour and run under the process's existing identity; entering a mode never creates an instance.

### Snapshot schema version

Every snapshot carries a `snapshot_version` field. The current version is `1`.

```json
{
  "snapshot_version": 1,
  "name": "research",
  "description": "Answer research questions by searching the codebase, documentation, and web.",
  "scope": "builtin",
  "source_path": "",
  "source_hash": "sha256:abc123def456...",
  "resolved_provider": "anthropic",
  "resolved_model": "claude-sonnet-4-20250514",
  "model_tier": "smart",
  "effective_tools": ["read", "find", "grep", "docs"],
  "max_turns": 30,
  "timeout": "10m",
  "disable_model_invocation": false,
  "user_invocable": true,
  "mode_switcher": true,
  "body": "You are a research agent. ...",
  "timestamp": "2026-07-13T01:00:00Z"
}
```

| Field | Source | Notes |
|---|---|---|
| `snapshot_version` | N/A | Integer, starts at 1. Bumped when the shape changes incompatibly. |
| `name`, `description`, `body` | Spec file frontmatter + template body | Body is the raw template content (NOT rendered). |
| `scope` | Discovery path | `"builtin"`, `"user"`, or `"project"`. |
| `source_path` | Discovery path | Filesystem path for user/project specs; empty for built-ins. |
| `source_hash` | sha256 of the raw `.agent.md` file bytes | Identifies which version of the spec file was snapshotted. |
| `resolved_provider`, `resolved_model` | Tier resolution or explicit config | The concrete pair the instance actually runs. |
| `model_tier` | From spec `model` field | The tier name (`"fast"`, `"smart"`, `"deep"`) or empty if a concrete model was used. |
| `effective_tools` | Attenuation intersection at instantiation | `null` = unrestricted; `[]` = no tools. |
| `max_turns`, `timeout` | Spec frontmatter, defaulted from config | The structural limits in effect at spawn. |
| `disable_model_invocation`, `user_invocable`, `mode_switcher` | Spec frontmatter | As resolved at instantiation time. |
| `timestamp` | `time.Now().UTC()` at snapshot creation | Audit trail; not used for decisions. |

### Canonical serialization and hashing

To ensure stable hashes across equivalent snapshots (acceptance criterion), snapshots are serialised in canonical form before hashing:

- **Key order**: all object keys are sorted lexicographically by Unicode codepoint.
- **Whitespace**: 2-space indent, no trailing whitespace, a single trailing newline (LF, `\n`).
- **Numbers**: integers are serialised without decimal points or exponents. Floating-point values are not used in snapshots.
- **Strings**: Unicode, unescaped where possible (only `"`, `\`, and control characters < U+0020 are escaped). Solidus (`/`) is NOT escaped.
- **Arrays**: no trailing comma, consistent spacing.
- `null` vs absent: fields with Go zero values (empty string, 0, false, nil slice) are **omitted** rather than serialised as `null` or `[]`, except for `effective_tools` where `null` and `[]` have distinct semantics (unrestricted vs no tools).

The canonical hash is `sha256(canonical_json_bytes)`, stored alongside the snapshot for fast equality comparison without re-serialising.

### Forward and backward compatibility

| Direction | Behavior |
|---|---|
| **Old snapshot read by new binary** | Supported for all versions ≥ 1. New fields introduced in later versions default to Go zero values (empty string, 0, false). Old snapshots decode deterministically — the same snapshot bytes always produce the same in-memory representation. |
| **New snapshot read by old binary** | If `snapshot_version` is greater than the binary's `MaxSupportedSnapshotVersion`, the binary fails at load time with a clear error: `"snapshot version N is not supported by this binary (max: M)"`. The session is not loaded and the error is returned to the caller. No state is corrupted because the snapshot is only read, never rewritten by a binary that doesn't understand it. |
| **Snapshot written by any binary** | Always written at the binary's own `CurrentSnapshotVersion`. Never downgraded to an older format. |

### Migration policy

No automatic in-place migration. Snapshots are immutable once written. When a new snapshot version is introduced:

1. The new binary reads both old and new versions (backward compatibility).
2. New instances write the new version.
3. Old instances (from before the upgrade) retain their original snapshot version. They load successfully because the new binary supports old versions.
4. If a snapshot field's semantics change incompatibly, the `snapshot_version` is bumped and a new field name is introduced alongside the old one. The old field is kept for reading old snapshots; the new field is authoritative when present.

### Resume behavior

**Resume uses the historical snapshot, not the latest spec.**

When an agent is resumed:

1. The resuming process loads the original instance's `spec_snapshot` from the store.
2. It instantiates the new instance from that historical snapshot — same name, description, body, resolved model, effective tools, max_turns, and timeout as the original instance.
3. It does NOT re-resolve the spec file from disk. The spec file may have changed or been deleted since the original spawn.

Rationale: identity continuity. The child session was started under a specific identity; resuming it must preserve that identity. If the user wants a different spec, they spawn a new child, not resume the old one. The `resume` parameter on the `agent` tool can optionally override `resolved_model` (to resume with a different model) but cannot change the spec identity.

### Snapshot hash stability

Two snapshots with identical semantic content produce identical hashes, regardless of:

- The order in which the spec file's frontmatter was parsed
- The machine or time they were generated on (except `timestamp`, which differs)

**Note:** `timestamp` is deliberately excluded from the canonical hash. Including it would make every snapshot unique even when the spec is unchanged. The `source_hash` field already captures whether the source file changed.