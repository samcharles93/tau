import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { messageReasoning, messageText, messageTools, useSessionStore } from './session'
import type { Envelope } from '@/lib/protocol'

function event(type: string, payload: unknown) {
  return { type, payload } as { type: string; [k: string]: unknown }
}

describe('session store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('applies the init message', () => {
    const s = useSessionStore()
    s.apply({
      type: 'init',
      session_id: 'sess-1',
      model: 'deepseek-v4-flash',
      provider: 'deepseek',
      commands: [{ name: '/model', label: 'model' }],
    })
    expect(s.sessionId).toBe('sess-1')
    expect(s.model).toBe('deepseek-v4-flash')
    expect(s.provider).toBe('deepseek')
    expect(s.commands).toHaveLength(1)
  })

  it('streams an assistant message from response deltas', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { session_id: 's', request_id: 'r', delta: 'Hel', snapshot: 'Hel' }))
    s.apply(event('ChatResponseDeltaEvent', { session_id: 's', request_id: 'r', delta: 'lo', snapshot: 'Hello' }))

    expect(s.messages).toHaveLength(1)
    expect(s.messages[0].role).toBe('assistant')
    expect(messageText(s.messages[0])).toBe('Hello')
    expect(s.messages[0].streaming).toBe(true)
    expect(s.streaming).toBe(true)
  })

  it('tracks tool lifecycle within the active assistant turn', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { session_id: 's', request_id: 'r', delta: 'working', snapshot: 'working' }))
    s.apply(
      event('ChatToolExecutionStartedEvent', {
        call_id: 'c1',
        tool_name: 'read_file',
        arguments_summary: 'path=x',
      }),
    )
    expect(messageTools(s.messages[0])).toHaveLength(1)
    expect(messageTools(s.messages[0])[0].status).toBe('running')

    s.apply(
      event('ChatToolExecutionCompletedEvent', {
        call_id: 'c1',
        tool_name: 'read_file',
        is_error: false,
        result_summary: 'ok',
      }),
    )
    expect(messageTools(s.messages[0])[0].status).toBe('ok')
    expect(messageTools(s.messages[0])[0].resultSummary).toBe('ok')
  })

  it('marks failed tools as error', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { session_id: 's', request_id: 'r', delta: 'x', snapshot: 'x' }))
    s.apply(event('ChatToolExecutionStartedEvent', { call_id: 'c1', tool_name: 'bash', arguments_summary: '' }))
    s.apply(event('ChatToolExecutionCompletedEvent', { call_id: 'c1', tool_name: 'bash', is_error: true, result_summary: 'boom' }))
    expect(messageTools(s.messages[0])[0].status).toBe('error')
  })

  it('finalises the turn and absorbs parameters on completion', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { session_id: 's', request_id: 'r', delta: 'done', snapshot: 'done' }))
    s.apply(
      event('ChatResponseCompletedEvent', {
        request_id: 'r',
        finish_reason: 'stop',
        state: {
          session_id: 's',
          model: { id: 'gpt-4o' },
          parameters: { max_tokens: 4096, temperature: 0.7, reasoning_effort: 'medium' },
          status: 'idle',
          messages: [{ role: 'assistant', content: 'done' }],
        },
      }),
    )
    expect(s.messages[0].streaming).toBe(false)
    expect(s.streaming).toBe(false)
    expect(s.parameters.max_tokens).toBe(4096)
    expect(s.parameters.temperature).toBe(0.7)
    expect(s.parameters.reasoning_effort).toBe('medium')
    expect(s.status).toBe('idle')
  })

  it('absorbs model, params and status from a snapshot', () => {
    const s = useSessionStore()
    s.apply(
      event('ChatSessionSnapshotEvent', {
        state: {
          session_id: 'snap',
          model: { id: 'claude-opus' },
          parameters: { max_tokens: 8192, temperature: 1, reasoning_effort: '' },
          status: 'streaming',
          messages: [],
        },
      }),
    )
    expect(s.sessionId).toBe('snap')
    expect(s.model).toBe('claude-opus')
    expect(s.parameters.max_tokens).toBe(8192)
    expect(s.status).toBe('streaming')
  })

  it('records notifications and runtime errors as notices', () => {
    const s = useSessionStore()
    s.apply(event('ChatNotificationEvent', { message: 'heads up', level: 'warn' }))
    s.apply(event('ChatRuntimeErrorEvent', { message: 'kaboom', fatal: false }))
    expect(s.notices).toHaveLength(2)
    expect(s.notices[0].level).toBe('warn')
    expect(s.notices[1].level).toBe('error')
    expect(s.notices[1].message).toBe('kaboom')
  })

  it('submits a prompt with an optimistic user message and correct envelope', () => {
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 'sess-1', model: 'm', provider: 'p' })

    const sent: Envelope[] = []
    s.bindSender((e) => {
      sent.push(e)
      return true
    })

    const ok = s.submitPrompt('  hello world  ')
    expect(ok).toBe(true)
    expect(s.messages).toHaveLength(1)
    expect(s.messages[0].role).toBe('user')
    expect(messageText(s.messages[0])).toBe('hello world')

    expect(sent).toHaveLength(1)
    expect(sent[0].type).toBe('SubmitChatPromptCommand')
    const payload = sent[0].payload as { session_id: string; prompt: string; request_id: string }
    expect(payload.session_id).toBe('sess-1')
    expect(payload.prompt).toBe('hello world')
    expect(payload.request_id).toBeTruthy()
  })

  it('ignores empty prompts', () => {
    const s = useSessionStore()
    s.bindSender(() => true)
    expect(s.submitPrompt('   ')).toBe(false)
    expect(s.messages).toHaveLength(0)
  })

  it('emits UpdateChatSessionCommand and reflects settings optimistically', () => {
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 'sess-1', model: 'old', provider: 'p' })

    const sent: Envelope[] = []
    s.bindSender((e) => {
      sent.push(e)
      return true
    })

    s.updateSettings({ temperature: 0.9, model: { id: 'new-model' } })
    expect(s.parameters.temperature).toBe(0.9)
    expect(s.model).toBe('new-model')

    expect(sent).toHaveLength(1)
    expect(sent[0].type).toBe('UpdateChatSessionCommand')
    const payload = sent[0].payload as { patch: { temperature: number; model: { id: string } } }
    expect(payload.patch.temperature).toBe(0.9)
    expect(payload.patch.model.id).toBe('new-model')
  })

  it('returns false from senders when not connected', () => {
    const s = useSessionStore()
    // No sender bound.
    expect(s.submitPrompt('hi')).toBe(false)
    expect(s.cancel()).toBe(false)
    expect(s.updateSettings({ temperature: 1 })).toBe(false)
    void vi
  })
})

describe('session store - enhancements', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('streams reasoning into the active assistant turn', () => {
    const s = useSessionStore()
    s.apply(event('ChatReasoningDeltaEvent', { delta: 'think', snapshot: 'think' }))
    s.apply(event('ChatReasoningDeltaEvent', { delta: 'ing', snapshot: 'thinking' }))
    expect(s.messages).toHaveLength(1)
    expect(messageReasoning(s.messages[0])).toBe('thinking')
  })

  it('streams live tool output by call id', () => {
    const s = useSessionStore()
    s.apply(event('ChatToolExecutionStartedEvent', { call_id: 'c1', tool_name: 'bash', arguments_summary: 'ls' }))
    s.apply(event('ChatToolOutputEvent', { call_id: 'c1', chunk: 'line1\n' }))
    s.apply(event('ChatToolOutputEvent', { call_id: 'c1', chunk: 'line2\n' }))
    expect(messageTools(s.messages[0])[0].output).toBe('line1\nline2\n')
  })

  it('upserts a tool from a tool-call delta before execution starts', () => {
    const s = useSessionStore()
    s.apply(event('ChatResponseDeltaEvent', { delta: 'x', snapshot: 'x' }))
    s.apply(event('ChatToolCallDeltaEvent', { call_id: 'c1', index: 0, tool_name: 'read', arguments_summary: 'pa' }))
    s.apply(event('ChatToolCallDeltaEvent', { call_id: 'c1', index: 0, tool_name: 'read', arguments_summary: 'path=x' }))
    expect(messageTools(s.messages[0])).toHaveLength(1)
    expect(messageTools(s.messages[0])[0].argumentsSummary).toBe('path=x')
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

  it('stores listed sessions and handles session deletion', () => {
    const s = useSessionStore()
    s.apply(event('SessionsListedEvent', { sessions: [{ id: 'a', message_count: 2 }, { id: 'b', message_count: 0 }] }))
    expect(s.sessions).toHaveLength(2)

    s.apply(event('SessionDeletedEvent', { session_id: 'a' }))
    expect(s.sessions).toHaveLength(1)
    expect(s.sessions[0].id).toBe('b')
  })

  it('orders reasoning, text and tools chronologically within a turn', () => {
    const s = useSessionStore()
    s.apply(event('ChatReasoningDeltaEvent', { delta: 'plan', snapshot: 'plan' }))
    s.apply(event('ChatResponseDeltaEvent', { delta: 'checking', snapshot: 'checking' }))
    s.apply(event('ChatToolExecutionStartedEvent', { call_id: 'c1', tool_name: 'bash', arguments_summary: 'ls' }))
    s.apply(event('ChatToolExecutionCompletedEvent', { call_id: 'c1', tool_name: 'bash', is_error: false, result_summary: 'ok' }))
    // The next iteration's text uses a fresh snapshot, so it opens a NEW text
    // part after the tool rather than merging back into the first.
    s.apply(event('ChatResponseDeltaEvent', { delta: 'done', snapshot: 'done' }))

    const parts = s.messages[0].parts
    expect(parts.map((p) => p.kind)).toEqual(['reasoning', 'text', 'tool', 'text'])
    const last = parts[parts.length - 1]
    expect(last.kind).toBe('text')
    if (last.kind === 'text') expect(last.text).toBe('done')
  })

  it('absorbs token usage, context window and cost from state', () => {
    const s = useSessionStore()
    s.apply(
      event('ChatSessionSnapshotEvent', {
        state: {
          session_id: 's',
          status: 'idle',
          model: { id: 'm', context_window: 1000, cost: { input: 3, output: 15 } },
          parameters: { max_tokens: 0, temperature: 0 },
          messages: [],
          last_usage: { prompt_tokens: 100, completion_tokens: 50, total_tokens: 150 },
        },
      }),
    )
    expect(s.usage.total_tokens).toBe(150)
    expect(s.contextWindow).toBe(1000)
    expect(s.cost.input).toBe(3)
  })

  it('resyncs messages from the authoritative snapshot after a reconnect', () => {
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 's', model: 'm', provider: 'p' })
    s.apply(event('ChatResponseDeltaEvent', { delta: 'partial', snapshot: 'partial' }))
    expect(s.messages).toHaveLength(1)

    // A repeated init marks a reconnect; the next snapshot replaces local state.
    s.apply({ type: 'init', session_id: 's', model: 'm', provider: 'p' })
    s.apply(
      event('ChatSessionSnapshotEvent', {
        state: {
          session_id: 's',
          status: 'idle',
          model: { id: 'm' },
          parameters: { max_tokens: 0, temperature: 0 },
          messages: [
            { role: 'user', content: 'hi' },
            { role: 'assistant', content: 'hello there' },
          ],
        },
      }),
    )
    expect(s.messages).toHaveLength(2)
    expect(messageText(s.messages[1])).toBe('hello there')
  })

  it('flips streaming on and adopts the request_id from ChatResponseStartedEvent', () => {
    // Before the first delta, the only signal that the backend has accepted
    // the prompt is ChatResponseStartedEvent. The Stop button relies on
    // streaming being true during that window, otherwise users have no way
    // to cancel a slow model warm-up.
    const s = useSessionStore()
    s.apply(event('ChatResponseStartedEvent', { session_id: 's', request_id: 'server-123', started_at: '2026-01-01T00:00:00Z' }))
    expect(s.streaming).toBe(true)
    expect(s.activeRequestId).toBe('server-123')
  })

  it('synchronises the active request_id from authoritative snapshot state', () => {
    // A snapshot from the backend is the source of truth for what request
    // is actually in flight. The client should adopt that id so cancel
    // commands always target the real in-flight turn, even after a
    // reconnect or a race between two submits.
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 's', model: 'm', provider: 'p' })
    s.bindSender(() => true)
    s.submitPrompt('hi')
    const local = s.activeRequestId
    expect(local).toBeTruthy()

    // Backend has promoted a *different* request (e.g. on reconnect after a
    // dropped submit): the snapshot's id is what the server has stored.
    s.apply(
      event('ChatSessionSnapshotEvent', {
        state: {
          session_id: 's',
          status: 'streaming',
          active_request_id: 'server-xyz',
          model: { id: 'm' },
          parameters: { max_tokens: 0, temperature: 0 },
          messages: [],
        },
      }),
    )
    expect(s.activeRequestId).toBe('server-xyz')
    expect(s.streaming).toBe(true)
  })

  it('clears the active request_id when the snapshot shows no active request', () => {
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 's', model: 'm', provider: 'p' })
    s.apply(
      event('ChatSessionSnapshotEvent', {
        state: {
          session_id: 's',
          status: 'idle',
          active_request_id: '',
          model: { id: 'm' },
          parameters: { max_tokens: 0, temperature: 0 },
          messages: [],
        },
      }),
    )
    expect(s.activeRequestId).toBe('')
    expect(s.streaming).toBe(false)
  })

  it('sends a cancel even when the local request_id is empty', () => {
    // After reconnect or a missed event the client may have lost its
    // request_id. The backend treats an empty id as "cancel whatever is
    // active", so the cancel should still be sent so the user can stop
    // a running response.
    const s = useSessionStore()
    s.apply({ type: 'init', session_id: 'sess-1', model: 'm', provider: 'p' })
    s.activeRequestId = ''

    const sent: Envelope[] = []
    s.bindSender((e) => {
      sent.push(e)
      return true
    })
    expect(s.cancel()).toBe(true)
    expect(sent).toHaveLength(1)
    expect(sent[0].type).toBe('CancelChatRequestCommand')
    const payload = sent[0].payload as { session_id: string; request_id: string }
    expect(payload.session_id).toBe('sess-1')
    expect(payload.request_id).toBe('')
  })
})
