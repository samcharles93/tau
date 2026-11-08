---
name: lint-backlog
description: Use when clearing a golangci-lint backlog (e.g. a beads issue like "clear the lint backlog so task check passes") or doing a large mechanical refactor across many files in tau. Covers the batch order, the per-linter playbook, the gotchas found the hard way, and the quality gates before committing. Trigger on "lint backlog", "golangci-lint findings", "task check fails", "refactor this function", or any multi-file complexity cleanup.
user-invocable: true
---

# Clearing a Lint Backlog (and big mechanical refactors)

Proven workflow from clearing tau-qin (165 findings -> 0). The discipline that made it safe: **small verified steps, lint+tests after every step, never batch unverified edits.**

## 1. Measure and triage

```bash
golangci-lint run ./... 2>&1 | tail -25                      # issue count + per-linter histogram
golangci-lint run ./... 2>&1 | grep -E "^internal|^pkg|^cmd|^scripts" > /tmp/lint.txt
sed -E 's/.*\(([^)]+)\)$/\1/' /tmp/lint.txt | sort | uniq -c | sort -rn   # by linter
sed 's/:[0-9]*:[0-9]*:.*//' /tmp/lint.txt | sort | uniq -c | sort -rn       # by file
```

- `.golangci.yaml` sets `max-issues-per-linter: 0` / `max-same-issues: 0`, so the count is exact and trackable downward. Keep it that way.
- Test files are exempt ONLY from complexity linters (cyclop/dupl/funlen/gocognit/gocyclo/maintidx) — `forcetypeassert`, `errcheck`, `noctx` etc. still fire in `_test.go`.
- To tell pre-existing findings from ones you introduced: `git worktree add /tmp/tau-head HEAD` and run golangci-lint there. (Plain `git stash` leaves untracked files behind and breaks typecheck.)

## 2. Batch order (mechanical first, complexity last)

1. **Formatters**: `golangci-lint fmt ./...` handles gci/gofumpt findings (`gofumpt -w` alone does NOT apply the config's `extra-rules`).
2. **One-off hygiene**: unconvert, predeclared (rename `cap`/`max`/`real`), ST1005 (lowercase error strings), godox (reword FIXME/TODO/BUG comments to "defect"), unused (delete dead code — e.g. a build-tag-gated no-op that nothing calls), staticcheck.
3. **Test-file correctness**: errcheck (`_ =` for buffer writes, `require.NoError` where available), forcetypeassert (prefer ONE generic helper over 27 checked assertions — e.g. make `drainUntil[T chat.ChatEvent]` return the typed value), noctx (use `ExecContext`/`QueryRowContext`).
4. **nilerr / contextcheck / containedctx / interfacebloat** — see playbook below; these are judgment calls, not mechanical.
5. **Complexity** (gocognit/gocyclo/cyclop/funlen/nestif/dupl/gocritic): file by file, extract named helpers, re-lint that package after each file.

## 3. Per-linter playbook (what actually worked)

- **gocognit/gocyclo/cyclop on a big function**: extract logical chunks into named helpers. Big type switches -> split into per-domain helpers returning `(handled bool)` or `(attrs, nil)` (see `handleCommand` -> `handleSessionLifecycleCommand`/`handleExtensionCommand`/`handlePersistCommand`/`handleSkillAgentBashCommand`, and the `chatCommandLogAttrs`/`chatEventLogAttrs` splits). Closures/callbacks (WalkDir, stream callbacks, event handlers) -> named factory funcs taking the shared state as params (`makeSkillWalkFunc`, `llmStreamCallbacks`, `childResultReader`).
- **funlen** counts lines AND statements — a giant struct literal counts. Extract a builder func (`newCoordinatorStruct`, `rootFlags` -> `rootConnectionFlags` + `rootSessionFlags`).
- **nestif**: flatten early-return guards; extract the nested block into a helper with a sentinel return (`done bool, err error`).
- **dupl** (threshold 150): extract the shared block into one helper — single source of truth (see tui2 reasoning/tools duplicate).
- **gocritic**: `singleCaseSwitch` -> `if`; `ifElseChain` -> `switch`; `exitAfterDefer` -> move `os.Exit` out of the deferred function (`main` -> `os.Exit(run())`); `appendAssign` -> reassign; `deprecatedComment` -> blank line before `Deprecated:`.
- **nilerr** (`if err != nil { ...; return nil }`): in WalkDir callbacks this IS the continue contract — do not "fix" by returning err (that aborts the walk). Per-site `//nolint:nilerr // reason` is the honest fix. Same for errors deliberately converted to a tool `Result` with `IsError: true`.
- **contextcheck**: thread a `ctx context.Context` param through the call chain (loop -> handleCommand -> handlers). On the shutdown path where the parent ctx is already cancelled, use `context.WithoutCancel(ctx)` — NOT `context.Background()` — it keeps values and satisfies the linter (see `persistSession`).
- **containedctx** (struct holding a ctx field): app-lifetime contexts on Coordinator/Bus/worker/TUI models are by design — per-field `//nolint:containedctx // reason`, not a config exclusion.
- **interfacebloat** (>8 methods): for cohesive core interfaces (SessionStore, plugin Host, taui Terminal) use per-interface `//nolint:interfacebloat // reason`. Prefer targeted nolint over raising the config max (which hides future real bloat).

## 4. Refactor safety rules (learned the hard way)

- **Verify behavior, not just compilation.** One extraction changed a budget-rejection error kind (`invalid_params` -> `agent_tool_error`) and only a test caught it. After any extraction, run the package tests: `go test ./internal/<pkg>/`.
- **Scripted splices on Go code mangle braces.** Prefer small `edit`-tool changes, or python splices with `assert old in src` + immediate `go build`. When a splice goes wrong, read the actual region and repair by hand — do not retry the same script.
- `go fix ./...` rewrites idioms (e.g. `for _, f := range strings.Fields(...)` -> `FieldsSeq`); run `go build ./...` right after.
- Keep public interfaces stable unless the task requires changing them; update callers in the same change.
- Don't fix pre-existing findings in files you merely touched unless they're in the backlog scope — but DO fix findings YOUR change introduced (compare against the HEAD worktree).

## 5. Quality gates before committing (all required)

```bash
gofumpt -extra -w . && go fix ./... && go build ./...
golangci-lint run ./...                      # must be 0 issues (or the agreed residual)
go test ./...                                # full suite
go test -race ./internal/<touched packages>/
GOOS=windows go build -o /tmp/tau.exe ./cmd/tau
GOOS=darwin go build -o /tmp/tau-darwin ./cmd/tau
```

Then the team-maintainer close-out: conventional commit (`fix:`/`refactor:`/`chore:` per AGENTS.md), `git push`, `bd close <id>`, `bd dolt push`, commit the `.beads/issues.jsonl` sync (`chore(beads): sync issue export`), push again.

## 6. Gotchas specific to tau found during tau-qin

- FTS5 `'delete'` special INSERT command only works on external-content/contentless tables; a regular FTS5 table needs `DELETE FROM fts WHERE rowid = old.id` in the trigger.
- `golangci-lint fmt` is the reliable way to apply gci/gofumpt config formatting.
- `bd update <id> --claim` then work; `bd close <id> --reason "..."` at the end.
