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

export interface InitMessage {
  type: 'init'
  session_id: string
  model: string
  provider: string
  /** Available model refs (id + config), if the backend advertises them. */
  models?: ChatModelRef[]
  /** Available provider names, if the backend advertises them. */
  providers?: string[]
  commands?: CommandRef[]
}

export interface CommandRef {
  name: string
  label: string
  description?: string
  accepts_args?: boolean
}

// ── Events (server -> client) ───────────────────────────────────────────────

export interface ChatModelRef {
  id: string
  url?: string
}

export interface ChatParameters {
  max_tokens: number
  temperature: number
  reasoning_effort?: string
}

export interface ChatSessionState {
  session_id: string
  model: ChatModelRef
  parameters: ChatParameters
  status: string
  messages: ChatMessage[]
  pending_assistant?: string
  active_request_id?: string
}

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant' | 'tool'
  content?: string
  reasoning_content?: string
}

export interface ChatSessionSnapshotEvent {
  state: ChatSessionState
}

export interface ChatResponseStartedEvent {
  session_id: string
  request_id: string
  started_at: string
}

export interface ChatResponseDeltaEvent {
  session_id: string
  request_id: string
  delta: string
  snapshot: string
  received_at: string
}

export interface ChatReasoningDeltaEvent {
  session_id: string
  request_id: string
  delta: string
  snapshot: string
  received_at: string
}

export interface ChatResponseCompletedEvent {
  state: ChatSessionState
  request_id: string
  finish_reason: string
  completed_at: string
}

export interface ChatToolExecutionStartedEvent {
  session_id: string
  request_id: string
  call_id: string
  tool_name: string
  arguments_summary: string
  started_at: string
}

export interface ChatToolExecutionCompletedEvent {
  session_id: string
  request_id: string
  call_id: string
  tool_name: string
  status: string
  result_summary: string
  is_error: boolean
  completed_at: string
}

export interface ChatRuntimeErrorEvent {
  session_id: string
  request_id?: string
  message: string
  fatal: boolean
  occurred_at: string
}

export interface ChatNotificationEvent {
  message: string
  level: 'info' | 'warn' | 'error'
  occurred_at: string
}

export interface ChatToolCallDeltaEvent {
  session_id: string
  request_id: string
  call_id: string
  index: number
  tool_name: string
  arguments_summary: string
  truncated: boolean
  received_at: string
}

export interface ChatToolOutputEvent {
  session_id: string
  request_id: string
  call_id: string
  chunk: string
  received_at: string
}

export type InteractivePromptKind = 'confirm' | 'question'

export interface InteractivePromptRequestedEvent {
  request_id: string
  kind: InteractivePromptKind
  title: string
  message: string
  requested_at: string
}

export interface SessionSummary {
  id: string
  model_id: string
  provider: string
  created_at: string
  updated_at: string
  status: string
  message_count: number
  total_tokens: number
  cost: number
}

export interface SessionsListedEvent {
  sessions: SessionSummary[]
  next_cursor?: string
}

export interface SessionLoadedEvent {
  state: ChatSessionState
}

export interface SessionDeletedEvent {
  session_id: string
}

// ── Commands (client -> server) ─────────────────────────────────────────────

export interface SubmitChatPromptCommand {
  session_id: string
  request_id: string
  prompt: string
  submitted_at: string
}

export interface CancelChatRequestCommand {
  session_id: string
  request_id: string
}

/** Partial update to session settings; only set fields are applied. */
export interface ChatSessionPatch {
  /** Model reference. When switching, set model.id to the desired model ID. */
  model?: ChatModelRef
  /** Provider name to switch to. When set alongside model, the backend treats
   * this as a full provider/model change. */
  provider?: string
  system_prompt?: string
  max_tokens?: number
  temperature?: number
  reasoning_effort?: string
}

export interface UpdateChatSessionCommand {
  session_id: string
  patch: ChatSessionPatch
}

export interface RespondInteractivePromptCommand {
  request_id: string
  response?: string
  confirmed: boolean
  canceled: boolean
  responded_at: string
}

export interface ListSessionsCommand {
  limit: number
  cursor?: string
}

export interface LoadSessionCommand {
  session_id: string
}

export interface DeleteSessionCommand {
  session_id: string
}

export interface ExportSessionCommand {
  session_id: string
  format: string
}

/** Build a command envelope for the wire. */
export function command<T>(type: string, payload: T): Envelope<T> {
  return { type, payload }
}
