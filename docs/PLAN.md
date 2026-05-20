# aim — AIM CLI

A single-binary Go CLI that replaces the oc-maas-chat / oc-login / oc-aim-bench
Python+PowerShell toolchain for interacting with the AI Models (AIM) platform on
OpenShift AI.

## Goals

- **Zero runtime dependencies** — single static binary, no Python, no venv, no oclib.py
- **Fast** — sub-second startup, no self-update-on-every-run
- **Safe** — tokens hashed and cached, no kubeconfig writes, TLS verification configurable
- **Simple** — small cohesive packages, no framework-heavy over-abstraction
- **Beautiful** — full TUI chat experience via Bubble Tea

## What it replaces

| Current script           | Lines | Problems                                                                            |
| ------------------------ | ----: | ----------------------------------------------------------------------------------- |
| oc-maas-chat.py          | 2,282 | Python dep, httpx venv bootstrap, self-update on run, downloads oclib.py at runtime |
| oc-aim-bench             | 1,402 | Async Python, same bootstrap mess, requires httpx                                   |
| oc-aim-eval              | 2,794 | Needs lm-evaluation-harness, uploads to Confluence                                  |
| oc-maas-chat-install.ps1 |    68 | Writes .cmd wrappers, edits $PROFILE                                                |
| oc-login.ps1             |   965 | PowerShell-only, duplicates OAuth logic                                             |
| oc-runner.ps1            | 1,127 | Runs oc across clusters, PowerShell-only                                            |
| oclib.py                 |  ~400 | Shared lib downloaded at runtime from Bitbucket                                     |
| maas-token-mint          |   904 | Bash, scaffolds SA YAML + mints 90-day JWTs                                         |

## Commands

```bash
aim token                  # Print a fresh MaaS JWT to stdout (pipe-friendly)
aim models                 # List available models
aim chat [--model X]       # Interactive streaming TUI chat
aim bench [--model X]      # Benchmark a model (TTFT, ITL, throughput)
aim admin login [hint]     # OAuth PKCE login → kubeconfig (replaces oc-login)
aim admin run [hints] -- oc args   # Run oc across clusters (replaces oc-runner)
aim admin mint [flags]     # Mint 90-day app SA token (replaces maas-token-mint)
aim admin eval [flags]     # LM eval harness runner (replaces oc-aim-eval)
aim version                # Print version
```

### `aim token`

1. OAuth 2.1 + PKCE browser flow → OCP token (auto-select OneAccess IDP)
2. POST /maas-api/v1/tokens → MaaS JWT (configurable expiry, default 4h)
3. Print JWT to stdout, all progress to stderr

Supports `--ocp-token` flag to skip OAuth and exchange a pre-existing token.
Supports `--expiry 8h` to set token lifetime.

Usage: `export MAAS_TOKEN="$(aim token)"`

### `aim models`

GET /maas-api/v1/models → table of model IDs, route URLs, and ready status.

### `aim chat`

Full TUI chat experience built on the Charm stack:

- In-process client/runtime split: the TUI frontend talks to a chat runtime via
  typed commands/events and channels, so the boundary is clean without adding a
  broker in M2
- Bubble Tea app loop with Bubbles textarea, viewport, spinner, and help components
- Lip Gloss styling for layout, theme, and status surfaces
- Glamour rendering for completed assistant turns; in-flight streaming stays plain text to avoid markdown reflow flicker
- Crush used as a UX/reference source for interaction patterns and layout ideas, not as a runtime dependency

  ### Colors (Westpac NOW Theme)

  | Color       | Hex       | Usage                      |
  | ----------- | --------- | -------------------------- |
  | Dark Navy   | `#181B25` | Primary text, headers      |
  | White       | `#FFFFFF` | Light backgrounds          |
  | Westpac Red | `#DA1710` | Primary accent, hyperlinks |
  | Navy Blue   | `#1F1B4F` | Secondary accent           |
  | Purple      | `#9819D7` | Accent                     |
  | Light Gray  | `#E8E8ED` | Subtle backgrounds         |

  ### Typography

  - **Headings:** Westpac Bold (fallback: Arial)
  - **Body:** Nunito Sans (fallback: Arial)
  - **Headlines:** ALL CAPS
  - **Subheadings:** Title Case

  ### Formatting Rules

  - Date format: DD/MM/YYYY
  - Use varied layouts — avoid monotonous text-heavy blocks
  - Bold all headers and inline labels

- Alternate-screen settings panel (model, system prompt, max tokens)
- Runtime event pump for non-blocking stream handling
- Ctrl+C cancels in-flight requests cleanly, with debounced twice-press to exit
- Slash commands: `/new`, `/model`, `/system`, `/exit` *with auto-completion*
- Conversation history saved to disk (`~/.config/aim/history.json`) for persistence across sessions - with option for in-memory-only mode (`--no-history` or `AIM_NO_HISTORY=true`) for non-persistent sessions or sensitive conversations.

The Bubble Tea client sits on top of the in-process chat runtime and consumes
the existing command/event boundary. The M2 implementation stays single-binary
and in-process; if we later need a detached runtime, we can swap the transport
under the same command/event model instead of rewriting the UI.

### `aim bench`

vLLM-style benchmarking against the MaaS OpenAI-compatible endpoint:

- Metrics: TTFT, ITL, TPOT, E2E latency (mean, p50, p90, p95, p99)
- Output throughput (tok/s), request throughput (req/s)
- Concurrent request load testing (configurable parallelism)
- Output formats: table, JSON, CSV
- Benchmark all models or a specific one

### `aim admin login`

- Fuzzy-match cluster from bundled cluster list
- OAuth PKCE → OCP token
- Write scoped kubeconfig
- Token cache with expiry check (reuse valid tokens)
- Optional: auto-download `oc` binary if missing

### `aim admin run`

- Run `oc` commands across multiple clusters in parallel
- Capture (rc, stdout, stderr) per cluster
- Save results to disk for offline search
- `aim admin run search` subcommand to slice results

### `aim admin mint`

- Scaffold ServiceAccount YAML for a new app consumer
- Mint 90-day SA-bound JWT (after ArgoCD sync)
- Update tracking.yaml
- Add RBAC for new models (`--add-rbac` mode)

### `aim admin eval`

- Run LM Evaluation Harness tasks against MaaS models
- Tier 1 (release gate), Tier 2 (use-case), Tier 3 (deep-dive)
- Optional Confluence upload of results

## Architecture

Single binary, but no longer a flat root package. Restructure before the Bubble
Tea UI grows further.

```tree
/docs
  DECISIONS.md         # Architectural decisions and rationale
  PLAN.md              # High-level plan and milestones
/cmd/aim
  main.go              # entrypoint only
/internal/cli
  root.go              # cli root + global flags
  token.go             # aim token command wiring
  models.go            # aim models command wiring
  chat.go              # aim chat command wiring
/internal/platform
  auth.go              # OAuth OIDC+PKCE flow
  config.go            # ~/.config/aim/config.yaml
  endpoints.go         # endpoint registry (dc/env → URLs)
  http.go              # shared HTTP client (TLS, timeouts)
/internal/maas
  token.go             # MaaS token exchange
  models.go            # model discovery
/internal/chat
  types.go             # session state and command/event contracts
  runtime.go           # in-process chat runtime/session orchestration
  stream.go            # SSE stream parser/client for chat completions
/internal/pubsub
  bus.go               # typed in-process topic bus for runtime/UI/services
/internal/store
  doc.go               # persistence package placeholder; history lands here next

# Planned next under /internal/chat:
#   tui/model.go       # Bubble Tea root model
#   tui/messages.go    # Bubble Tea messages/commands mapping
#   tui/styles.go      # Lip Gloss theme and layout
#   tui/render.go      # viewport/input rendering helpers
#   tui/markdown.go    # Glamour rendering for completed turns
/internal/bench
  bench.go             # aim bench — benchmarking
```

## Dependencies

- `github.com/urfave/cli/v3` — CLI framework
- `charm.land/bubbletea/v2` — TUI runtime/event loop
- Bubble Tea component packages — textarea, viewport, spinner, help, and related widgets
- `charm.land/lipgloss/v2` — styling/layout
- `charm.land/glamour/v2` — markdown rendering for completed assistant turns
- `sqlc` — planned code generation tool once AIM lands a real SQLite-backed store
- `github.com/int128/oauth2cli` — local OAuth callback server + PKCE auth code exchange (from oc get-token)
- `github.com/coreos/go-oidc/v3` — OIDC discovery, ID token verification, PKCE method detection
- `golang.org/x/oauth2` — OAuth2 config, token source, refresh flow
- `github.com/pkg/browser` — cross-platform browser.OpenURL()
- `gopkg.in/yaml.v3` — config
- Standard library for everything else (net/http, crypto, encoding/json)

`github.com/charmbracelet/crush` is a reference application we can study for UX
and architectural ideas, but it is not a direct dependency.

`charm.land/fantasy` is explicitly out of scope for now. AIM is not trying to
be a multi-provider LLM shell; it is a focused MaaS/OpenShift client, so a
provider abstraction layer would add indirection without solving a real current
problem.

### Why these OAuth deps?

The official `oc` CLI (`openshift/oc/pkg/cli/gettoken`) implements the same
OIDC + PKCE browser flow we need. Rather than hand-rolling a localhost callback
server (~200 LOC), state/nonce management, and PKCE verifier generation, we
reuse the same battle-tested libraries:

- `int128/oauth2cli` handles: local HTTP server, redirect capture, state
  verification, and code→token exchange in a single `GetToken()` call.
- `coreos/go-oidc/v3` handles: `.well-known/openid-configuration` discovery,
  `id_token` verification, and probing `code_challenge_methods_supported`.

### What we deliberately skip from oc

- `spf13/cobra` + `k8s.io/cli-runtime` — massive dep tree; urfave/cli is lighter
- `client-go` / k8s API machinery — for `aim admin mint` we issue a raw
  `POST /api/v1/namespaces/{ns}/serviceaccounts/{sa}/token` (TokenRequest API)
  with our existing HTTP client instead of importing the entire k8s client stack
- `credwriter` — ExecCredential protocol for kubectl plugins; not needed

## Config

`~/.config/aim/config.yaml`:

```yaml
default_endpoint: rcc/npr
default_model: nemotron-nano-9b
token_expiry: 8h
skip_tls_verify: false
```

## Endpoints

Bundled from the reference scripts:

| DC   | Env  | MaaS Gateway                                    | OCP API                                                            |
| ---- | ---- | ----------------------------------------------- | ------------------------------------------------------------------ |
| RCC  | eDev | https://api.ai.rcc.edev.ocp.srv.westpac.com.au  | https://api.ocp-rcc-edev-isd-100.edev.ocp.srv.westpac.com.au:6443  |
| RCC  | NPR  | https://api.ai.rcc.npr.ocp.srv.westpac.com.au   | https://api.ocp-rcc-npr-isd-100.npr.ocp.srv.westpac.com.au:6443    |
| WSDC | eDev | https://api.ai.wsdc.edev.ocp.srv.westpac.com.au | https://api.ocp-wsdc-edev-isd-100.edev.ocp.srv.westpac.com.au:6443 |
| WSDC | NPR  | https://api.ai.wsdc.npr.ocp.srv.westpac.com.au  | https://api.ocp-wsdc-npr-isd-100.npr.ocp.srv.westpac.com.au:6443   |

## Auth Flow

```
Browser ──PKCE──→ OpenShift OAuth ──→ OCP token (sha256~...)
                                         │
                                         ▼
                                POST /maas-api/v1/tokens
                                         │
                                         ▼
                                    MaaS JWT (4h)
                                         │
                                         ▼
                              Bearer auth on model routes
```

### Token Lifecycle (from oc get-token pattern)

1. **Cache hit** — check `~/.config/aim/auth.json` for unexpired OCP token → skip to step 4
2. **Refresh** — if refresh token exists, attempt token refresh via OIDC token endpoint
3. **Full auth code** — if refresh fails or no cache, launch browser PKCE flow:
   - OIDC discovery on OCP OAuth server
   - Verify `code_challenge_methods_supported` includes S256
   - `int128/oauth2cli.GetToken()` → spins up localhost server, opens browser,
     captures redirect, exchanges code for tokens
4. **MaaS exchange** — POST OCP token to `/maas-api/v1/tokens` → MaaS JWT
5. **Cache write** — persist OCP token + refresh token (SHA-256 keyed filename)

## Build

```
go build -o aim ./cmd/aim
```

## Milestones

1. **M1: token + models** — OAuth flow, token exchange, model listing
2. **M2: chat TUI** — Bubble Tea chat on top of the in-process chat runtime
3. **M3: bench** — model benchmarking with latency/throughput metrics
4. **M4: admin login** — cluster login (oc-login replacement)
5. **M5: admin run** — multi-cluster oc runner
6. **M6: admin mint + eval** — SA token minting, LM eval harness (optional)
