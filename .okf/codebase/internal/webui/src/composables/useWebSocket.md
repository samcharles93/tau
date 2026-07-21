---
description: Source module internal/webui/src/composables/useWebSocket.ts (102 lines).
resource: internal/webui/src/composables/useWebSocket.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: useWebSocket.ts
type: Module
---

# Module useWebSocket.ts

**Path**: `internal/webui/src/composables/useWebSocket.ts`  
**Lines**: 102

## Snippet Preview

```
import { onScopeDispose, ref, shallowRef } from 'vue'
import type { Envelope } from '@/lib/protocol'

export type ConnectionStatus = 'connecting' | 'open' | 'closed'

export interface UseWebSocketOptions {
  /** Called for every parsed inbound message (init + event envelopes). */
  onMessage: (msg: { type: string; [key: string]: unknown }) => void
  /** Base reconnect delay in ms; backs off exponentially up to maxDelay. */
  baseDelay?: number
  maxDelay?: number
}

/**
 * useWebSocket manages a single connection to the Tau bridge with automatic
 * exponential-backoff reconnection. It resolves the ws:// URL from the current
 * page origin so it works behind the Vite dev proxy and when embedded.
 */
export function useWebSocket(path: string, options: UseWebSocketOptions) {
  const { onMessage, baseDelay = 500, maxDelay = 10_000 } = options

  const status = ref<ConnectionStatus>('connecting')
  const socket = shallowRef<WebSocket | null>(null)

  let attempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let disposed = false

  function url(): string {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
```
