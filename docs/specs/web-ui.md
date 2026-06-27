# Tau Web UI — Architecture Reference

The web UI is an embedded Vue 3 SPA served over WebSocket. It is a first-class
peer to the TUI: both subscribe to the same `ChatEvent` stream from the
coordinator and send the same `ChatCommand` messages back.

## Overview

```
┌───────────────────────────────────────────────────────────┐
│                       tau process                         │
│                                                           │
│  ┌──────────────┐  ChatCommand   ┌────────────────────┐  │
│  │   TUI        │───────────────▶│  agent.Coordinator │  │
│  │  (terminal)  │◀── ChatEvent ──│  eventbus.Bus      │  │
│  └──────────────┘                │                    │  │
│                                  │                    │  │
│  ┌──────────────┐  ChatCommand   │                    │  │
│  │   Bridge     │───────────────▶│                    │  │
│  │  (internal/  │◀── ChatEvent ──│                    │  │
│  │   bridge)    │                └────────────────────┘  │
│       │ WebSocket                                        │
│       ▼ 127.0.0.1:PORT/ws                               │
│  ┌──────────────┐                                        │
│  │  Browser     │  (one or more tabs)                   │
│  └──────────────┘                                        │
└───────────────────────────────────────────────────────────┘
```

## Go Backend

### `internal/bridge`

`Bridge` is the WebSocket gateway. It:

- Creates a `"web"` bus client and subscribes to `ChatEvent`.
- `broadcastLoop()` fans events out to all connected browser `client`s.
- Caches the most recent `ChatSessionSnapshotEvent` as `lastSnapshot`; replays
  it to every new browser connection so they see existing history immediately.
- `UpgradeHTTP()` upgrades HTTP to WebSocket, sends `initData` + `lastSnapshot`,
  then enters the client's read loop.
- `client.readLoop()` receives JSON commands from the browser and forwards them
  to `bridge.runtime.Send()`.
- Ping/pong keepalives: 30 s ping interval, 60 s read deadline.

```go
// NewBridge(runtime, bus, InitInfo, logger)
// UpgradeHTTP(w, r) — blocks until connection closes
// ClientCount() int
// Close() error
```

### `internal/server`

Thin HTTP server:

- `GET /` → serves embedded SPA (`internal/spa/dist/`) with SPA fallback.
- `GET /ws` → upgrades to WebSocket, delegates to `bridge.UpgradeHTTP()`.
- `GET /health` → `{ "ok": true, "session_id": "...", "clients": 0 }`.

### `internal/spa`

`//go:embed dist/*` bakes the built SPA into the binary. After any frontend
change, run `task webui` before rebuilding the binary.

### `internal/app/web.go`

`startWebUI()` wires everything together:

1. Creates `bridge.NewBridge(coordinator, bus, init, logger)`.
2. Creates `server.New(addr, bridge)`.
3. Starts the server in a goroutine; returns the URL, shutdown func, and wait func.

## WebSocket Wire Protocol

Every message is JSON: `{ "type": "<discriminator>", "payload": { … } }`.

Go types: `internal/bridge/wire.go`
TypeScript types: `internal/webui/src/lib/protocol.ts`

**Both files must be kept in sync.** Adding a new event or command type requires
changes in both.

### Connection init (server → client, once on connect)

```json
{
  "type": "init",
  "session_id": "…",
  "model": "deepseek-chat",
  "provider": "deepseek",
  "models": [ { "id": "…", "provider": "…", "context_window": 128000 } ],
  "providers": [ "deepseek", "openrouter" ],
  "commands": [ { "name": "model", "description": "switch model" } ]
}
```

`models` is the full aggregated cross-provider model list — the same list that
powers the TUI's `/model` picker. The browser uses it to populate the model
dropdown and to look up `provider` when switching models.

### Server → client events

| Type | Trigger |
| ---- | ------- |
| `ChatSessionSnapshotEvent` | Full state snapshot; replayed on connect |
| `ChatResponseDeltaEvent` | Streaming text chunk |
| `ChatReasoningDeltaEvent` | Streaming reasoning chunk |
| `ChatToolCallDeltaEvent` | Tool call argument delta |
| `ChatToolExecutionStartedEvent` | Tool started running |
| `ChatToolExecutionCompletedEvent` | Tool finished |
| `ChatToolOutputEvent` | Live tool stdout/stderr chunk |
| `ChatResponseCompletedEvent` | Turn end + final state |
| `ChatResponseCancelledEvent` | Turn cancelled |
| `ChatRuntimeErrorEvent` | Runtime error |
| `ChatNotificationEvent` | Info / warn / error notification |
| `InteractivePromptRequestedEvent` | Tool confirmation / question dialog |
| `SessionsListedEvent` | Response to ListSessionsCommand |
| `SessionLoadedEvent` | Response to LoadSessionCommand |
| `SessionDeletedEvent` | Response to DeleteSessionCommand |

### Client → server commands

| Type | Effect |
| ---- | ------ |
| `SubmitChatPromptCommand` | Send a user prompt |
| `UpdateChatSessionCommand` | Patch session (model, provider, temperature, etc.) |
| `CancelChatRequestCommand` | Cancel in-flight request |
| `ResetChatSessionCommand` | Clear conversation |
| `ListSessionsCommand` | List saved sessions |
| `LoadSessionCommand` | Load a saved session |
| `DeleteSessionCommand` | Delete a saved session |
| `ExportSessionCommand` | Export a session to JSONL / HTML |
| `RespondInteractivePromptCommand` | Answer a tool dialog |
| `ReloadExtensionsCommand` | Reload plugins |

## Vue Frontend

### Source layout

```
internal/webui/src/
├── lib/
│   └── protocol.ts          ← Wire types (MUST match internal/bridge/wire.go)
├── stores/
│   └── session.ts           ← Pinia store: all mutable UI state
├── composables/
│   ├── useWebSocket.ts      ← WebSocket connection with JSON envelope handling
│   └── useConnection.ts     ← Reconnect logic, bound sender
├── pages/
│   └── ChatPage.vue         ← Root page; feeds inbound events into session store
├── components/
│   ├── SettingsDrawer.vue   ← Model, provider, temperature, reasoning effort
│   ├── ChatMessage.vue      ← Renders ordered text/reasoning/tool parts
│   ├── StatusBar.vue        ← Provider, model, token usage, cost
│   ├── ChatInput.vue        ← Prompt textarea + send / cancel
│   ├── SessionSwitcher.vue  ← Session list / load / delete panel
│   ├── ReasoningPanel.vue   ← Collapsible reasoning content
│   ├── ToolCard.vue         ← Per-tool running/completed display
│   └── ToastContainer.vue   ← Ephemeral notification toasts
└── layouts/
    └── ChatLayout.vue       ← Full-page shell
```

### Session store (`stores/session.ts`)

The Pinia store is the single source of truth for all client state.

**Key flows:**

- `apply(msg)` — inbound event reducer; routes each `type` to state mutations
- `absorbState(state)` — hydrates `model`, `provider`, `parameters`, `usage`
  from an authoritative `ChatSessionState`; rebuilds `messages` from history
  on first connect or reconnect (`pendingResync = true`)
- `updateSettings(patch)` — sends `UpdateChatSessionCommand`; also updates
  `model` / `provider` / `parameters` optimistically before the round-trip

**`DisplayMessage`** uses ordered `parts: MessagePart[]` (`text | reasoning | tool`)
to preserve the model's actual output timeline (reason → call tools → answer).

### Model/provider switching

`SettingsDrawer.vue` → `applyModelById(id)`:

1. Look up `id` in `session.availableModels` (seeded from `init.models`).
2. Build patch: `{ model: { id }, provider: ref.provider }`.
3. `session.updateSettings(patch)` → `UpdateChatSessionCommand` over WebSocket.
4. Backend applies patch, emits `ChatSessionSnapshotEvent`.
5. `absorbState()` updates `model` and `provider`; store reflects the change.

**Always include `provider` when switching models.** Omitting it leaves the
session on the old provider regardless of which model is selected.

## Building

```bash
task webui       # pnpm install + pnpm build → internal/spa/dist/
task             # webui + go build (default)
```

The SPA build output is embedded in the Go binary at compile time. Any
Vue/TypeScript change requires `task webui` before `go build` to take effect.

## Development Workflow

```bash
# Terminal 1: Go backend
go run ./cmd/tau --web --port 9343

# Terminal 2: Vue dev server (hot reload against the running backend)
cd internal/webui && pnpm dev
```

Vite proxies `/ws` to the Go backend, so the dev server gets live events while
you iterate on Vue components.
