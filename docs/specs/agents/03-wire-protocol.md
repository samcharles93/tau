# Wire Protocol

The agent wire extends the existing bridge envelope (`internal/bridge/wire.go`), which already wraps every `chat.ChatEvent` and `chat.ChatCommand` in a discriminated JSON envelope and generates `docs/asyncapi/tau.yaml` via `tools/specgen`. Agent messages join that registry: one authoritative catalogue, one generated spec, browser clients and agent processes decoding the same shapes.

## Envelope

```json
{
  "type":    "agent.assign",
  "from":    "tau#8q2mfe",        // sender instance address
  "to":      "research#k3v9qp",   // recipient instance address
  "payload": { ... }
}
```

`from`/`to` are new optional fields on the existing envelope. Browser-bound messages simply omit them, so current WebUI clients are unaffected. The envelope is peer-shaped from day one: nothing about it assumes parent/child, only sender and recipient. Tree topology is a property of who spawns whom, not of the protocol.

## Transport v1: stdio JSONL

- Parent spawns the child with the control channel on the child's stdin/stdout. stderr is reserved for uncaught panics and goes to the parent's log, never parsed.
- One envelope per line, UTF-8, LF-terminated. Maximum line size 8 MiB; anything bigger belongs in the store, not on the wire (data-plane rule, see 04).
- The child writes `agent.ready` as its first line; the parent replies with `agent.assign`. Anything else first is a protocol error and the parent kills the spawn.
- stdin EOF at the child means the parent is gone or has given up: persist and exit (see lifecycle table in 02).
- stdout EOF at the parent without a preceding `agent.result` means the child died: synthesise `failed`.

Future transports (Unix socket per instance for attach, TCP/WebSocket with discovery for cross-machine) carry the identical envelope. They are deferred; patterns to lift when they arrive: p2pchat's channel event loop and mDNS discovery, nell-engine's peer heartbeat/state tracking.

## Message catalogue

### `agent.ready` (child → parent)

First message on the wire.

```json
{ "instance": "research#k3v9qp", "pid": 41172, "protocol": 1 }
```

`protocol` is the agent-wire version, bumped on breaking change. A parent seeing an unknown version cancels the spawn with a clear error (version skew is possible when the binary updates mid-session).

### `agent.assign` (parent → child)

The task. Everything the child needs beyond what it loads from the store.

```json
{
  "task_id":     "t-01",
  "instance_id": "research#k3v9qp",
  "session_id":  "<child session id>",
  "prompt":      "...",
  "context":     "...",              // optional; rendered into a <parent_context> block
  "model":       { "provider": "...", "model": "..." },   // resolved pair, never a tier
  "tools":       ["read", "grep", "glob"],                 // effective set after attenuation; null = unrestricted
  "depth":       1,
  "max_depth":   2,
  "limits":      { "max_turns": 30, "timeout": "10m" },
  "budget":      { "max_tokens": 200000, "deadline": "5m" }   // optional fields
}
```

The child validates that `instance_id`/`session_id` exist in the store and match; a mismatch is a fatal protocol error.

### `agent.event` (child → parent)

Every ChatEvent the child's coordinator publishes, wrapped and forwarded live. The parent re-publishes these onto its own bus scoped to the spawning tool call, which is how both UIs render the child state block and drill-down (see 05).

```json
{ "instance": "research#k3v9qp", "event": { "type": "ChatToolExecutionStartedEvent", "payload": { ... } } }
```

The inner object reuses the bridge's existing event envelope and type registry verbatim.

### `agent.usage` (child → parent)

After every completed turn.

```json
{ "instance": "research#k3v9qp", "turns": 3, "input_tokens": 18234, "output_tokens": 2011, "cost": 0.0142 }
```

Cumulative totals, not deltas, so a dropped message never corrupts accounting.

### `agent.cancel` (parent → child)

```json
{ "task_id": "t-01", "reason": "user_cancelled", "grace_ms": 5000 }
```

### `agent.result` (child → parent)

Terminal message; the child exits after flushing it.

```json
{
  "task_id":    "t-01",
  "status":     "completed",
  "final_text": "...",
  "session_id": "<child session id>",
  "usage":      { "turns": 7, "input_tokens": 0, "output_tokens": 0, "cost": 0.0 },
  "error":      null,
  "partial":    false
}
```

## Ordering and delivery guarantees

- The wire is a pipe: ordered, lossless, no acks needed in v1.
- `agent.event` and `agent.usage` may interleave; `agent.result` is always last from the child.
- The parent must tolerate a result arriving with no prior events (instant tasks) and events with no result (crash).

## Versioning and spec generation

- Agent message types register alongside the existing `EventTypes`/command registries in `internal/bridge` so `tools/specgen` emits them into `docs/asyncapi/tau.yaml`. The AsyncAPI document remains the published contract.
- The `protocol` integer in `agent.ready` gates structural changes to the agent handshake itself; additive payload fields do not bump it.