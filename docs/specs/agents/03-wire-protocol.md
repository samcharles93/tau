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

## Parent-child authority validation

### Pipe binding

When the parent spawns a child, it creates a pipe binding that associates the stdio file descriptors with the spawned identity:

```go
type PipeBinding struct {
    InstanceID  string   // child's full instance address ("research#k3v9qp")
    TaskID      string   // the single task this pipe carries ("t-01")
    SessionID   string   // child session ID, set in agent.assign
    From        string   // parent instance address (for child-side validation)
    To          string   // child instance address (redundant with InstanceID; belt-and-braces)
    SpawnedAt   time.Time
}
```

The binding is created atomically with the instance row (see 04, transaction boundary). It lives in memory on the parent side for the lifetime of the child process. It is never serialised — it is a runtime guard, not persisted state.

### Parent-side validation (inbound from child)

Every envelope the parent reads from the child's stdout is validated against the pipe binding **before its payload is deserialised or dispatched**:

| Field | Rule | Violation behavior |
|---|---|---|
| `from` | Must equal `binding.To` (the child's instance address) | Reject envelope, log at WARN, increment `protocol_violations` counter |
| `to` | Must equal `binding.From` (the parent's instance address) | Reject envelope, log at WARN |
| `instance_id` (in payload) | Must equal `binding.InstanceID` | Reject envelope, log at ERROR (the child is asserting a false identity) |
| `task_id` (in payload) | Must equal `binding.TaskID` | Reject envelope, log at WARN |
| `session_id` (in payload) | Must equal `binding.SessionID` | Reject envelope, log at WARN |
| Message type | Must be a known child→parent type (`agent.ready`, `agent.event`, `agent.usage`, `agent.result`) | Reject envelope, log at WARN |

A rejected envelope is dropped. The parent does **not** kill the child on a single violation — a buggy plugin may inject a stray print to stdout. The parent counts violations and closes the pipe (treats the child as crashed) if the count exceeds a configurable threshold (default: 3).

### Child-side validation (inbound from parent)

The child validates `agent.assign` on receipt:

| Field | Rule | Violation behavior |
|---|---|---|
| `instance_id` | Must match the instance ID the child was started with (passed via CLI flag or env var) | Fatal: log, exit 1 |
| `session_id` | Must exist in the store and its `agent_instance_id` must match | Fatal: log, exit 1 |
| Message order | First message must be `agent.assign`; never a second `agent.assign` or `agent.cancel` after `agent.result` | Fatal protocol error: log, persist if possible, exit 1 |

### Nested event forwarding

When a child forwards events from its own children (nested `agent.event` envelopes), the intermediate parent must:

1. **Allowlist** the nested event type against a known set of forwardable types. Only `ChatEvent` variants produced by the coordinator are forwardable; internal protocol messages (`agent.ready`, `agent.assign`, `agent.cancel`) are never forwarded.
2. **Re-attribute** the `from` and `to` fields of the nested envelope: the parent replaces the original `from` with its own instance address and the original `to` with the grandparent's address. This prevents spoofing of identity in nested event streams.
3. **Scope** the re-attributed event to the tool call that spawned the intermediate child, so the grandparent's UI renders it under the correct tree node.

A nested event with an unregistered or non-forwardable type is dropped with a logged warning. The intermediate parent does not propagate rejection upward — a single bad nested event does not poison the parent-child pipe.

### Rejection, logging, and result synthesis

| Scenario | Log level | Parent action | Child-visible outcome |
|---|---|---|---|
| Single `from`/`to` mismatch | WARN | Drop envelope, increment counter | Child continues (envelope silently dropped) |
| Repeated violations (≥3) | ERROR | Close pipe, synthesise `agent.result` with `status: failed` and reason | Child sees stdin EOF → persist-and-exit (02) |
| Nested event with invalid type | WARN | Drop the nested event only | No propagation; parent-child pipe unaffected |
| Unknown message type | WARN | Drop envelope, increment counter | Same as `from`/`to` mismatch |

Result synthesis on violation-triggered pipe closure: the parent emits a `ChatToolExecutionCompletedEvent` for the spawn tool call with `status: failed` and a structured `failure_reason` including the violation count and the last rejected field. The child's session retains whatever was persisted before the pipe was closed.

## Ordering and delivery guarantees

- The wire is a pipe: ordered, lossless, no acks needed in v1.
- `agent.event` and `agent.usage` may interleave; `agent.result` is always last from the child.
- The parent must tolerate a result arriving with no prior events (instant tasks) and events with no result (crash).

## Versioning and spec generation

- Agent message types register alongside the existing `EventTypes`/command registries in `internal/bridge` so `tools/specgen` emits them into `docs/asyncapi/tau.yaml`. The AsyncAPI document remains the published contract.
- The `protocol` integer in `agent.ready` gates structural changes to the agent handshake itself; additive payload fields do not bump it.