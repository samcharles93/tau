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
    return `${proto}//${location.host}${path}`
  }

  function scheduleReconnect() {
    if (disposed) return
    const delay = Math.min(baseDelay * 2 ** attempts, maxDelay)
    attempts += 1
    reconnectTimer = setTimeout(connect, delay)
  }

  function connect() {
    if (disposed) return
    status.value = 'connecting'

    let ws: WebSocket
    try {
      ws = new WebSocket(url())
    } catch {
      scheduleReconnect()
      return
    }
    socket.value = ws

    ws.onopen = () => {
      attempts = 0
      status.value = 'open'
    }

    ws.onmessage = (ev) => {
      let parsed: { type: string; [key: string]: unknown }
      try {
        parsed = JSON.parse(ev.data as string)
      } catch {
        return
      }
      if (parsed && typeof parsed.type === 'string') {
        onMessage(parsed)
      }
    }

    ws.onclose = () => {
      status.value = 'closed'
      socket.value = null
      scheduleReconnect()
    }

    ws.onerror = () => {
      // onclose fires after onerror; reconnect is handled there.
      ws.close()
    }
  }

  /** Send a JSON envelope to the server. Returns false if not connected. */
  function send(envelope: Envelope): boolean {
    const ws = socket.value
    if (!ws || ws.readyState !== WebSocket.OPEN) return false
    ws.send(JSON.stringify(envelope))
    return true
  }

  function close() {
    disposed = true
    if (reconnectTimer) clearTimeout(reconnectTimer)
    socket.value?.close()
    socket.value = null
  }

  connect()
  onScopeDispose(close)

  return { status, send, close }
}
