import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  command,
  type ChatCost,
  type ChatModelRef,
  type ChatParameters,
  type ChatReasoningDeltaEvent,
  type ChatResponseCancelledEvent,
  type ChatSessionPatch,
  type ChatSessionState,
  type ChatUsage,
  type ChildAgentResult,
  type ChildAgentStateEvent,
  type CommandRef,
  type CommandsChangedEvent,
  type ExtensionCommand,
  type ExtensionCommandsChangedEvent,
  type ExtensionsReloadedEvent,
  type ChatNotificationEvent,
  type ChatResponseCompletedEvent,
  type ChatResponseDeltaEvent,
  type ChatResponseStartedEvent,
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
  type SessionDeletedEvent,
  type SessionSummary,
  type SkillInfo,
  type SkillsChangedEvent,
  type SubmitChatPromptCommand,
  type UpdateChatSessionCommand,
  type ResetChatSessionCommand,
  type ReloadExtensionsCommand,
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

/**
 * A single ordered segment of an assistant turn. Reasoning, text, and tool
 * calls are kept in arrival order so the rendered timeline reflects what the
 * model actually did (reason → call tools → read results → answer), rather
 * than collapsing everything into fixed reasoning/text/tools buckets.
 */
export type MessagePart =
  | { kind: 'text'; id: string; text: string }
  | { kind: 'reasoning'; id: string; text: string }
  | { kind: 'tool'; id: string; tool: ToolCall }

export interface DisplayMessage {
  id: string
  role: 'user' | 'assistant'
  /** Ordered timeline of this turn's content. */
  parts: MessagePart[]
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

/** Concatenated text of all text parts in a message. */
export function messageText(m: DisplayMessage): string {
  return m.parts
    .filter((p): p is Extract<MessagePart, { kind: 'text' }> => p.kind === 'text')
    .map((p) => p.text)
    .join('\n\n')
}

/** Concatenated text of all reasoning parts in a message. */
export function messageReasoning(m: DisplayMessage): string {
  return m.parts
    .filter((p): p is Extract<MessagePart, { kind: 'reasoning' }> => p.kind === 'reasoning')
    .map((p) => p.text)
    .join('\n\n')
}

/** All tool calls in a message, in order. */
export function messageTools(m: DisplayMessage): ToolCall[] {
  return m.parts
    .filter((p): p is Extract<MessagePart, { kind: 'tool' }> => p.kind === 'tool')
    .map((p) => p.tool)
}

export const useSessionStore = defineStore('session', () => {
  const sessionId = ref('')
  const model = ref('')
  const provider = ref('')
  const providers = ref<string[]>([])
  const commands = ref<CommandRef[]>([])
  const extensionCommands = ref<ExtensionCommand[]>([])
  const availableModels = ref<ChatModelRef[]>([])
  const messages = ref<DisplayMessage[]>([])
  const notices = ref<Notice[]>([])
  const streaming = ref(false)
  const activeRequestId = ref('')
  const status = ref('idle')
  const parameters = ref<ChatParameters>({ max_tokens: 0, temperature: 0, reasoning_effort: '' })
  const activePrompt = ref<InteractivePrompt | null>(null)
  const skills = ref<SkillInfo[]>([])
  const sessions = ref<SessionSummary[]>([])
  // Child agent state keyed by spawning tool call id, mirroring
  // internal/tui2's childAgents map and docs/specs/state-taxonomy.md (Category 4).
  const childAgents = ref<Map<string, ChildAgentResult>>(new Map())
  // Token usage and pricing for the current model, surfaced in the input bar.
  const usage = ref<ChatUsage>({})
  const contextWindow = ref(0)
  const cost = ref<ChatCost>({})

  let sendEnvelope: ((e: Envelope) => boolean) | null = null
  // Tracks whether we have seen the first `init`. A second `init` means the
  // socket reconnected, so the next authoritative snapshot must replace local
  // state to recover anything missed while offline.
  let initialized = false
  let pendingResync = false

  function bindSender(fn: (e: Envelope) => boolean) {
    sendEnvelope = fn
  }

  function activeAssistant(): DisplayMessage {
    const last = messages.value[messages.value.length - 1]
    if (last && last.role === 'assistant' && last.streaming) return last
    const msg: DisplayMessage = {
      id: nextId(),
      role: 'assistant',
      parts: [],
      streaming: true,
    }
    messages.value.push(msg)
    return msg
  }

  /** Append/extend the trailing text part, opening a new one after a tool. */
  function writeText(a: DisplayMessage, snapshot: string, delta: string) {
    const last = a.parts[a.parts.length - 1]
    if (last && last.kind === 'text') {
      last.text = snapshot || last.text + delta
    } else {
      a.parts.push({ kind: 'text', id: nextId(), text: snapshot || delta })
    }
  }

  /** Append/extend the trailing reasoning part. */
  function writeReasoning(a: DisplayMessage, snapshot: string, delta: string) {
    const last = a.parts[a.parts.length - 1]
    if (last && last.kind === 'reasoning') {
      last.text = snapshot || last.text + delta
    } else {
      a.parts.push({ kind: 'reasoning', id: nextId(), text: snapshot || delta })
    }
  }

  /** Find or create a tool part by call id within the active assistant turn. */
  function upsertTool(a: DisplayMessage, callId: string): ToolCall {
    const existing = findTool(callId)
    if (existing) return existing
    const tool: ToolCall = {
      callId,
      name: '',
      argumentsSummary: '',
      status: 'running',
      output: '',
    }
    a.parts.push({ kind: 'tool', id: nextId(), tool })
    return tool
  }

  /** Find a tool by call id within any rendered message. */
  function findTool(callId: string): ToolCall | undefined {
    for (const m of messages.value) {
      for (const p of m.parts) {
        if (p.kind === 'tool' && p.tool.callId === callId) return p.tool
      }
    }
    return undefined
  }

  /** The final assistant content carried by an authoritative state, if any. */
  function lastAssistantContent(state: ChatSessionState | undefined): string {
    if (!state?.messages?.length) return ''
    for (let i = state.messages.length - 1; i >= 0; i--) {
      const m = state.messages[i]
      if (m.role === 'assistant' && m.content) return m.content
    }
    return ''
  }

  /** Reduce a single inbound wire message into store state. */
  function apply(msg: { type: string; [key: string]: unknown }) {
    switch (msg.type) {
      case 'init': {
        const init = msg as unknown as InitMessage
        sessionId.value = init.session_id
        model.value = init.model
        provider.value = init.provider
        providers.value = init.providers ?? []
        commands.value = init.commands ?? []
        skills.value = init.skills ?? []
        extensionCommands.value = init.extension_commands ?? []
        // availableModels is ChatModelRef[] but the wire delivers it as a plain
        // object array; cast through unknown to satisfy the type checker.
        availableModels.value = (init.models ?? []) as ChatModelRef[]
        applyModelMetadata(model.value)
        // A repeat init signals a reconnect: force a resync from the next snapshot.
        if (initialized) pendingResync = true
        initialized = true
        break
      }
      case 'ChatSessionSnapshotEvent': {
        absorbState((msg.payload as ChatSessionSnapshotEvent).state)
        break
      }
      case 'SessionLoadedEvent': {
        // A different session was loaded: rebuild the stream from scratch.
        messages.value = []
        childAgents.value = new Map()
        absorbState((msg.payload as { state: ChatSessionState }).state)
        break
      }
      case 'ChatResponseStartedEvent': {
        // The backend has begun processing the request and confirmed the
        // authoritative request_id. Adopt it (it normally matches what we
        // generated client-side, but the snapshot is the source of truth
        // for any future re-attach) and flip streaming on so the Stop button
        // appears immediately - without waiting for the first delta, which
        // can take several seconds for slow prompts.
        const ev = msg.payload as ChatResponseStartedEvent
        if (ev.request_id) activeRequestId.value = ev.request_id
        streaming.value = true
        break
      }
      case 'ChatResponseDeltaEvent': {
        const ev = msg.payload as ChatResponseDeltaEvent
        writeText(activeAssistant(), ev.snapshot, ev.delta)
        streaming.value = true
        break
      }
      case 'ChatReasoningDeltaEvent': {
        const ev = msg.payload as ChatReasoningDeltaEvent
        writeReasoning(activeAssistant(), ev.snapshot, ev.delta)
        streaming.value = true
        break
      }
      case 'ChatToolCallDeltaEvent': {
        const ev = msg.payload as ChatToolCallDeltaEvent
        if (!ev.call_id) break
        const tool = upsertTool(activeAssistant(), ev.call_id)
        if (ev.tool_name) tool.name = ev.tool_name
        if (ev.arguments_summary) tool.argumentsSummary = ev.arguments_summary
        break
      }
      case 'ChatToolExecutionStartedEvent': {
        const ev = msg.payload as ChatToolExecutionStartedEvent
        const tool = upsertTool(activeAssistant(), ev.call_id)
        tool.name = ev.tool_name
        tool.argumentsSummary = ev.arguments_summary || tool.argumentsSummary
        tool.status = 'running'
        break
      }
      case 'ChatToolOutputEvent': {
        const ev = msg.payload as ChatToolOutputEvent
        const t = findTool(ev.call_id)
        if (t) t.output += ev.chunk
        break
      }
      case 'ChildAgentStateEvent': {
        const ev = msg.payload as ChildAgentStateEvent
        const prev = childAgents.value.get(ev.call_id)
        childAgents.value.set(ev.call_id, {
          instance_id: ev.instance_id,
          spec_name: ev.spec_name,
          status: ev.status,
          activity: ev.activity,
          turns: ev.turns,
          tokens: ev.input_tokens + ev.output_tokens,
          duration_ms: ev.elapsed_ms,
          error_msg: ev.error,
          // ChildAgentStateEvent never carries a session_id; preserve any
          // already recorded from a prior ChatToolExecutionCompletedEvent.
          session_id: prev?.session_id,
        })
        break
      }
      case 'ChatToolExecutionCompletedEvent': {
        const ev = msg.payload as ChatToolExecutionCompletedEvent
        const t = findTool(ev.call_id)
        if (t) {
          t.status = ev.is_error ? 'error' : 'ok'
          t.resultSummary = ev.result_summary
        }
        // Extract child agent result from agent tool details.
        // Skip spawn-rejected entries that carry no instance_id (the
        // concurrency admission gate sends details with only status +
        // failure_reason + queued). Matches tui2's extractChildAgentResult
        // which returns false when instance_id is empty.
        if (ev.tool_name === 'agent' && ev.details?.instance_id) {
          const prev = childAgents.value.get(ev.call_id)
          const d = ev.details
          childAgents.value.set(ev.call_id, {
            instance_id: d.instance_id ?? prev?.instance_id ?? '',
            spec_name: d.spec_name ?? prev?.spec_name ?? '',
            status: d.status ?? prev?.status ?? 'completed',
            activity: prev?.activity ?? '',
            turns: d.usage?.turns ?? prev?.turns ?? 0,
            tokens: (d.usage?.input_tokens ?? 0) + (d.usage?.output_tokens ?? 0),
            duration_ms: d.duration_ms ?? prev?.duration_ms ?? 0,
            error_msg: d.error ?? prev?.error_msg,
            session_id: d.session_id ?? prev?.session_id,
          })
        }
        break
      }
      case 'ChatResponseCompletedEvent': {
        const ev = msg.payload as ChatResponseCompletedEvent
        const a = messages.value[messages.value.length - 1]
        if (a && a.role === 'assistant') {
          a.streaming = false
          const finalText = lastAssistantContent(ev.state)
          if (finalText) {
            const lastText = [...a.parts].reverse().find((p) => p.kind === 'text')
            if (lastText && lastText.kind === 'text') lastText.text = finalText
            else a.parts.push({ kind: 'text', id: nextId(), text: finalText })
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
      case 'ChatResponseCancelledEvent': {
        const ev = msg.payload as ChatResponseCancelledEvent
        const a = messages.value[messages.value.length - 1]
        if (a && a.role === 'assistant' && a.streaming) {
          a.streaming = false
          // Mark still-running tools as interrupted so they don't render a
          // permanently-pulsing "running" badge — no completion event will
          // ever arrive for them.
          for (const p of a.parts) {
            if (p.kind === 'tool' && p.tool.status === 'running') {
              p.tool.status = 'error'
            }
          }
        }
        if (ev.state) absorbState(ev.state)
        streaming.value = false
        activeRequestId.value = ''
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
      case 'SessionDeletedEvent': {
        const ev = msg.payload as SessionDeletedEvent
        sessions.value = sessions.value.filter((s) => s.id !== ev.session_id)
        break
      }
      case 'SkillsChangedEvent': {
        const ev = msg.payload as SkillsChangedEvent
        skills.value = ev.skills ?? []
        break
      }
      case 'CommandsChangedEvent': {
        const ev = msg.payload as CommandsChangedEvent
        commands.value = ev.commands ?? []
        break
      }
      case 'ExtensionCommandsChangedEvent': {
        const ev = msg.payload as ExtensionCommandsChangedEvent
        extensionCommands.value = ev.commands ?? []
        break
      }
      case 'ExtensionsReloadedEvent': {
        const ev = msg.payload as ExtensionsReloadedEvent
        extensionCommands.value = ev.result?.commands ?? []
        break
      }
    }
  }

  /** Absorb authoritative session state from a snapshot or completion event. */
  function absorbState(state: ChatSessionState) {
    if (!state) return
    sessionId.value = state.session_id || sessionId.value
    if (state.provider) provider.value = state.provider
    if (state.model?.id) {
      model.value = state.model.id
      // The snapshot carries context_window/cost when the model ref has a
      // populated Config. After a model patch the Config is lost, so fall
      // back to the model list that was seeded at init time.
      if (state.model.context_window) {
        contextWindow.value = state.model.context_window
      }
      if (state.model.cost?.input != null || state.model.cost?.output != null) {
        cost.value = state.model.cost
      }
      // If the snapshot lacked metadata, pull it from the init model list.
      if ((!state.model.context_window || !state.model.cost?.input) && state.model.id) {
        applyModelMetadata(state.model.id)
      }
    }
    if (state.status) status.value = state.status
    if (state.parameters) parameters.value = { ...state.parameters }
    if (state.last_usage) usage.value = state.last_usage
    // The backend's active_request_id is the source of truth for cancel
    // commands. When the snapshot carries a non-empty value, adopt it;
    // when it goes empty (turn finished), clear our local mirror.
    if (state.active_request_id !== undefined) {
      activeRequestId.value = state.active_request_id ?? ''
      // Streaming is true whenever the session has an active request, even
      // before the first delta arrives (e.g. during a long model warm-up).
      streaming.value = !!state.active_request_id
    }

    // On a fresh connection the local stream is empty; hydrate it from the
    // replayed snapshot so a browser joining mid-session sees the history. A
    // reconnect (pendingResync) also rebuilds to recover missed events.
    if (messages.value.length === 0 || pendingResync) {
      if (state.messages?.length) hydrateFromHistory(state.messages)
      pendingResync = false
    }
  }

  /** Update context-window / pricing from the advertised model list. */
  function applyModelMetadata(id: string) {
    const ref = availableModels.value.find((m) => m.id === id)
    if (!ref) return
    if (ref.context_window) contextWindow.value = ref.context_window
    if (ref.cost) cost.value = ref.cost
  }

  /** Rebuild display messages from persisted conversation history. */
  function hydrateFromHistory(history: ChatSessionState['messages']) {
    const hydrated: DisplayMessage[] = []
    // Tool result messages (role: 'tool') update the matching tool call from a
    // prior assistant message rather than becoming a display message themselves.
    const toolMap = new Map<string, ToolCall>()

    for (const m of history) {
      if (m.role === 'system') continue

      if (m.role === 'tool') {
        if (m.tool_call_id) {
          const tool = toolMap.get(m.tool_call_id)
          if (tool) {
            tool.status = 'ok'
            if (m.content) tool.resultSummary = m.content
          }
        }
        continue
      }

      if (m.role !== 'user' && m.role !== 'assistant') continue

      // Assistant messages may carry tool_calls without text content, so only
      // skip when all possible content fields are empty.
      if (!m.content && !m.reasoning_content && !m.tool_calls?.length) continue

      const parts: MessagePart[] = []

      if (m.reasoning_content) {
        parts.push({ kind: 'reasoning', id: nextId(), text: m.reasoning_content })
      }

      if (m.content) {
        parts.push({ kind: 'text', id: nextId(), text: m.content })
      }

      if (m.tool_calls) {
        for (const tc of m.tool_calls) {
          const tool: ToolCall = {
            callId: tc.id,
            name: tc.function.name,
            argumentsSummary: tc.function.arguments,
            status: 'ok', // Historical tools that made it into the log completed.
            output: '',
          }
          toolMap.set(tc.id, tool)
          parts.push({ kind: 'tool', id: nextId(), tool })
        }
      }

      hydrated.push({ id: nextId(), role: m.role, parts, streaming: false })
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
      parts: [{ kind: 'text', id: nextId(), text }],
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
    if (!sendEnvelope) return false
    // Prefer the locally tracked request_id (it matches what we sent on
    // submit). If that's empty - e.g. because the user clicked Stop during
    // a reconnect window before the new submit's snapshot arrived - still
    // send a cancel with an empty id; the backend treats that as "cancel
    // whatever is active" so the user's intent is honoured.
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
    if (patch.model?.id) {
      model.value = patch.model.id
      applyModelMetadata(patch.model.id)
    }
    if (patch.provider) provider.value = patch.provider

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
    // Optimistically remove from the list for a responsive UI.
    sessions.value = sessions.value.filter((s) => s.id !== id)
    return sendEnvelope(command('DeleteSessionCommand', { session_id: id }))
  }

  function exportSession(id: string, format = 'jsonl'): boolean {
    if (!sendEnvelope) return false
    return sendEnvelope(command('ExportSessionCommand', { session_id: id, format }))
  }

  /** Reset the current session (clear conversation). */
  function resetSession(): boolean {
    if (!sendEnvelope) return false
    const payload: ResetChatSessionCommand = { session_id: sessionId.value, requested_at: new Date().toISOString() }
    return sendEnvelope(command('ResetChatSessionCommand', payload))
  }

  /** Reload extensions. */
  function reloadExtensions(): boolean {
    if (!sendEnvelope) return false
    const payload: ReloadExtensionsCommand = { requested_at: new Date().toISOString() }
    return sendEnvelope(command('ReloadExtensionsCommand', payload))
  }

  return {
    sessionId,
    model,
    provider,
    providers,
    commands,
    skills,
    extensionCommands,
    availableModels,
    messages,
    notices,
    streaming,
    status,
    parameters,
    activePrompt,
    activeRequestId,
    sessions,
    childAgents,
    usage,
    contextWindow,
    cost,
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
    resetSession,
    reloadExtensions,
  }
})
