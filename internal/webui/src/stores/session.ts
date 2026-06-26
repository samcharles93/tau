import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  command,
  type ChatParameters,
  type ChatReasoningDeltaEvent,
  type ChatSessionPatch,
  type ChatSessionState,
  type CommandRef,
  type ChatNotificationEvent,
  type ChatResponseCompletedEvent,
  type ChatResponseDeltaEvent,
  type ChatRuntimeErrorEvent,
  type ChatSessionSnapshotEvent,
  type ChatToolCallDeltaEvent,
  type ChatToolExecutionCompletedEvent,
  type ChatToolExecutionStartedEvent,
  type ChatToolOutputEvent,
  type Envelope,
  type InitMessage,
  type InteractivePromptKind,
  type InteractivePromptRequestedEvent,
  type SessionsListedEvent,
  type SessionSummary,
  type SubmitChatPromptCommand,
  type UpdateChatSessionCommand,
} from '@/lib/protocol'

export type ToolStatus = 'running' | 'ok' | 'error'

export interface ToolCall {
  callId: string
  name: string
  argumentsSummary: string
  status: ToolStatus
  resultSummary?: string
  /** Live stdout/stderr chunks streamed while the tool runs. */
  output: string
}

export interface DisplayMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  /** Streamed chain-of-thought for this assistant turn, if any. */
  reasoning: string
  /** Tools invoked during an assistant turn, in call order. */
  tools: ToolCall[]
  streaming: boolean
}

export interface Notice {
  id: string
  level: 'info' | 'warn' | 'error'
  message: string
}

export interface InteractivePrompt {
  requestId: string
  kind: InteractivePromptKind
  title: string
  message: string
}

let seq = 0
const nextId = () => `m${++seq}`

export const useSessionStore = defineStore('session', () => {
  const sessionId = ref('')
  const model = ref('')
  const provider = ref('')
  const commands = ref<CommandRef[]>([])
  const availableModels = ref<string[]>([])
  const messages = ref<DisplayMessage[]>([])
  const notices = ref<Notice[]>([])
  const streaming = ref(false)
  const activeRequestId = ref('')
  const status = ref('idle')
  const parameters = ref<ChatParameters>({ max_tokens: 0, temperature: 0, reasoning_effort: '' })
  const activePrompt = ref<InteractivePrompt | null>(null)
  const sessions = ref<SessionSummary[]>([])

  let sendEnvelope: ((e: Envelope) => boolean) | null = null

  function bindSender(fn: (e: Envelope) => boolean) {
    sendEnvelope = fn
  }

  function activeAssistant(): DisplayMessage {
    const last = messages.value[messages.value.length - 1]
    if (last && last.role === 'assistant' && last.streaming) return last
    const msg: DisplayMessage = {
      id: nextId(),
      role: 'assistant',
      content: '',
      reasoning: '',
      tools: [],
      streaming: true,
    }
    messages.value.push(msg)
    return msg
  }

  /** Find a tool by call id within any rendered message. */
  function findTool(callId: string): ToolCall | undefined {
    for (const m of messages.value) {
      const t = m.tools.find((tool: ToolCall) => tool.callId === callId)
      if (t) return t
    }
    return undefined
  }

  /** Reduce a single inbound wire message into store state. */
  function apply(msg: { type: string; [key: string]: unknown }) {
    switch (msg.type) {
      case 'init': {
        const init = msg as unknown as InitMessage
        sessionId.value = init.session_id
        model.value = init.model
        provider.value = init.provider
        commands.value = init.commands ?? []
        availableModels.value = init.models ?? []
        break
      }
      case 'ChatSessionSnapshotEvent': {
        absorbState((msg.payload as ChatSessionSnapshotEvent).state)
        break
      }
      case 'SessionLoadedEvent': {
        // A different session was loaded: rebuild the stream from scratch.
        messages.value = []
        absorbState((msg.payload as { state: ChatSessionState }).state)
        break
      }
      case 'ChatResponseDeltaEvent': {
        const ev = msg.payload as ChatResponseDeltaEvent
        const a = activeAssistant()
        a.content = ev.snapshot || a.content + ev.delta
        streaming.value = true
        break
      }
      case 'ChatReasoningDeltaEvent': {
        const ev = msg.payload as ChatReasoningDeltaEvent
        const a = activeAssistant()
        a.reasoning = ev.snapshot || a.reasoning + ev.delta
        streaming.value = true
        break
      }
      case 'ChatToolCallDeltaEvent': {
        const ev = msg.payload as ChatToolCallDeltaEvent
        if (!ev.call_id) break
        const a = activeAssistant()
        const existing = a.tools.find((t) => t.callId === ev.call_id)
        if (existing) {
          if (ev.tool_name) existing.name = ev.tool_name
          if (ev.arguments_summary) existing.argumentsSummary = ev.arguments_summary
        } else {
          a.tools.push({
            callId: ev.call_id,
            name: ev.tool_name,
            argumentsSummary: ev.arguments_summary,
            status: 'running',
            output: '',
          })
        }
        break
      }
      case 'ChatToolExecutionStartedEvent': {
        const ev = msg.payload as ChatToolExecutionStartedEvent
        const a = activeAssistant()
        const existing = a.tools.find((t) => t.callId === ev.call_id)
        if (existing) {
          existing.name = ev.tool_name
          existing.argumentsSummary = ev.arguments_summary || existing.argumentsSummary
          existing.status = 'running'
        } else {
          a.tools.push({
            callId: ev.call_id,
            name: ev.tool_name,
            argumentsSummary: ev.arguments_summary,
            status: 'running',
            output: '',
          })
        }
        break
      }
      case 'ChatToolOutputEvent': {
        const ev = msg.payload as ChatToolOutputEvent
        const t = findTool(ev.call_id)
        if (t) t.output += ev.chunk
        break
      }
      case 'ChatToolExecutionCompletedEvent': {
        const ev = msg.payload as ChatToolExecutionCompletedEvent
        const t = findTool(ev.call_id)
        if (t) {
          t.status = ev.is_error ? 'error' : 'ok'
          t.resultSummary = ev.result_summary
        }
        break
      }
      case 'ChatResponseCompletedEvent': {
        const ev = msg.payload as ChatResponseCompletedEvent
        const a = messages.value[messages.value.length - 1]
        if (a && a.role === 'assistant') {
          a.streaming = false
          if (ev.state?.messages?.length) {
            const lastAssistant = [...ev.state.messages].reverse().find((m) => m.role === 'assistant')
            if (lastAssistant?.content) a.content = lastAssistant.content
          }
        }
        if (ev.state) absorbState(ev.state)
        streaming.value = false
        activeRequestId.value = ''
        break
      }
      case 'ChatRuntimeErrorEvent': {
        const ev = msg.payload as ChatRuntimeErrorEvent
        pushNotice('error', ev.message)
        streaming.value = false
        break
      }
      case 'ChatNotificationEvent': {
        const ev = msg.payload as ChatNotificationEvent
        pushNotice(ev.level, ev.message)
        break
      }
      case 'InteractivePromptRequestedEvent': {
        const ev = msg.payload as InteractivePromptRequestedEvent
        activePrompt.value = {
          requestId: ev.request_id,
          kind: ev.kind,
          title: ev.title,
          message: ev.message,
        }
        break
      }
      case 'SessionsListedEvent': {
        sessions.value = (msg.payload as SessionsListedEvent).sessions ?? []
        break
      }
    }
  }

  /** Absorb authoritative session state from a snapshot or completion event. */
  function absorbState(state: ChatSessionState) {
    if (!state) return
    sessionId.value = state.session_id || sessionId.value
    if (state.model?.id) model.value = state.model.id
    if (state.status) status.value = state.status
    if (state.parameters) parameters.value = { ...state.parameters }

    // On a fresh connection the local stream is empty; hydrate it from the
    // replayed snapshot so a browser joining mid-session sees the history.
    if (messages.value.length === 0 && state.messages?.length) {
      hydrateFromHistory(state.messages)
    }
  }

  /** Rebuild display messages from persisted conversation history. */
  function hydrateFromHistory(history: ChatSessionState['messages']) {
    const hydrated: DisplayMessage[] = []
    for (const m of history) {
      if (m.role !== 'user' && m.role !== 'assistant') continue
      if (!m.content) continue
      hydrated.push({
        id: nextId(),
        role: m.role,
        content: m.content,
        reasoning: m.reasoning_content ?? '',
        tools: [],
        streaming: false,
      })
    }
    messages.value = hydrated
  }

  function pushNotice(level: Notice['level'], message: string) {
    notices.value.push({ id: nextId(), level, message })
    if (notices.value.length > 50) notices.value.shift()
  }

  function dismissNotice(id: string) {
    notices.value = notices.value.filter((n) => n.id !== id)
  }

  /** Submit a prompt: optimistically render it, then send the command. */
  function submitPrompt(prompt: string): boolean {
    const text = prompt.trim()
    if (!text || !sendEnvelope) return false

    const requestId = `web-${Date.now()}`
    activeRequestId.value = requestId
    messages.value.push({
      id: nextId(),
      role: 'user',
      content: text,
      reasoning: '',
      tools: [],
      streaming: false,
    })

    const payload: SubmitChatPromptCommand = {
      session_id: sessionId.value,
      request_id: requestId,
      prompt: text,
      submitted_at: new Date().toISOString(),
    }
    return sendEnvelope(command('SubmitChatPromptCommand', payload))
  }

  function cancel(): boolean {
    if (!sendEnvelope || !activeRequestId.value) return false
    return sendEnvelope(
      command('CancelChatRequestCommand', {
        session_id: sessionId.value,
        request_id: activeRequestId.value,
      }),
    )
  }

  /** Emit an UpdateChatSessionCommand with only the changed settings fields. */
  function updateSettings(patch: ChatSessionPatch): boolean {
    if (!sendEnvelope) return false
    // Optimistically reflect param changes so the drawer stays responsive.
    parameters.value = {
      max_tokens: patch.max_tokens ?? parameters.value.max_tokens,
      temperature: patch.temperature ?? parameters.value.temperature,
      reasoning_effort: patch.reasoning_effort ?? parameters.value.reasoning_effort,
    }
    if (patch.model?.id) model.value = patch.model.id

    const payload: UpdateChatSessionCommand = { session_id: sessionId.value, patch }
    return sendEnvelope(command('UpdateChatSessionCommand', payload))
  }

  /** Answer an interactive prompt (tool confirmation or question). */
  function respondPrompt(opts: { confirmed: boolean; canceled?: boolean; response?: string }): boolean {
    const prompt = activePrompt.value
    if (!sendEnvelope || !prompt) return false
    const ok = sendEnvelope(
      command('RespondInteractivePromptCommand', {
        request_id: prompt.requestId,
        confirmed: opts.confirmed,
        canceled: opts.canceled ?? false,
        response: opts.response,
        responded_at: new Date().toISOString(),
      }),
    )
    activePrompt.value = null
    return ok
  }

  function listSessions(limit = 50): boolean {
    if (!sendEnvelope) return false
    return sendEnvelope(command('ListSessionsCommand', { limit }))
  }

  function loadSession(id: string): boolean {
    if (!sendEnvelope) return false
    return sendEnvelope(command('LoadSessionCommand', { session_id: id }))
  }

  function deleteSession(id: string): boolean {
    if (!sendEnvelope) return false
    return sendEnvelope(command('DeleteSessionCommand', { session_id: id }))
  }

  function exportSession(id: string, format = 'jsonl'): boolean {
    if (!sendEnvelope) return false
    return sendEnvelope(command('ExportSessionCommand', { session_id: id, format }))
  }

  return {
    sessionId,
    model,
    provider,
    commands,
    availableModels,
    messages,
    notices,
    streaming,
    status,
    parameters,
    activePrompt,
    sessions,
    bindSender,
    apply,
    submitPrompt,
    cancel,
    updateSettings,
    respondPrompt,
    dismissNotice,
    listSessions,
    loadSession,
    deleteSession,
    exportSession,
  }
})
