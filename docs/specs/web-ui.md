# Technical Specification: Tau Web UI

**Document Status:** Draft
**Version:** 0.1
**Author:** Tau Agent
**Date:** 2026-06-27
**Reviewers:** (pending)

## Executive Summary

This specification adds an optional, embedded Vue 3 web interface to Tau. When a user runs `tau` in a terminal, the same process also starts a local HTTP/WebSocket server. A browser (optionally auto-opened with `--web`) can connect to that server and observe or continue the same chat session. The web UI is a first-class peer to the existing TUI: it subscribes to the same `ChatEvent` stream and sends the same `ChatCommand` messages through the coordinator.

**Problem:** The inline TUI is powerful but limited by terminal constraints (no rich layout, no images, complex tool-call UX, no mobile access). Users want a richer, more approachable interface without giving up the terminal-first workflow.
**Solution:** Add a local web server + embedded SPA that mirrors the TUI’s command/event contract over WebSocket. The terminal remains the default launcher; the browser is an optional continuation surface.
**Impact:** Enables richer UI experiments, mobile access, and easier onboarding for non-technical users while keeping Tau a single short-lived process.

---

## 1. Background

Tau’s architecture already cleanly separates the chat runtime from any client:

- `agent.Coordinator` implements `chat.ChatRuntime` and owns the turn loop.
- The coordinator publishes `chat.ChatEvent` values on `internal/eventbus.Bus`.
- Clients send `chat.ChatCommand` values via `Coordinator.Send()`.
- The TUI (`internal/tui`) is just one subscriber/publisher pair on the bus.

Because the TUI does not own the coordinator, a second client can subscribe to the same bus and send commands through the same `ChatRuntime` interface. The web UI is therefore a new client on the bus, not a rewrite of the runtime.

The embedded SPA approach is borrowed from the sibling project `spawn` (`/work/apps/spawn`), which already ships a Vue 3 + Vite + Tailwind v4 + shadcn-vue SPA embedded in the Go binary via `//go:embed`.

---

## 2. Goals

1. **Terminal-first, web-optional:** `tau` continues to work exactly as it does today. The web UI is additive.
2. **Single process, single session:** One `tau` process owns one coordinator session. TUI and web clients are observers/controllers of that session.
3. **First-class peer:** The web UI uses the same `ChatEvent`/`ChatCommand` types as the TUI; no custom runtime API.
4. **Local-only:** Bind `127.0.0.1` only. No authentication in this phase.
5. **Reusable stack:** Match `spawn` — Vue 3, Vite, Tailwind v4, shadcn-vue, Pinia — so the build/embedding toolchain is consistent.
6. **Documented protocol:** Publish an AsyncAPI spec for the WebSocket protocol so mobile/future clients can implement it.

## 3. Non-Goals

1. **Remote/daemon mode:** `tau serve` as a persistent multi-session daemon is out of scope here. It is an expected follow-up.
2. **Authentication/authorization:** Only localhost access. Security is deferred to the daemon phase.
3. **Replacing the TUI:** The TUI remains the default interactive client.
4. **Real-time collaborative editing:** Multiple browsers may connect, but they share a single session state; concurrent user input is serialized, not merged.
5. **Images/vision in LLM messages:** The web UI can display markdown and tool output, but vision model support is a separate feature.

---

## 4. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         tau process                             │
│                                                                  │
│  ┌─────────────┐      ChatCommand       ┌──────────────────┐   │
│  │   TUI       │ ───────────────────────▶│                  │   │
│  │  (terminal) │◀──── ChatEvent ─────────│  agent.          │   │
│  └─────────────┘                         │  Coordinator     │   │
│                                          │                  │   │
│  ┌─────────────┐      ChatCommand       │  eventbus.Bus    │   │
│  │   Web UI    │ ───────────────────────▶│  (ChatEvent)     │   │
│  │  (browser)  │◀──── WebSocket ──────────│                  │   │
│  └─────────────┘                         └──────────────────┘   │
│        ▲                                                         │
│        │ WebSocket                                               │
│        ▼                                                         │
│  127.0.0.1:PORT                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Package | Responsibility |
| --------- | ------- | -------------- |
| WebSocket bridge | `internal/bridge` | Subscribes to `ChatEvent`, forwards to browsers; receives JSON commands and calls `Coordinator.Send()` |
| HTTP/WS server | `internal/server` | Serves the embedded SPA and mounts the WebSocket endpoint |
| Embedded SPA | `internal/spa/dist` | Built `dist/` of the Vue app, embedded with `//go:embed` |
| Vue SPA source | `internal/webui/src` | Vue 3 chat app: message stream, tool cards, input, settings |
| AsyncAPI spec | `docs/asyncapi/tau.yaml` | Machine-readable protocol documentation |
| CLI integration | `internal/cli`, `internal/app` | Add `--web`, `--port`, and `--no-web` flags; wire server lifecycle |

---

## 5. Go Backend

### 5.1 New packages

#### `internal/bridge`

A bus client named `"web"` that:

- Creates `eventbus.Subscribe[chat.ChatEvent]` and fans out every event to all connected WebSocket clients.
- Accepts WebSocket messages, unmarshals them into `chat.ChatCommand` interface values, and calls `runtime.Send(cmd)`.
- Tracks connected clients so the TUI status line can show `web: 1 client`.

```go
type Bridge struct {
    runtime chat.ChatRuntime
    sub     *eventbus.Subscriber[chat.ChatEvent]
    mu      sync.RWMutex
    clients map[*Conn]struct{}
}

func NewBridge(runtime chat.ChatRuntime, bus *eventbus.Bus) (*Bridge, error)
func (b *Bridge) HandleWebSocket(ws *websocket.Conn)
func (b *Bridge) ClientCount() int
func (b *Bridge) Close() error
```

`Bridge` does **not** interpret commands; it only translates JSON ↔ Go types. Command validation stays in the coordinator.

#### `internal/server`

A tiny HTTP server:

- `GET /` → serves the embedded SPA (with SPA fallback to `index.html`).
- `GET /ws` → upgrades to WebSocket and hands the connection to `internal/bridge`.
- `GET /health` → returns `{ "ok": true, "session_id": "...", "clients": 0 }`.

The server binds **only** `127.0.0.1` by default.

```go
type Server struct {
    addr    string
    bridge  *bridge.Bridge
    spa     http.Handler
}

func New(addr string, b *bridge.Bridge) *Server
func (s *Server) Start(ctx context.Context) error // blocks until shutdown
```

### 5.2 WebSocket message format

All messages are JSON objects with a `type` field.

**Server → Client (`event`)**

```json
{
  "type": "event",
  "payload": { /* any chat.ChatEvent */ }
}
```

**Client → Server (`command`)**

```json
{
  "type": "command",
  "payload": { /* any chat.ChatCommand */ }
}
```

**Server → Client (`init`)** — sent once on connection:

```json
{
  "type": "init",
  "session_id": "...",
  "model": "deepseek-v4-flash",
  "provider": "deepseek",
  "commands": [ { "name": "/model", "description": "..." } ]
}
```

### 5.3 WebSocket library

Use `github.com/gorilla/websocket` v1.5+. It is stable, supports per-message compression, and is familiar to the team from other projects.

### 5.4 Coordinator changes

No coordinator changes are required. The bridge uses the existing `ChatRuntime` interface and event bus. If we later want the coordinator to know whether any web client is connected (e.g., to skip TUI rendering), we can add a flag, but that is optional.

---

## 6. Vue Frontend

### 6.1 Stack

- **Framework:** Vue 3 (Composition API, `<script setup lang="ts">`)
- **Build tool:** Vite 6
- **Styling:** Tailwind CSS v4 with CSS variables for theming
- **Component primitives:** shadcn-vue (Button, Card, Input, ScrollArea, Badge, Select, etc.)
- **State:** Pinia stores
- **Icons:** lucide-vue / @hugeicons/vue (match spawn)
- **Markdown rendering:** `marked` (already used in spawn)

### 6.2 Source layout

```tree
internal/webui/
├── index.html
├── package.json
├── vite.config.ts
├── src/
│   ├── main.ts
│   ├── App.vue
│   ├── router/
│   │   └── index.ts          # only /chat for MVP
│   ├── layouts/
│   │   └── ChatLayout.vue
│   ├── pages/
│   │   └── ChatPage.vue
│   ├── components/
│   │   ├── ChatMessage.vue
│   │   ├── ToolCard.vue      # expandable tool detail
│   │   ├── ToolGroup.vue     # groups parallel tool calls
│   │   ├── ReasoningPanel.vue
│   │   ├── ChatInput.vue
│   │   ├── ModelSelector.vue
│   │   ├── SettingsDrawer.vue
│   │   └── StatusBar.vue
│   ├── stores/
│   │   ├── session.ts        # session state + runtime events
│   │   ├── config.ts         # provider/model/settings
│   │   └── commands.ts       # slash command registry
│   ├── composables/
│   │   ├── useWebSocket.ts   # ws connect/reconnect/send
│   │   └── useAutoScroll.ts
│   └── lib/
│       └── utils.ts
```

### 6.3 Build output

`vite.config.ts` builds into `internal/spa/dist/`. The Go package `internal/spa` embeds `dist/*`.

### 6.4 UI behavior

- **Chat stream:** Messages append as `ChatEvent` values arrive. Streaming deltas are appended to the active assistant bubble.
- **Tool calls:** Each `ChatToolExecutionStartedEvent` creates a `ToolCard`. Resolved tools animate to success/failed colors from `internal/theme`. Tool cards are **expandable** in the browser (this is where the non-technical-friendly detail view lives).
- **Tool groups:** Parallel tool calls during one turn are visually grouped under a collapsible `ToolGroup`.
- **Input:** Auto-growing textarea. Shift+Enter inserts newline; Enter submits.
- **Settings:** Drawer or inline panel with model selector, reasoning toggle, temperature, max tokens. Changes emit `UpdateChatSessionCommand`.
- **Status bar:** Shows model, provider, web client count, and a copyable localhost URL.

---

## 7. CLI Integration

### 7.1 New flags

Added to `internal/cli/root.go`:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--web` | bool | false | Start the web UI and open the browser |
| `--port` | int | 0 | HTTP port (0 = auto-assign ephemeral) |
| `--no-web` | bool | false | Do not start the web server |

`--web` implies a web server. If `--port 0`, the OS assigns a free port and the URL is printed.

### 7.2 Default behavior

- `tau` → starts TUI + web server on localhost. URL shown in TUI status line and printed on startup.
- `tau --web` → starts TUI + web server, then opens `http://127.0.0.1:<port>` in the default browser.
- `tau --no-web` → starts only the TUI (today’s behavior).
- `tau --prompt "hello"` → one-shot stdin mode; web server is not started.

### 7.3 Browser opening

Use `github.com/pkg/browser` (already in `go.sum` via ai-sdk transitive deps) or a small `xdg-open`/`open` wrapper. Open only when `--web` is explicitly set.

### 7.4 App lifecycle

`internal/app/run.go` currently:

1. builds coordinator
2. starts session
3. runs TUI
4. closes coordinator

Updated flow:

1. build coordinator
2. start session
3. build `bridge.Bridge`
4. if web enabled:
   - pick port
   - start `server.Server` in a goroutine
   - print URL
   - if `--web`, open browser
5. run TUI
6. on TUI exit, shutdown web server, close bridge, close coordinator

---

## 8. Build and Embedding

### 8.1 Taskfile additions

```yaml
  build:webui:
    desc: Build the Tau Web UI SPA
    cmds:
      - pnpm run -C internal/webui build
    sources:
      - internal/webui/src/**/*.vue
      - internal/webui/src/**/*.ts
      - internal/webui/src/**/*.css
      - internal/webui/index.html
      - internal/webui/vite.config.ts
      - internal/webui/package.json
    generates:
      - internal/spa/dist/index.html

  build:
    desc: Build the web UI and Go binary
    deps:
      - build:webui
    cmds:
      - go build -ldflags "-s -w -X main.version={{.VERSION}}" -o tau ./cmd/tau
```

### 8.2 Go embed

```go
//go:embed dist/*
var distDir embed.FS
```

### 8.3 Development

During local development, run the web UI Vite dev server separately and proxy WebSocket to the Go backend:

```bash
cd internal/webui && pnpm dev        # Vite dev server
go run ./cmd/tau --web --port 9343   # Go backend
```

Vite config will proxy `/ws` and `/api` to the Go server.

---

## 9. WebSocket Protocol (AsyncAPI Sketch)

A formal `docs/asyncapi/tau.yaml` will document the protocol. High-level channels:

```yaml
asyncapi: '3.0.0'
info:
  title: Tau Web UI Protocol
  version: '0.1.0'

servers:
  local:
    host: localhost:{port}
    pathname: /ws
    protocol: ws
    description: Local Tau WebSocket endpoint

channels:
  tauEvents:
    address: /ws
    messages:
      event:
        payload:
          type: object
          properties:
            type: { type: string, const: event }
            payload:
              oneOf:
                - $ref: '#/components/messages/ChatSessionSnapshotEvent'
                - $ref: '#/components/messages/ChatResponseDeltaEvent'
                - ...
      command:
        payload:
          type: object
          properties:
            type: { type: string, const: command }
            payload:
              oneOf:
                - $ref: '#/components/messages/SubmitChatPromptCommand'
                - $ref: '#/components/messages/UpdateChatSessionCommand'
                - ...
      init:
        payload:
          type: object
          properties:
            type: { type: string, const: init }
            session_id: { type: string }
            model: { type: string }
            provider: { type: string }
            commands:
              type: array
              items:
                type: object
                properties:
                  name: { type: string }
                  description: { type: string }
```

The AsyncAPI document will be expanded with concrete message schemas once the command/event JSON shapes are stable.

---

## 10. Testing Strategy

| Layer | Test approach |
| ----- | ------------- |
| `internal/bridge` | Unit tests with a mock `ChatRuntime` and fake WebSocket; verify commands forwarded and events broadcast |
| `internal/server` | `httptest` + `websocket` client; verify SPA fallback, `/ws` upgrade, `/health` |
| `internal/app` | Integration test: start `RunChat` with `--no-web`, `--web`, and `--port` flags; assert server binds |
| Vue composables | Vitest unit tests for `useWebSocket` reconnection and message parsing |
| E2E | Playwright test: run `tau --web`, open browser, send a message, assert response appears |

---

## 11. Risks and Mitigations

| Risk | Impact | Mitigation |
| ---- | ------ | ---------- |
| Binary size bloat from embedded SPA | Medium | Code-split SPA; lazy-load routes; only embed `dist/` assets |
| WebSocket reconnection complexity | Medium | Robust `useWebSocket` with exponential backoff; replay last event snapshot on reconnect |
| Concurrent input from TUI and browser | Low | Coordinator serializes `ChatCommand` via its send channel; last-write-wins is acceptable in this phase |
| Port conflicts | Low | Default to ephemeral port (`:0`); print assigned URL |
| Cross-platform browser launch | Low | Use `pkg/browser`; fall back to printing URL |
| Maintenance of two UIs | Medium | Keep web UI a thin view over existing commands/events; no business logic in Vue |

---

## 12. Implementation Phases

### Phase 1: WebSocket bridge + server skeleton

- Add `internal/bridge` package
- Add `internal/server` package
- Add `gorilla/websocket` dependency
- Create `internal/spa` embed package with a placeholder `index.html`
- Add `--port` and `--no-web` flags; wire server lifecycle in `app.RunChat`
- Add `/health` endpoint

**Acceptance:** `tau --no-web` behaves exactly like today. `tau --port 9343` prints a URL and serves a placeholder page.

### Phase 2: Vue chat skeleton

- Scaffold `internal/webui` with Vite + Vue + Tailwind + shadcn-vue
- Implement `useWebSocket` composable
- Implement `ChatPage` with message list and input
- Render markdown messages
- Build into `internal/spa/dist`

**Acceptance:** Browser can connect, send a message, and see the assistant response.

### Phase 3: Tool cards + settings

- Add `ToolCard` and `ToolGroup` components
- Wire tool lifecycle events
- Add `SettingsDrawer` for model/temperature/reasoning
- Add status bar with URL and client count

**Acceptance:** Tool calls render as grouped, expandable cards. Settings changes affect the session.

### Phase 4: Polish + AsyncAPI

- Add `--web` browser auto-open
- Print URL in TUI status line when web is enabled
- Write `docs/asyncapi/tau.yaml`
- Add E2E test

**Acceptance:** `tau --web` launches browser and connects automatically. AsyncAPI spec validates.

---

## 13. Acceptance Criteria

- [ ] `tau` starts a local HTTP server on `127.0.0.1` alongside the TUI.
- [ ] `tau --web` opens the browser to the web UI.
- [ ] `tau --no-web` does not start any server.
- [ ] The web UI connects via WebSocket and receives all `ChatEvent` values.
- [ ] Messages submitted in the browser appear in the TUI and vice versa.
- [ ] Tool calls render as grouped, expandable cards in the browser.
- [ ] Settings changes in the browser emit the same `UpdateChatSessionCommand` as the TUI.
- [ ] WebSocket protocol is documented in `docs/asyncapi/tau.yaml`.
- [ ] All new Go code passes `golangci-lint run` and `go fix ./...`.

---

## 14. Post-MVP Enhancements

Phases 1-4 deliver a working, terminal-paired web UI. The following enhancements
were identified after the first live end-to-end test (browser round-trip against
a real provider). They are grouped by priority. Most are frontend-only because
their events/commands already exist on the wire; the two backend items are
called out explicitly.

### Tier 1 — Foundational gaps

1. **Session history on connect.** A browser that connects mid-session starts
   with an empty stream because the bridge subscribes to the bus *after* the
   coordinator emits its session-start snapshot. *Backend:* the bridge caches the
   most recent `ChatSessionSnapshotEvent` (and last completed state) and replays
   it to each new client immediately after the `init` message; alternatively the
   full `ChatSessionState` is folded into `init`. This also populates the settings
   drawer parameters on first open. *Frontend:* reconcile replayed snapshot
   messages into the store and dedupe against any optimistic local echo.
2. **Sanitise markdown.** `ChatMessage` injects `marked` output via `v-html`;
   model or tool output containing HTML is an XSS vector. Sanitise with DOMPurify
   before rendering. Pair with code-block syntax highlighting.
3. **Stop control.** The store already exposes `cancel()` →
   `CancelChatRequestCommand`; surface a Stop button while `streaming`.

### Tier 2 — TUI feature parity

1. **Reasoning panel.** `ChatReasoningDeltaEvent` is on the wire but unrendered.
   Add a collapsible `ReasoningPanel` that streams reasoning tokens per turn.
2. **Live tool output.** `ChatToolCallDeltaEvent` (streaming arguments) and
   `ChatToolOutputEvent` (live stdout chunks) are currently ignored. Stream them
   into `ToolCard` so output appears as it is produced, not only on completion.
3. **Interactive prompts.** `InteractivePromptRequestedEvent` /
   `RespondInteractivePromptCommand` drive tool confirmations and questions. The
   web UI must render these as a dialog, otherwise an approval-gated tool hangs
   for web users.
4. **Slash commands.** `init` already carries the `commands` (`CommandRef`) list;
   surface a `/`-triggered autocomplete menu in `ChatInput`.

### Tier 3 — Richer UX

1. **Real model selector.** Model is currently a free-text field. *Backend:*
   include the available-models list in `init` (the coordinator already builds
   model refs). *Frontend:* replace the text field with a `ModelSelect` dropdown.
2. **Toast notices.** Notices currently accumulate inline in the stream. Move
   transient info/warn/error to a Sonner-style toast container.
3. **Session switcher.** `ListSessions` / `LoadSession` / `DeleteSession` /
    `ExportSession` commands and their events are already defined; add a sidebar
    to browse, resume, export, and delete sessions.

### Tier 4 — Hardening and reach (future)

- E2E coverage in CI against a stubbed runtime (no provider key required).
- `tau serve` multi-session daemon with token auth — the prerequisite for genuine
  mobile/remote access beyond localhost (see Non-Goals §3.1).
- Mobile polish: safe-area insets, virtual-keyboard handling, responsive header.
