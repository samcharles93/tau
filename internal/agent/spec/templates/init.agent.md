---
name: init
description: Generate an AGENTS.md for the current project
tools: [read, find, grep, write]
disable-model-invocation: true
metadata:
  category: documentation
---
Analyze this codebase and create or update the repository's **AGENTS.md** file to guide future automated agents working in this repository.

**Pre-flight Check**: Check if the workspace directory is completely empty or contains only base configuration files. If so, abort immediately and report: "Directory appears empty or only contains config. Add source code first, then run /init to generate AGENTS.md."

**Objective**: Document the non-obvious rules, specialized patterns, automation commands, and architectural boundaries an agent must know to work productively here without trial-and-error discovery.

**Discovery Process**:
1. Map out the tree structure using file discovery tools.
2. Locate and check existing instruction assets (`.cursorrules`, `.cursor/rules/*`, `claude.md`, etc.). Only read if present.
3. Identify project ecosystem from package managers, lockfiles, and configs (e.g., `go.mod`, `Taskfile.yaml`).
4. Read highly representative domain, infrastructure, and entry-point source files to parse the architecture style.

**Required Documentation Sections**:
- **Essential Commands**: Build, test, lint, and generation workflows with necessary flags.
- **Project Architecture**: High-level control flow, package communication rules, and layer descriptions.
- **Strict Constraints**: Rigid patterns (e.g., no hardcoded strings, exact formatting engines like gofumpt, mandatory shared package utilization).
- **Gotchas**: Non-obvious characteristics, hidden pitfalls, or unexpected framework patterns.

<rules>
- Progressive Disclosure: Do NOT document standard, obvious details that an LLM would instantly see by opening a single file. Focus purely on cross-package architecture constraints and implicit codebase behavior.
- Document only what you explicitly observe. Never extrapolate or guess commands, paths, or conventions.
</rules>

<env>
Working directory: {{.WorkingDir}}
Platform: {{.Platform}}
Today's date: {{.Date}}
</env>
