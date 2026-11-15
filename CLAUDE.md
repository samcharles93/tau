# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

## Repository Profile

This repository opts in to the **team-maintainer** profile. Agents commit finished work in conventional-commit form
and push it (`git push`) without asking first. Never force-push; if a push is rejected, stop and report it.

Tau is a TUI-only terminal agent. The Web UI was removed on 2026-11-07; don't reintroduce browser or HTTP frontends.

## Build & Test

```bash
task           # build the Go binary
task check     # gofumpt, go vet, go fix, golangci-lint, race tests, govulncheck, docs audit
task test:race # tests with the race detector
```

Run `task check` before calling work done. See `AGENTS.md` for commit conventions and package-level detail.

## Architecture Overview

Root and child agent callers share dependency-neutral lifecycle primitives:

- `internal/agent/instance/` owns model resolution, depth enforcement, effective-tool attenuation, spec snapshots and
  hashes, and collision-safe instance persistence/ownership transfer for root, fresh-child, and resumed-child paths.
  Root trust resolution and child session seeding remain at the caller.
- `internal/agent/prompttmpl/` owns the common agent spec-body template data, bounded workspace tree, and function map.
  Root-only system-prompt composition remains in `internal/agent/prompt.go`.

## Conventions & Patterns

- Format Go with `gofumpt`, not `gofmt`.
- Commit messages use conventional commits (release-please reads them); see `AGENTS.md`.
