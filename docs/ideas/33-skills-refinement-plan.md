# 33. Skills refinement: full alignment with the Agent Skills spec

## Status

Proposed refinement of the skills system in `internal/skills`, `internal/agent`,
`internal/chat`, and `internal/config`. Each section below is independent and
can be shipped incrementally, but they all converge on a single goal: tau
should be a high-fidelity consumer of the Agent Skills open standard rather
than a partial reimplementation.

## Why this matters

Tau's skills implementation already follows the core progressive-disclosure
pattern (catalog → SKILL.md body → bundled resources), but several spec
features are missing or partially implemented. The compact index now correctly
shows name + description + location + compatibility + bundled-resource hints,
and the file scanner matches `SKILL.md` case-insensitively. The remaining
gaps are the ones that distinguish a partial implementation from a complete
one that can trust skills the way other clients (Claude Code, Codex, Cursor)
do.

## Five sub-initiatives

### A. Skill activation deduplication

**Spec reference**: "Manage skill context over time → Deduplicate activations."

**Problem**: When the agent decides to load a skill (e.g. by `read`ing
`SKILL.md`), the same content may get re-injected multiple times if the
agent re-reads it, or if a `compact`/`summarise` operation resurfaces the
file path. This wastes tokens and risks drift between copies.

**Implementation**:
- `internal/chat/types.go` — Add `ActivatedSkills []string` to
  `ChatSessionConfig` (per-session) so dedup state is part of the session
  lifecycle and survives `compact`.
- `internal/agent/coordinator.go` — Track activations in a per-session set
  keyed by skill name. When a `read` tool call targets a path inside
  `<skill-directory>/SKILL.md`, check the set; if already activated, append
  a marker instead of letting the raw file content re-enter context:
  ```
  <skill_content name="pdf" already_activated="true">
  This skill is already loaded into context. Refer to the original
  activation above rather than re-reading.
  </skill_content>
  ```
- Reset the set on `/compact` and `/reload` so the user can force
  re-activation.

**Test coverage**: Add a coordinator-level test that simulates the agent
reading the same `SKILL.md` twice and verifies only the second read produces
the dedup marker.

### B. Skill content protection from compaction

**Spec reference**: "Manage skill context over time → Protect skill content
from context compaction."

**Problem**: Tau's `compact.md.tpl` summarises the entire conversation to
shrink the context window. If an activated `SKILL.md` body is in the history,
the compact operation may drop it, silently degrading the agent's behaviour
mid-session. The agent continues working but no longer has the specialised
guidance the skill provided.

**Implementation**:
- When the agent activates a skill, wrap the loaded body in identifying
  tags (as the spec recommends for structured wrapping):
  ```xml
  <skill_content name="pdf-processing">
  [original SKILL.md body]
  Skill directory: /home/user/.agents/skills/pdf-processing
  </skill_content>
  ```
- Update `internal/agent/templates/compact.md.tpl` to instruct the
  compacting agent: *preserve any `<skill_content>` blocks verbatim;
  summarise around them, never inside them*. Add an explicit example.
- Optionally add a `preserve_skills bool` field to `ChatSessionConfig` that
  defaults to `true` and can be flipped to `false` for experimental sessions
  where the user wants skills to be compacted too.

**Test coverage**: Snapshot the compact output for a fixture session that
includes a fake `<skill_content>` block; assert the block appears
character-for-character in the output.

### C. Project-level skill trust gating

**Spec reference**: "Discover skills → Trust considerations."

**Problem**: Tau scans `<project>/.agents/skills/`, `<project>/.claude/skills/`,
and `<project>/.tau/skills/` for skills. A freshly-cloned (potentially
malicious) repository can ship a `SKILL.md` that injects instructions into
the agent's context before the user has even read the project's README.
User-level skills are fine (the user installed them on purpose) but
project-level skills are untrusted by default.

**Implementation**:
- `internal/config/config.go` — Add `TrustedProjects []string` (paths the
  user has explicitly approved) and `TrustProjectSkills bool` (master
  switch, default `false`).
- `internal/skills/skills.go` — In `DefaultSources`, accept an additional
  `trustAllProjects bool` parameter (or read it from a passed
  `DiscoveryContext` struct). When false, skip project-level sources with a
  warning diagnostic:
  ```
  Warning: project-level skills in /path/to/repo are not loaded
  because the project is not trusted. Run `tau trust /path/to/repo`
  to enable them.
  ```
- `internal/cli/root.go` — Add a `tau trust <path>` subcommand that
  resolves the absolute path and appends to `TrustedProjects` in
  `~/.config/tau/config.yaml`.
- `internal/agent/prompt.go` — When project skills are skipped, the prompt
  builder should still mention this in a brief `<skills_notice>` block so
  the user knows why a skill they expected to see isn't in the catalog.

**Test coverage**: Verify that with default config, project-level skills are
not loaded; with `TrustProjectSkills = true`, they are. Verify the warning
diagnostic is emitted in the skipped case.

### D. `metadata` field surfacing in catalog

**Spec reference**: "Frontmatter → `metadata` field" and the
"optimizing-descriptions" doc which mentions that authors sometimes use
`metadata` to tag skills with domain markers.

**Problem**: Tau parses `metadata map[string]string` from the frontmatter
but never uses it. Some skills carry useful signals there (e.g.
`metadata.domain: kubernetes`, `metadata.author: anthropic`,
`metadata.tier: official`) that could help the agent judge relevance
without reading the SKILL.md body.

**Implementation**:
- `internal/skills/skills.go` — Extend `ToPromptIndex` to append a compact
  `[meta: k=v, k=v]` suffix when `skill.Metadata` is non-empty. Cap at
  3-4 key-value pairs to avoid token bloat; truncate values over 32 chars
  with `…`. Skip pairs whose key matches `description` or `compatibility`
  to avoid duplicating fields already shown.
- If a skill declares `metadata.compatibility` but not the top-level
  `compatibility` field, treat the metadata value as the compatibility
  hint (some authors do this). This is a permissive read, not a
  spec-violation.

**Test coverage**: Verify that skills with metadata get the `[meta: ...]`
suffix, that long values are truncated, and that overflow (more than 4
pairs) is silently capped.

### E. Dedicated `activate_skill` tool

**Spec reference**: "Activate skills → Dedicated tool activation." This is
the biggest architectural change in this plan but also the one that
unlocks the most spec benefits.

**Problem**: Currently the agent activates a skill by `read`ing its
`SKILL.md` directly. The host (tau) has no record of when activation
happens, no chance to strip frontmatter, no chance to enumerate bundled
resources for the agent, and no way to dedup or protect content (per
A and B above).

**Implementation**:
- `internal/agent/tools/` — Add a new `activate_skill` tool registered
  alongside the other tools. Schema:
  ```json
  {
    "name": "activate_skill",
    "description": "Load a skill's full instructions and bundled resources.",
    "parameters": {
      "type": "object",
      "properties": {
        "name": {
          "type": "string",
          "enum": ["pdf-processing", "go-development", "..."],
          "description": "Name of the skill to activate"
        }
      },
      "required": ["name"],
      "additionalProperties": false
    }
  }
  ```
  Constrain `name` to the set of valid skill names (per the spec: "constrain
  the name parameter to the set of valid skill names... to prevent the model
  from hallucinating nonexistent skill names").
- If no skills are available, do NOT register the tool at all.
- The tool's handler:
  1. Looks up the skill in the session's known skill set.
  2. Returns a structured wrapper:
     ```xml
     <skill_content name="pdf-processing" activated="2026-06-06T12:34:56Z">
     [SKILL.md body, frontmatter stripped]
     Skill directory: /home/user/.agents/skills/pdf-processing
     Relative paths in this skill are relative to the skill directory.
     <skill_resources>
       <file>scripts/extract.py</file>
       <file>references/pdf-spec-summary.md</file>
     </skill_resources>
     </skill_content>
     ```
  3. Records the activation in the session's `ActivatedSkills` set (see A).
- `internal/agent/coordinator.go` — Plumb the activation call through so
  the dedup and content-protection flows (A, B) work transparently.
- Update `internal/agent/templates/agent.md.tpl` so the catalog text
  switches from "use read to load its SKILL.md" to "call the
  `activate_skill` tool with the skill's name".

**Test coverage**: Verify the tool is registered when skills are present
and not registered when none are. Verify a successful activation returns
the wrapped body and records the activation. Verify calling `activate_skill`
with an unknown name returns an error result.

## Recommended implementation order

The five items have some natural dependencies. Suggested order:

1. **D** (metadata surfacing) — small, isolated, no architectural change.
2. **A** (dedup) — sets up the per-session tracking that B and E reuse.
3. **B** (content protection) — depends on A's activation tracking; small
   template change.
4. **C** (trust gating) — security-critical, independent of the others.
5. **E** (dedicated tool) — biggest change, depends on A, B, and uses the
   activation infrastructure. Worth doing last so the supporting pieces
   are in place.

## Out of scope for this pass

- Re-implementing the spec's validator (Tau's `validateSkill` is already
  lenient in the spec's recommended way).
- A `tau skill install` subcommand (skill management UX is a separate
  initiative).
- Multi-modal skill resources (images, audio) — out of scope for a TUI
  agent.
- Subagent delegation of skill execution — the spec marks this as
  advanced and optional.
