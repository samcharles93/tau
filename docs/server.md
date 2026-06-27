# Server & Bridge (Web UI Backend)

The Web UI backend consists of two packages: `internal/server` (HTTP server) and `internal/bridge` (WebSocket fan-out bridge). Together they serve the embedded SPA and relay commands/events between the browser and the coordinator.

## Package: `internal/server`

The HTTP server binds to `127.0.0.1` and serves three routes:

| Route | Method | Purpose |
| ----- | ------ | ------- |
| `/` | GET | Serves the embedded SPA with SPA fallback (all non-`/ws`/`/health` routes serve `index.html`) |
| `/ws` | GET | Upgrades to WebSocket, hands connection to the bridge |
| `/health` | GET | Returns `{ "ok": true, "clients": 0 }` JSON |

### Server Struct

```go
type Server struct {
    addr    string        // bind address (e.g., "127.0.0.1:0" for ephemeral)
    bridge  Bridge        // bridge interface for WS upgrades
    spa     http.Handler  // embedded SPA handler
    logger  *slog.Logger
    httpSrv *http.Server
}
```

### Server Lifecycle

1. **`New(addr, bridge, spa, logger)`** — Creates the server.
2. **`Start(ctx)`** — Binds the listener, starts serving in a background goroutine, returns the bound address. Listens for `ctx.Done()` to trigger graceful shutdown.
3. **`Wait()`** — Blocks until the server has fully shut down.
4. **`URL()`** — Returns the reachable HTTP URL (e.g., `http://127.0.0.1:9343`).

Graceful shutdown sequence:
1. `ctx` is cancelled (via `webCancel` from the app layer).
2. `httpSrv.Shutdown(5s timeout)` stops accepting new connections and drains existing ones.
3. `bridge.Close()` disconnects all WebSocket clients and unsubscribes from the event bus.

### Port Selection

- When `addr` is `"127.0.0.1:0"`, the OS assigns a free ephemeral port.
- When a specific port is given (e.g., `"127.0.0.1:9343"`), that port is used.
- Port-only strings (e.g., `":9343"`) are prefixed with `127.0.0.1`.

### Bridge Interface

```go
type Bridge interface {
    UpgradeHTTP(w http.ResponseWriter, r *http.Request) error
    ClientCount() int
    Close() error
}
```

## Package: `internal/bridge`

The bridge is the WebSocket ↔ event bus adapter. It subscribes to `ChatEvent` on the event bus and fans out every event to all connected WebSocket clients. It also accepts WebSocket messages, unmarshals them into `ChatCommand` values, and forwards them to the coordinator.

### Bridge Struct

```go
type Bridge struct {
    runtime Runtime                              // coordinator (ChatRuntime)
    bus     *eventbus.Bus
    client  *eventbus.Client                     // "web" client
    sub     *eventbus.Subscriber[tauchat.ChatEvent]
    clients map[*client]struct{}                 // connected WebSocket clients
    initData      []byte                         // pre-marshalled init message
    lastSnapshot  []byte                         // cached for new client replay
    upgrader websocket.Upgrader
    logger   *slog.Logger
}
```

### Runtime Interface

```go
type Runtime interface {
    Send(cmd tauchat.ChatCommand) error
    Close()
}
```

### InitInfo

Sent to every browser on connection:

```go
type InitInfo struct {
    SessionID string
    Model     string
    Provider  string
    Models    []tauchat.ChatModelRef
    Providers []string
    Commands  []tauchat.CommandRef
}
```

The `Models` and `Providers` fields enable rich model selection and cross-provider switching in the Web UI.

### Connection Lifecycle

1. **`NewBridge(runtime, bus, init, logger)`** — Creates the bridge, subscribes to `ChatEvent` on the bus, starts the `broadcastLoop()` goroutine.
2. **`UpgradeHTTP(w, r)`** — Handles a WebSocket upgrade request:
   - Upgrades HTTP to WebSocket via `gorilla/websocket`.
   - Creates a `client` with a buffered send channel (64 messages).
   - Sends the `init` message immediately.
   - Replays the cached `lastSnapshot` so the client sees existing history.
   - Enters `readLoop()` to process incoming messages.
   - On return (connection closed), removes the client.
3. **`Close()`** — Closes all client connections, unsubscribes from the bus, waits for goroutines.

### Client Model

Each connected browser has a `client`:

```go
type client struct {
    bridge    *Bridge
    conn      *websocket.Conn
    send      chan []byte     // buffered outbound channel (cap 64)
    closeOnce sync.Once
}
```

- **`readLoop()`** — Reads text messages from the WebSocket, unmarshals them as `ChatCommand`, and calls `runtime.Send(cmd)`. Handles pong frames for keepalive (60s read deadline). On error, returns and the client is removed.
- **`writeLoop()`** — Reads from the `send` channel and writes to the WebSocket. Sends ping frames every 30 seconds for keepalive. Closes the WebSocket when the channel is closed or the bridge is done.
- **`close()`** — Closes the send channel once (via `sync.Once`).

### Event Broadcast

The `broadcastLoop()` goroutine:

1. Receives `ChatEvent` from the bus subscriber.
2. Marshals the event to a JSON envelope via `MarshalEvent()`.
3. If the event is a `ChatSessionSnapshotEvent`, caches it in `lastSnapshot` for replay to new clients.
4. Fans out the marshalled data to all connected clients' send channels (non-blocking — slow clients are warned and dropped).

### Wire Format

The wire format uses JSON envelopes with a `type` discriminator field. See `internal/bridge/wire.go`:

```go
type Envelope struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

**Event serialization** (`MarshalEvent`):
- The event value is first marshalled to JSON as the payload.
- The payload is wrapped in an envelope with the event's concrete type name (e.g., `"ChatResponseDeltaEvent"`).

**Command deserialization** (`UnmarshalCommand`):
- The envelope is parsed to extract the `type` field.
- A switch on the type name unmarshals the payload into the corresponding concrete command struct.

### Event Type Names

The `eventTypeName()` function maps concrete event types to wire names via a type switch. Supported event types:

`ChatSessionSnapshotEvent`, `ChatResponseStartedEvent`, `ChatResponseDeltaEvent`, `ChatReasoningDeltaEvent`, `ChatToolCallDeltaEvent`, `ChatToolExecutionStartedEvent`, `ChatToolOutputEvent`, `ChatToolExecutionCompletedEvent`, `ChatResponseCompletedEvent`, `ChatResponseCancelledEvent`, `ChatRuntimeErrorEvent`, `ChatNotificationEvent`, `ExtensionsReloadedEvent`, `ExtensionCommandsChangedEvent`, `ExtensionCommandResultEvent`, `InteractivePromptRequestedEvent`, `SessionsListedEvent`, `SessionLoadedEvent`, `SessionDeletedEvent`, `SessionExportedEvent`, `CommandsChangedEvent`.

### Command Type Names

`unmarshalCommandPayload()` handles: `StartChatSessionCommand`, `SubmitChatPromptCommand`, `SteerChatPromptCommand`, `UpdateChatSessionCommand`, `CancelChatRequestCommand`, `ResetChatSessionCommand`, `CloseChatSessionCommand`, `ReloadExtensionsCommand`, `RunExtensionCommandCommand`, `RespondInteractivePromptCommand`, `ListSessionsCommand`, `LoadSessionCommand`, `DeleteSessionCommand`, `ExportSessionCommand`.

## App Integration

The server and bridge are wired together in `internal/app/web.go`:

```go
func startWebUI(
    runtime webbridge.Runtime,
    bus *eventbus.Bus,
    opts ChatOptions,
    sessionID, modelID string,
    availableModels []tauchat.ChatModelRef,
    availableProviders []string,
    commands []tauchat.CommandRef,
    logger *slog.Logger,
) (*webServerResult, error)
```

The function:
1. Creates the bridge with `InitInfo` including available models, providers, and commands.
2. Picks the bind address (`127.0.0.1:<port>`).
3. Creates and starts the server with the embedded SPA handler.
4. Returns the server, URL, and shutdown/wait functions.

In `app.RunChat()`, the web UI lifecycle is:

```go
// 1. Start the web UI
webRes, _ := startWebUI(...)
webURL = webRes.URL

// 2. Optionally open browser
if opts.Web { openBrowser(ctx, webURL) }

// 3. Run the TUI (blocks until exit)
tui.Run(ctx, coordinator, tuiCfg)

// 4. Shutdown web UI after TUI exits
webShutdown()
webWait()
```

This ensures the web server stays alive for the entire TUI session and shuts down cleanly when the user quits.
