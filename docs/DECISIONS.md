# Design Decisions

Answers to open questions raised during planning.

## 1. Distribution

**Q:** How will users install the binary? Homebrew tap, Artifactory generic repo, or manual download?

**A:** Binary hosted on Artifactory. A Confluence page will provide instructions including a scripted install that places the binary in `~/.local/bin` and ensures it's on PATH. No homebrew tap needed.

## 2. Token Caching

**Q:** Where and how long should tokens be cached?

**A:** Default token expiry is 8 hours. Override via CLI param `--expires 15m` (parsed as `time.Duration`). Cached in `os.UserCacheDir()` as `auth.json`.

## 3. Zscaler / Proxy

**Q:** Do we need to bypass Zscaler for MaaS traffic?

**A:** Probably not. The Python scripts likely had trouble with CA certs rather than needing a true proxy bypass. We'll obtain and install the Westpac + Zscaler root CAs the same way the DevOps installer does (see forge-bench setup scripts below). No need to set `Proxy: nil`.

Reference install scripts:

```bash
# Mac
curl -fsSL https://bitbucket.srv.westpac.com.au/projects/WDP2/repos/forge-bench/raw/podman/setup.sh | bash

# Windows
irm https://bitbucket.srv.westpac.com.au/projects/WDP2/repos/forge-bench/raw/podman/setup.ps1 | iex
```

These scripts add Westpac and Zscaler root CAs among other things.

## 4. CA Bundle

**Q:** Should we embed or locate the system CA bundle for TLS?

**A:** Same answer as #3. We'll rely on the system trust store (which includes Westpac/Zscaler CAs once the setup script has run). Go's `crypto/tls` uses the system store by default, so no special handling needed beyond documenting the prerequisite.

## 5. Cross-Compilation Targets

**Q:** Which OS/arch combos to build?

**A:** macOS (darwin/amd64, darwin/arm64) and Windows (windows/amd64). Linux not required — very few users on that platform.

## 6. Taskfile

**Q:** Use a Taskfile for build/test/lint?

**A:** Yes, Taskfile required. Will include targets for build (all platforms), test, lint, and release packaging.

## 7. OAuth/OIDC Libraries

**Q:** Hand-roll the OAuth PKCE browser flow or use existing libraries?

**A:** Use `github.com/int128/oauth2cli` + `github.com/coreos/go-oidc/v3` — the same stack the official `oc get-token` command uses (`openshift/oc/pkg/cli/gettoken`). This eliminates ~200 LOC of localhost callback server, state/nonce management, and PKCE verifier generation. Proven in production across the entire OpenShift ecosystem.

## 8. SA Token Minting Approach

**Q:** Use the legacy `SecretTypeServiceAccountToken` pattern (like `oc serviceaccounts new-token`) or the modern TokenRequest API?

**A:** TokenRequest API (`POST /api/v1/namespaces/{ns}/serviceaccounts/{sa}/token`). The legacy pattern is deprecated in `oc` itself ("Use `oc create token` instead"). TokenRequest directly mints bound JWTs with a specified expiry (our 90-day requirement) without the watch/wait dance for a secret controller. We issue a single HTTP POST with our existing HTTP client — no `client-go` dependency needed.

## 9. Token Cache Strategy

**Q:** How to cache OCP tokens on disk?

**A:** Follow the `oc get-token` pattern: SHA-256 hash of (issuer URL + client ID) as filename, stored as JSON in `~/.config/aim/`. On `aim token`, check cache → try refresh → fallback to full browser flow. This avoids redundant browser popups when the token is still valid or refreshable.

## 10. M2 Chat Client/Runtime Boundary

**Q:** Should `aim chat` use embedded NATS or another broker to separate the TUI client from the backend runtime?

**A:** No broker for M2. Keep the boundary, but keep it in-process.

The chat UI and chat runtime will be separated by typed commands/events and a
small runtime interface, but they will live inside the same binary and
communicate with plain Go channels. That gives us a detached-client-friendly
shape without taking on broker lifecycle, subjects, serialization, request
cancellation semantics, and extra failure modes before they are justified.

If a later milestone needs a genuinely detached runtime, multiple frontends, or
shared long-lived sessions across commands, we can add a transport layer then
and preserve the same command/event contract.

## 11. Chat TUI Framework

**Q:** Should `aim chat` use `go-tui` or the Charm stack?

**A:** Use Bubble Tea + Bubbles + Lip Gloss, with Glamour used selectively.

Bubble Tea is the better long-term fit for a maintained CLI product: broader
ecosystem, stronger documentation, more production usage, and mature building
blocks for textarea, viewport, spinner, help, and key handling.

Glamour should render completed assistant turns, not every in-flight delta.
During streaming, the UI should render plain text to avoid expensive markdown
reflow and flicker on each token.

`github.com/charmbracelet/crush` is a useful reference for UX patterns and
layout ideas, but not something we should depend on directly.

## 12. Repository Structure

**Q:** Should the repo stay as a flat `package main` layout now that chat is
growing into a real subsystem?

**A:** No. Perform a package restructure before the Bubble Tea UI grows much
further.

The temporary flat layout was acceptable for M1 and for landing the first chat
contracts/runtime scaffold, but it will become friction quickly once the TUI,
rendering, runtime, MaaS API logic, and CLI wiring all deepen.

Restructure into `cmd/aim` and `internal/...` packages with clear separation
between:

- CLI wiring
- platform concerns (auth, config, endpoints, HTTP)
- MaaS API access
- chat session/domain/runtime/streaming
- chat TUI/rendering

The current chat command/event contract remains the seam between runtime and UI.
That seam is what lets us restructure without changing the behavior model again.

## 13. UI/Test/Store Dependency Posture

**Q:** Which Crush-adjacent dependencies should AIM adopt now?

**A:** Adopt the terminal/UI stack, not the multi-provider stack.

Use:

- `charm.land/bubbletea/v2`
- `charm.land/lipgloss/v2`
- `charm.land/glamour/v2`

Plan for `sqlc` once AIM lands a real SQLite-backed persistence layer.

Do **not** adopt `charm.land/fantasy` at this stage. AIM currently talks to one
backend family: MaaS/OpenShift. A provider abstraction layer would be premature
until there is an actual multi-provider requirement.

## 14. Internal Pub/Sub

**Q:** Should AIM adopt a pub/sub package similar to Crush's `internal/pubsub`?

**A:** Yes, but as a local AIM package tailored to our needs.

Add `internal/pubsub` as a small typed in-process topic bus for runtime/UI and
service fan-out. Use it for decoupled internal communication where plain direct
channels would otherwise force awkward ownership or single-subscriber shapes.

Crush remains a reference, not a source tree to vendor wholesale. We should
copy the pattern, not couple AIM to Crush internals or import large unrelated
subsystems just because they exist upstream.
