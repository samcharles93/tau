---
description: Source module internal/webui/src/lib/protocol.ts (421 lines).
resource: internal/webui/src/lib/protocol.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: protocol.ts
type: Module
---

# Module protocol.ts

**Path**: `internal/webui/src/lib/protocol.ts`  
**Lines**: 421

## Snippet Preview

```
/**
 * Wire protocol shared with the Go backend (internal/bridge/wire.go).
 *
 * Every WebSocket message is a JSON object with a `type` discriminator. Events
 * (server -> client) and commands (client -> server) wrap their payload in an
 * { type, payload } envelope. The `init` message is sent once on connect.
 *
 * Field names mirror the JSON tags on internal/chat types exactly.
 */

// ── Envelopes ──────────────────────────────────────────────────────────────

export interface Envelope<T = unknown> {
  type: string
  payload: T
}

export interface SkillInfo {
  name: string
  description: string
  scope: string
}

export interface InitMessage {
  type: 'init'
  session_id: string
  model: string
  provider: string
  /** Available model refs (id + config), if the backend advertises them. */
  models?: ChatModelRef[]
```
