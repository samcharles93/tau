---
name: ai-sdk-workflow
description: This skill should be used when developing, testing, or releasing changes to ai-sdk, tau's LLM provider SDK - for example when a task involves "fix a provider bug", "add a new LLM provider", "the API returned a 400", "release a new ai-sdk version", "bump ai-sdk in tau", or anything touching request building, response parsing, streaming, or usage/cost accounting for OpenAI/Anthropic/Ollama/etc.
user-invocable: true
---

# ai-sdk Workflow

## Repo layout - two repos, not one

- `/work/apps/tau` is the CLI/TUI app.
- `/work/projects/ai-sdk` is a **separate git repo** (`github.com/samcharles93/ai-sdk`) implementing every LLM provider (`provider/openai`, `provider/anthropic`, `provider/ollama`, etc.) plus the shared `chat` types and `runtime` provider registry.
- `go.work` at tau's root links them (`use ... /work/projects/ai-sdk`), so tau always builds against the **local ai-sdk checkout** regardless of what `go.mod`'s `require` line pins. Edits to ai-sdk take effect in tau immediately, without publishing.
- Route provider bugs to ai-sdk, not tau. A wire-format 400, a missing usage field, a wrong tool-call shape - fix it in `provider/*` inside ai-sdk. tau only consumes `chat.Request`/`chat.Response`/`chat.Usage`.
- ai-sdk has two remotes: `gitea` (private, `git.catlow.cloud`) and `origin` (public GitHub). Push to both every time; never let them drift.

## Release workflow

Follow this sequence for every ai-sdk change, in order:

1. Make the fix in `/work/projects/ai-sdk`.
2. Run `gofumpt -l <files>`; it must print nothing. Run `gofumpt -w` if it does.
3. Run `go build ./...`, `go vet ./...`, `go test ./...`; all must pass clean.
4. Run `golangci-lint run ./...` and `staticcheck ./...`; both must report zero issues. CI fails the build on any lint finding even when tests pass - do not skip this step. A stale `./pkg/...` path in `Taskfile.yaml`/`.github/workflows/ci.yml` sat broken through an entire tag because only `go test` was checked locally, not the lint job.
5. Write a regression test with `httptest.NewServer` reproducing the exact wire shape of the bug.
6. Live-verify against the real provider (see "Testing philosophy" below) before considering the fix done.
7. Commit with a clear conventional-commit message.
8. Tag with `git tag -a vX.Y.Z -m "..."` - bump patch for fixes, minor for features.
9. Push to both remotes: `git push gitea main && git push gitea vX.Y.Z`, then the same for `origin`.
10. Confirm CI is actually green with `gh run list --limit 3` followed by `gh run watch <run-id> --exit-status`. Do not assume; watch it.
11. Return to tau and run `go get github.com/samcharles93/ai-sdk@vX.Y.Z` to bump `go.mod`/`go.sum`. Do this even though `go.work` already points at the local copy - it is the source of truth for anyone building without the workspace, including CI.
12. Run `go build ./...`, `go vet ./...`, `go test ./...` in tau, then commit the dependency bump as its own `chore(deps):` commit, separate from unrelated changes.

Expect `go build`/editor tooling to sometimes auto-rewrite import paths in tau's own `.go` files after a version bump (e.g. `ai-sdk/pkg/chat` → `ai-sdk/chat` following an ai-sdk package-layout refactor). Treat this as normal and correct once the build and vet pass clean - do not manually revert it.

## Testing philosophy: mocks are not enough

Across one session, five distinct real bugs in the OpenAI Responses API path and Anthropic's thinking API were all invisible to `httptest`-mocked tests and only surfaced by making a real API call. Mocks encode assumptions about the wire format and cannot catch a shape the author didn't know existed.

- Write a throwaway `go run` script in the scratch directory that builds a minimal `chat.Request` and calls the provider directly. Never commit this script unless the user asks for it to be kept for reuse, in which case save it at the path they name (e.g. `/work/scratch/`).
- Hit the real endpoint and read the actual error or response shape.
- Convert each finding into both a fix and a permanent `httptest`-based regression test, so future verification doesn't require burning another real API call.
- When a live error references an unfamiliar field or concept (e.g. `"thinking.type.adaptive"`, `"output_config.effort"`), ask the user for the relevant vendor docs section instead of guessing field-by-field against their live, billed account. Users often hold current docs that postdate a model's training cutoff.

## Secrets

This environment stores API keys in `bws` (Bitwarden Secrets Manager CLI), not plain environment variables or dotfiles.

- Never grep shell configs, dotfiles, or `fish_variables` looking for a key. If a key appears unset in the shell, ask how it's stored instead of hunting for it.
- Expect the user to hand over the exact retrieval command for a specific secret, e.g. `bws secret get <secret-id> | jq -r .value`.
- Use command substitution so the raw value never appears in any tool call or its output: `ANTHROPIC_API_KEY=$(bws secret get <id> | jq -r .value) go run script.go`.
- Do not run `bws secret list` / `bws project list` speculatively to hunt for keys; treat that as a step the user gates.
