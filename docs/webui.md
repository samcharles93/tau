# Web UI

The Tau Web UI is a Vue 3 single-page application (SPA) embedded in the Go binary. It provides a browser-based chat interface that mirrors the TUI's functionality using the same command/event contract over a WebSocket.

## Stack

- **Framework:** Vue 3 (Composition API, `<script setup lang="ts">`)
- **Build tool:** Vite 6
- **Styling:** Tailwind CSS v4 with CSS variables for theming
- **Component primitives:** shadcn-vue (Button, Card, Input, Select, Badge, Sheet, Tooltip, Collapsible, Separator)
- **State management:** Pinia
- **Routing:** Vue Router (hash mode, single `/chat` route)
- **Icons:** lucide-vue
- **Markdown:** `marked` with DOMPurify sanitization

## Source Layout

```
internal/webui/
├── index.html                    # HTML shell
├── package.json                  # Dependencies
├── vite.config.ts                # Vite configuration
├── tsconfig.json                 # TypeScript configuration
├── components.json               # shadcn-vue configuration
├── pnpm-workspace.yaml           # pnpm workspace
└── src/
    ├── main.ts                   # Vue app bootstrap
    ├── App.vue                   # Root component
    ├── router/index.ts           # Hash router (single /chat route)
    ├── vite-env.d.ts             # Vite type declarations
    ├── assets/index.css          # Global styles + Tailwind
    ├── lib/
    │   ├── protocol.ts           # Wire protocol types (mirrors Go types)
    │   ├── markdown.ts           # Markdown rendering with sanitization
    │   └── utils.ts              # Utility functions (cn() classnames)
    ├── stores/
    │   └── session.ts            # Pinia store — session state + event handling
    ├── composables/
    │   ├── useWebSocket.ts       # WebSocket connection with auto-reconnect
    │   ├── useConnection.ts      # Connection status wrapper
    │   └── useAutoScroll.ts      # Auto-scroll chat to bottom
    ├── layouts/
    │   └── ChatLayout.vue        # Main layout shell
    ├── pages/
    │   └── ChatPage.vue          # Main chat page
    └── components/
        ├── ChatMessage.vue       # Single message (user or assistant)
        ├── ChatInput.vue         # Auto-growing textarea with submit
        ├── ToolCard.vue          # Expandable tool call card
        ├── ToolGroup.vue         # Groups parallel tool calls
        ├── ReasoningPanel.vue    # Collapsible reasoning content
        ├── SettingsDrawer.vue    # Model/temperature/reasoning settings
        ├── StatusBar.vue         # Model, provider, client count, URL
        ├── InteractivePromptDialog.vue  # Tool confirmation/input dialog
        ├── SessionSwitcher.vue   # Session list + load/delete/export
        ├── ToastContainer.vue    # Transient notification toasts
        ├── UsageIndicator.vue    # Token usage and cost display
        └── ui/                   # shadcn-vue primitives
            ├── button/
            ├── card/
            ├── badge/
            ├── input/
            ├── label/
            ├── select/
            ├── separator/
            ├── tooltip/
            ├── collapsible/
            └── sheet/
```

## State Management (Pinia Store)

The `useSessionStore` (`stores/session.ts`) is the single source of truth for all UI state. It holds:

### State Fields

| Field | Type | Purpose |
| ----- | ---- | ------- |
| `sessionId` | `string` | Current session UUID |
| `model` | `string` | Active model ID |
| `provider` | `string` | Active provider name |
| `providers` | `string[]` | Available provider names |
| `commands` | `CommandRef[]` | Registry commands for autocomplete |
| `availableModels` | `ChatModelRef[]` | Models for model selector |
| `messages` | `DisplayMessage[]` | Rendered message timeline |
| `notices` | `Notice[]` | Toast notifications |
| `streaming` | `boolean` | Whether a response is in flight |
| `activeRequestId` | `string` | Current request ID for cancellation |
| `status` | `string` | Session status |
| `parameters` | `ChatParameters` | Current session parameters |
| `activePrompt` | `InteractivePrompt \| null` | Active confirmation/input dialog |
| `sessions` | `SessionSummary[]` | Saved session list |
| `usage` | `ChatUsage` | Token usage for input bar display |
| `contextWindow` | `number` | Model context window size |
| `cost` | `ChatCost` | Per-1M-token pricing |

### Store Actions

| Action | Purpose |
| ------ | ------- |
| `bindSender(fn)` | Bind the WebSocket send function |
| `apply(msg)` | Reduce a single wire message into store state |
| `submitPrompt(text)` | Submit user prompt (optimistic render + send) |
| `cancel()` | Cancel in-flight request |
| `updateSettings(patch)` | Emit session settings update |
| `respondPrompt(opts)` | Answer interactive prompt |
| `listSessions(limit)` | Request session list |
| `loadSession(id)` | Load a saved session |
| `deleteSession(id)` | Delete a saved session (optimistic removal) |
| `exportSession(id, format)` | Export session as JSONL |
| `dismissNotice(id)` | Remove a toast notification |

### Message Timeline Model

Messages use an ordered `parts[]` array for accurate timeline rendering:

```typescript
type MessagePart =
  | { kind: 'text'; id: string; text: string }
  | { kind: 'reasoning'; id: string; text: string }
  | { kind: 'tool'; id: string; tool: ToolCall }

interface DisplayMessage {
  id: string
  role: 'user' | 'assistant'
  parts: MessagePart[]
  streaming: boolean
}
```

This preserves the model's actual order (reason → call tools → read results → answer) rather than collapsing everything into fixed buckets.

### Session Hydration

When a browser connects mid-session, the bridge replays the most recent `ChatSessionSnapshotEvent`. The store's `absorbState()` function checks if `messages` is empty (fresh connect) or if `pendingResync` is set (reconnect) and rebuilds the message timeline from the snapshot's `state.messages` history.

### Reconnect Recovery

On WebSocket reconnection:
1. A second `init` message sets `pendingResync = true`.
2. The next `ChatSessionSnapshotEvent` triggers a full state rebuild via `absorbState()`.
3. This recovers any events missed while disconnected.

## Composables

### useWebSocket

`useWebSocket(path, options)` manages a single WebSocket connection with automatic exponential-backoff reconnection:

- Resolves `ws://` URL from the current page origin (works behind Vite dev proxy and when embedded).
- On connection: resets attempt counter, sets status to `"open"`.
- On message: parses JSON, calls `onMessage` callback if `type` field is present.
- On close: schedules reconnect with exponential backoff (base: 500ms, max: 10s).
- `send(envelope)` returns `false` if not connected (caller can show error toast).
- `close()` disposes the connection and cancels reconnect timers.

### useConnection

Wraps `useWebSocket` with connection status tracking. Provides `status` ref (connecting/open/closed) for UI display.

### useAutoScroll

Auto-scrolls the chat container to the bottom when new messages arrive. Pauses auto-scroll if the user has scrolled up.

## Components

### ChatMessage

Renders a single message in the timeline:
- **User messages:** Right-aligned with a user avatar, plain text content.
- **Assistant messages:** Left-aligned with an assistant avatar. Renders `parts[]` in order:
  - `reasoning` parts → `ReasoningPanel` (collapsible, dimmed)
  - `tool` parts → `ToolGroup` wrapping `ToolCard` instances
  - `text` parts → Rendered markdown via `marked` + DOMPurify
- Streaming indicator (animated dots) shown while `streaming` is true.

### ChatInput

Auto-growing textarea:
- Enter submits; Shift+Enter inserts newline.
- Model selector dropdown and usage indicator above the input.
- Stop button visible while streaming.
- Disabled state when not connected.

### ToolCard

Expandable card showing a single tool call:
- **Header:** Tool name + status badge (running/spinning, ok/check, error/x).
- **Collapsed:** Truncated arguments summary.
- **Expanded:** Full arguments, live output stream, result summary.
- Color-coded: green for success, red for error, amber for running.

### ToolGroup

Collapsible group wrapping all tool calls from a single turn:
- Header: "N tools" with expand/collapse toggle.
- Children: `ToolCard` instances.

### ReasoningPanel

Collapsible panel for reasoning content:
- Header: "Reasoning" with expand/collapse toggle.
- Body: Rendered markdown in dimmed text.
- Auto-expands while streaming; collapsible once complete.

### SettingsDrawer

Slide-out drawer with session settings:
- Model selector (dropdown with all available models, grouped by provider).
- Temperature slider (0.0–2.0).
- Max tokens input.
- Reasoning effort selector (low/medium/high).
- Changes emit `UpdateChatSessionCommand` immediately.

### InteractivePromptDialog

Modal dialog for tool confirmations and input:
- `confirm` kind: Title + message + Confirm/Cancel buttons.
- `question` kind: Title + message + text input + Submit/Cancel buttons.
- Response sent as `RespondInteractivePromptCommand`.

### SessionSwitcher

Sidebar for session management:
- Lists saved sessions with model, message count, date.
- Load, delete, and export actions per session.
- Refresh button to reload the list.

### StatusBar

Bottom bar showing:
- Connected provider and model name.
- WebSocket connection status indicator (green/yellow/red dot).
- Copyable localhost URL.
- Client count (when >1 browser connected).

### ToastContainer

Fixed-position toast container for transient notifications:
- Info/warn/error severity levels with icons.
- Auto-dismiss after configurable duration.
- Manual dismiss on click.

### UsageIndicator

Compact token usage display:
- Current turn tokens / context window capacity (progress bar).
- Estimated cost based on model pricing.

## Wire Protocol

The WebSocket protocol uses JSON envelopes with a `type` discriminator:

```typescript
// Server → Client
{ type: "init", session_id, model, provider, models, providers, commands }
{ type: "ChatSessionSnapshotEvent", payload: { state: ChatSessionState } }
{ type: "ChatResponseDeltaEvent", payload: { session_id, request_id, delta, snapshot } }
{ type: "ChatReasoningDeltaEvent", payload: { session_id, request_id, delta, snapshot } }
{ type: "ChatToolCallDeltaEvent", payload: { ... } }
{ type: "ChatToolExecutionStartedEvent", payload: { ... } }
{ type: "ChatToolOutputEvent", payload: { ... } }
{ type: "ChatToolExecutionCompletedEvent", payload: { ... } }
{ type: "ChatResponseCompletedEvent", payload: { ... } }
{ type: "ChatRuntimeErrorEvent", payload: { ... } }
{ type: "ChatNotificationEvent", payload: { ... } }
{ type: "InteractivePromptRequestedEvent", payload: { ... } }
{ type: "SessionsListedEvent", payload: { sessions } }
{ type: "SessionLoadedEvent", payload: { state } }
{ type: "SessionDeletedEvent", payload: { session_id } }

// Client → Server
{ type: "SubmitChatPromptCommand", payload: { session_id, request_id, prompt } }
{ type: "CancelChatRequestCommand", payload: { session_id, request_id } }
{ type: "UpdateChatSessionCommand", payload: { session_id, patch } }
{ type: "RespondInteractivePromptCommand", payload: { request_id, confirmed, response } }
{ type: "ListSessionsCommand", payload: { limit } }
{ type: "LoadSessionCommand", payload: { session_id } }
{ type: "DeleteSessionCommand", payload: { session_id } }
{ type: "ExportSessionCommand", payload: { session_id, format } }
```

The TypeScript types are defined in `src/lib/protocol.ts` and mirror the Go types in `internal/chat/types.go`.

## Build and Embedding

The SPA is built with Vite into `internal/spa/dist/` and embedded in the Go binary:

```go
// internal/spa/spa.go
//go:embed dist/*
var distDir embed.FS
```

The `all` task builds the SPA and the Go binary in one step:

```bash
task all  # pnpm install + pnpm build + go build -o tau
```

See `Taskfile.yaml` in the project root for the exact build commands.

## Development

During development, the Vite dev server runs separately and proxies WebSocket to the Go backend:

```bash
# Terminal 1: Vite dev server
cd internal/webui && pnpm dev

# Terminal 2: Go backend
go run ./cmd/tau --web --port 9343
```

The Vite config proxies `/ws` to the Go server, so hot module replacement works for the Vue app while the WebSocket connects to the real backend.
