import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  command,
  type ChatParameters,
  type ChatSessionPatch,
  type ChatSessionState,
  type CommandRef,
  type ChatNotificationEvent,
  type ChatResponseCompletedEvent,
  type ChatResponseDeltaEvent,
  type ChatRuntimeErrorEvent,
  type ChatSessionSnapshotEvent,
  type ChatToolExecutionCompletedEvent,
  type ChatToolExecutionStartedEvent,
  type Envelope,
  type InitMessage,
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
}

export interface DisplayMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  /** Tools invoked during an assistant turn, in call order. */
  tools: ToolCall[]
  streaming: boolean
}

export interface Notice {
  id: string
  level: 'info' | 'warn' | 'error'
  message: string
}

let seq = 0
const nextId = () => `m${++seq}`

export const useSessionStore = defineStore('session', () => {
  const sessionId = ref('')
  const model = ref('')
  const provider = ref('')
  const commands = ref<CommandRef[]>([])
  const messages = ref<DisplayMessage[]>([])
  const notices = ref<Notice[]>([])
  const streaming = ref(false)
  const activeRequestId = ref('')
  const status = ref('idle')
  const parameters = ref<ChatParameters>({ max_tokens: 0, temperature: 0, reasoning_effort: '' })

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
      tools: [],
      streaming: true,
    }
    messages.value.push(msg)
    return msg
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
        break
      }
      case 'ChatSessionSnapshotEvent': {
        absorbState((msg.payload as ChatSessionSnapshotEvent).state)
        break
      }
      case 'ChatResponseDeltaEvent': {
        const ev = msg.payload as ChatResponseDeltaEvent
        const a = activeAssistant()
        a.content = ev.snapshot || a.content + ev.delta
        streaming.value = true
        break
      }
      case 'ChatToolExecutionStartedEvent': {
        const ev = msg.payload as ChatToolExecutionStartedEvent
        const a = activeAssistant()
        a.tools.push({
          callId: ev.call_id,
          name: ev.tool_name,
          argumentsSummary: ev.arguments_summary,
          status: 'running',
        })
        break
      }
      case 'ChatToolExecutionCompletedEvent': {
        const ev = msg.payload as ChatToolExecutionCompletedEvent
        for (const m of messages.value) {
          const t = m.tools.find((tool: ToolCall) => tool.callId === ev.call_id)
          if (t) {
            t.status = ev.is_error ? 'error' : 'ok'
            t.resultSummary = ev.result_summary
            break
          }
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
    }
  }

  /** Absorb authoritative session state from a snapshot or completion event. */
  function absorbState(state: ChatSessionState) {
    if (!state) return
    sessionId.value = state.session_id || sessionId.value
    if (state.model?.id) model.value = state.model.id
    if (state.status) status.value = state.status
    if (state.parameters) parameters.value = { ...state.parameters }
  }

  function pushNotice(level: Notice['level'], message: string) {
    notices.value.push({ id: nextId(), level, message })
    if (notices.value.length > 50) notices.value.shift()
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

  return {
    sessionId,
    model,
    provider,
    commands,
    messages,
    notices,
    streaming,
    status,
    parameters,
    bindSender,
    apply,
    submitPrompt,
    cancel,
    updateSettings,
  }
})
