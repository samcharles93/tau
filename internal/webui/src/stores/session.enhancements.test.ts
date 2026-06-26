import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSessionStore } from './session'
import type { Envelope } from '@/lib/protocol'

function event(type: string, payload: unknown) {
  return { type, payload } as { type: string; [k: string]: unknown }
}

describe('session store — T2/T3 enhancements', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('streams reasoning into the active assistant turn', () => {
    const s = useSessionStore()
    s.apply(event('ChatReasoningDeltaEvent', { delta: 'think', snapshot: 'think' }))
    s.apply(event('ChatReasoningDeltaEvent', { delta: 'ing', snapshot: 'thinking' }))
    expect(s.messages).toHaveLength(1)
    expect(s.messages[0].reasoning).toBe('thinking')
  })

  it('streams live tool output by call id', () => {
    const s = useSessionStore()
    s.apply(event('ChatToolExecutionStartedEvent', { call_id: 'c1', tool_name: 'bash', arguments_summary: 'ls' }))
    s.apply(event('ChatToolOutputEvent', { call_id: 'c1', chunk: 'line1\n' }))
    s.apply(event('ChatToolOutputEvent', { call_id: 'c1', chunk: 'line2\n' }))
    expect(s.messages[0].tools[0].output).toBe('line1\nline2\n')
  })

  it('upserts a tool from a tool-call delta before execution starts', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { delta: 'x', snapshot: 'x' }))
    s.apply(event('ChatToolCallDeltaEvent', { call_id: 'c1', index: 0, tool_name: 'read', arguments_summary: 'pa' }))
    s.apply(event('ChatToolCallDeltaEvent', { call_id: 'c1', index: 0, tool_name: 'read', arguments_summary: 'path=x' }))
    expect(s.messages[0].tools).toHaveLength(1)
    expect(s.messages[0].tools[0].argumentsSummary).toBe('path=x')
  })

  it('captures an interactive prompt and responds, clearing it', () => {
    const s = useSessionStore()
    const sent: Envelope[] = []
    s.bindSender((e) => {
      sent.push(e)
      return true
    })

    s.apply(
      event('InteractivePromptRequestedEvent', {
        request_id: 'p1',
        kind: 'confirm',
        title: 'Run?',
        message: 'allow tool',
      }),
    )
    expect(s.activePrompt?.requestId).toBe('p1')

    s.respondPrompt({ confirmed: true })
    expect(s.activePrompt).toBeNull()
    expect(sent[0].type).toBe('RespondInteractivePromptCommand')
    const payload = sent[0].payload as { request_id: string; confirmed: boolean }
    expect(payload.request_id).toBe('p1')
    expect(payload.confirmed).toBe(true)
  })

  it('stores listed sessions and resets the stream on session load', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { delta: 'old', snapshot: 'old' }))
    expect(s.messages).toHaveLength(1)

    s.apply(event('SessionsListedEvent', { sessions: [{ id: 'a', message_count: 2 }, { id: 'b', message_count: 0 }] }))
    expect(s.sessions).toHaveLength(2)

    s.apply(
      event('SessionLoadedEvent', {
        state: {
          session_id: 'b',
          model: { id: 'm' },
          parameters: { max_tokens: 1, temperature: 0 },
          status: 'idle',
          messages: [{ role: 'user', content: 'hi from b' }],
        },
      }),
    )
    expect(s.sessionId).toBe('b')
    expect(s.messages).toHaveLength(1)
    expect(s.messages[0].content).toBe('hi from b')
  })

  it('reads advertised models from init', () => {
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 's', model: 'a', provider: 'p', models: ['a', 'b', 'c'] })
    expect(s.availableModels).toEqual(['a', 'b', 'c'])
  })
})
