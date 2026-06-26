import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { useWebSocket } from './useWebSocket'
import type { Envelope } from '@/lib/protocol'

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3

  static instances: MockWebSocket[] = []

  readyState = MockWebSocket.CONNECTING
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor(public url: string) {
    MockWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  // Test helpers.
  open() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  emit(data: string) {
    this.onmessage?.({ data })
  }

  static latest() {
    return this.instances[this.instances.length - 1]
  }
}

function run<T>(fn: () => T): { value: T; stop: () => void } {
  const scope = effectScope()
  const value = scope.run(fn) as T
  return { value, stop: () => scope.stop() }
}

describe('useWebSocket', () => {
  beforeEach(() => {
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('connects and transitions to open', () => {
    const { value, stop } = run(() => useWebSocket('/ws', { onMessage: () => {} }))
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(value.status.value).toBe('connecting')

    MockWebSocket.latest().open()
    expect(value.status.value).toBe('open')
    stop()
  })

  it('parses inbound JSON and forwards valid messages only', () => {
    const received: { type: string }[] = []
    const { stop } = run(() =>
      useWebSocket('/ws', { onMessage: (m) => received.push(m as { type: string }) }),
    )
    const ws = MockWebSocket.latest()
    ws.open()

    ws.emit(JSON.stringify({ type: 'init', session_id: 's' }))
    ws.emit('not json')
    ws.emit(JSON.stringify({ no_type: true }))

    expect(received).toHaveLength(1)
    expect(received[0].type).toBe('init')
    stop()
  })

  it('only sends when the socket is open', () => {
    const { value, stop } = run(() => useWebSocket('/ws', { onMessage: () => {} }))
    const ws = MockWebSocket.latest()
    const envelope: Envelope = { type: 'SubmitChatPromptCommand', payload: { prompt: 'hi' } }

    expect(value.send(envelope)).toBe(false)
    expect(ws.sent).toHaveLength(0)

    ws.open()
    expect(value.send(envelope)).toBe(true)
    expect(JSON.parse(ws.sent[0]).type).toBe('SubmitChatPromptCommand')
    stop()
  })

  it('reconnects with backoff after the socket closes', () => {
    vi.useFakeTimers()
    const { stop } = run(() => useWebSocket('/ws', { onMessage: () => {}, baseDelay: 500 }))
    expect(MockWebSocket.instances).toHaveLength(1)

    MockWebSocket.latest().open()
    MockWebSocket.latest().close()

    // Nothing reconnects before the backoff elapses.
    vi.advanceTimersByTime(499)
    expect(MockWebSocket.instances).toHaveLength(1)

    vi.advanceTimersByTime(1)
    expect(MockWebSocket.instances).toHaveLength(2)
    stop()
  })

  it('stops reconnecting once disposed', () => {
    vi.useFakeTimers()
    const { stop } = run(() => useWebSocket('/ws', { onMessage: () => {}, baseDelay: 500 }))
    MockWebSocket.latest().open()
    stop() // dispose the scope -> close()

    MockWebSocket.latest().close()
    vi.advanceTimersByTime(5000)
    // No new socket created after disposal.
    expect(MockWebSocket.instances).toHaveLength(1)
  })
})
