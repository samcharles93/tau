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
  /** Available provider names, if the backend advertises them. */
  providers?: string[]
  commands?: CommandRef[]
  skills?: SkillInfo[]
  extension_commands?: ExtensionCommand[]
}

export interface CommandRef {
  name: string
  label: string
  description?: string
  accepts_args?: boolean
}

export interface ExtensionCommand {
  name: string
  description?: string
  extension_name: string
  args_hint?: string
  subcommands?: ExtensionCommand[]
}

export interface ExtensionCommandsChangedEvent {
  commands: ExtensionCommand[]
  occurred_at: string
}

export interface ExtensionsReloadedEvent {
  result: {
    extension_count: number
    commands: ExtensionCommand[]
  }
  occurred_at: string
}

// ── Events (server -> client) ───────────────────────────────────────────────

export interface ChatModelRef {
  id: string
  url?: string
  /** Provider the model belongs to, so selecting it can switch the session's
   * provider too (aggregated, cross-provider model list). */
  provider?: string
  /** Maximum context window in tokens, when the backend advertises it. */
  context_window?: number
  /** Per-1M-token pricing, when the backend advertises it. */
  cost?: ChatCost
  /** Supported reasoning effort levels, e.g. ["low","medium","high"]. Present
   * only when the model exposes selectable effort. Empty/absent means reasoning
   * is either unsupported or fixed (no user control). */
  reasoning_efforts?: string[]
}

export interface ChatCost {
  input?: number
  output?: number
  cache_read?: number
  cache_write?: number
}

export interface ChatUsage {
  prompt_tokens?: number
  completion_tokens?: number
  output_tokens?: number
  total_tokens?: number
  /** Portion of prompt_tokens served from a provider cache (OpenAI's
   * automatic prefix cache, or an Anthropic cache_control read), billed
   * at a reduced rate. */
  cached_tokens?: number
  /** Anthropic-specific: tokens written to the cache on this call,
   * billed at a premium over normal input tokens. */
  cache_creation_tokens?: number
}

export interface ChatParameters {
  max_tokens: number
  temperature: number
  reasoning_effort?: string
}

export interface ChatSessionState {
  session_id: string
  provider?: string
  model: ChatModelRef
  parameters: ChatParameters
  status: string
  messages: ChatMessage[]
  pending_assistant?: string
  active_request_id?: string
  last_usage?: ChatUsage
}

export interface ChatMessage {
  id?: string
  role: 'system' | 'user' | 'assistant' | 'tool'
  content?: string
  reasoning_content?: string
  tool_calls?: ChatToolCall[]
  tool_call_id?: string
}

export interface ChatToolCall {
  id: string
  type: string // always "function"
  function: ChatFunctionCall
}

export interface ChatFunctionCall {
  name: string
  arguments: string // JSON string
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
  duration: number
  result_summary: string
  is_error: boolean
  truncated: boolean
  completed_at: string
  /** Tool-specific structured data (e.g. child agent result for agent tools). */
  details?: ChildAgentResultDetails
}

export interface ChildAgentResultDetails {
  status: ChildAgentStatus
  instance_id: string
  spec_name?: string
  session_id?: string
  usage?: {
    turns: number
    input_tokens: number
    output_tokens: number
    cost?: number
  }
  error?: string
  partial?: boolean
  duration_ms?: number
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

export interface ChatResponseCancelledEvent {
  state: ChatSessionState
  request_id: string
  cancelled_at: string
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

// ── Child agent events ────────────────────────────────────────────────────

export type ChildAgentStatus = 'working' | 'completed' | 'failed' | 'cancelled' | 'budget_exhausted' | 'timed_out'

// Keep in sync with ChildAgentStatus.IsTerminal() in internal/chat/types.go.
// New status values added to the Go enum must be mirrored here, otherwise
// ToolCard.vue renders the live/working layout forever for that status.
export function isChildTerminal(status: ChildAgentStatus): boolean {
  switch (status) {
    case 'completed':
    case 'failed':
    case 'cancelled':
    case 'budget_exhausted':
    case 'timed_out':
      return true
    default:
      return false
  }
}

export interface ChildAgentStateEvent {
  instance_id: string
  call_id: string
  spec_name: string
  activity: string
  turns: number
  input_tokens: number
  output_tokens: number
  elapsed_ms: number
  status: ChildAgentStatus
  error?: string
  partial?: boolean
  occurred_at: string
}

// ChildAgentResult mirrors the child agent state stored in the session
// store alongside tool calls.
export interface ChildAgentResult {
  instance_id: string
  spec_name: string
  status: ChildAgentStatus
  activity: string
  turns: number
  tokens: number
  duration_ms: number
  error_msg?: string
  session_id?: string
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
  parent_session_id?: string
  agent_instance_id?: string;
    agent_spec_name?: string;
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

export interface SkillsChangedEvent {
  skills: SkillInfo[]
}

export interface CommandsChangedEvent {
  commands: CommandRef[]
  occurred_at: string
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

export interface ResetChatSessionCommand {
  session_id: string
  requested_at: string
}

export interface ReloadExtensionsCommand {
  requested_at: string
}

/** Build a command envelope for the wire. */
export function command<T>(type: string, payload: T): Envelope<T> {
  return { type, payload }
}
