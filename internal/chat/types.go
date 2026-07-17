package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samcharles93/tau/internal/config"
)

const (
	defaultChatSystemPrompt = "You are a helpful assistant."
	defaultChatMaxTokens    = 0
	defaultChatTemperature  = 0.7
)

func DefaultParameters() ChatParameters {
	return defaultChatParameters()
}

// ClampMaxTokensForModel caps a requested output-token budget to the selected
// model's configured output ceiling. A zero requested value means provider
// default and stays zero.
func ClampMaxTokensForModel(maxTokens int, model ChatModelRef) int {
	if maxTokens <= 0 {
		return maxTokens
	}
	if model.Config.MaxTokens > 0 && maxTokens > model.Config.MaxTokens {
		return model.Config.MaxTokens
	}
	return maxTokens
}

// ChatRole is the role value sent to the OpenAI-compatible chat endpoint.
type ChatRole string

const (
	ChatRoleSystem    ChatRole = "system"
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleTool      ChatRole = "tool"
)

func (r ChatRole) Valid() bool {
	switch r {
	case ChatRoleSystem, ChatRoleUser, ChatRoleAssistant, ChatRoleTool:
		return true
	default:
		return false
	}
}

// ChatSessionStatus describes the current lifecycle state of a chat session.
type ChatSessionStatus string

const (
	ChatSessionIdle       ChatSessionStatus = "idle"
	ChatSessionStreaming  ChatSessionStatus = "streaming"
	ChatSessionCancelling ChatSessionStatus = "cancelling"
	ChatSessionClosed     ChatSessionStatus = "closed"
)

// ChatMessage is the canonical conversation item used for request history.
type ChatMessage struct {
	// ID identifies this message across the TUI, WebUI, and persisted
	// storage - e.g. so a client can address a specific message (a
	// right-click context menu action, a future edit/delete). It is empty
	// for the transient system message synthesised in RequestMessages
	// (never persisted or rendered) and for messages persisted before
	// per-message IDs were tracked; callers must treat "" as "no stable
	// identity available" rather than assuming it's always populated.
	ID               string              `json:"id,omitempty"`
	Role             ChatRole            `json:"role"`
	Content          string              `json:"content,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatToolCall      `json:"tool_calls,omitempty"`
	ToolCallID       string              `json:"tool_call_id,omitempty"`
	ToolResult       *ToolResultMetadata `json:"tool_result,omitempty"`
	// CreatedAt records when the message was appended to the conversation.
	// It is zero for the transient system message synthesised in
	// RequestMessages and for messages persisted before per-message
	// timestamps were tracked (the store falls back to a synthesised time).
	CreatedAt time.Time `json:"created_at,omitzero"`
}

// ToolResultMetadata records authoritative execution facts for a tool result.
// Content remains the model-visible result; this metadata is for persistence,
// observability, and analysis without heuristically parsing that content.
type ToolResultMetadata struct {
	ToolName    string        `json:"tool_name"`
	RequestID   string        `json:"request_id,omitempty"`
	Status      string        `json:"status"`
	ErrorKind   string        `json:"error_kind,omitempty"`
	Duration    time.Duration `json:"duration"`
	Truncated   bool          `json:"truncated,omitempty"`
	ResultBytes int           `json:"result_bytes"`
	StartedAt   time.Time     `json:"started_at,omitzero"`
	CompletedAt time.Time     `json:"completed_at,omitzero"`
}

// NewMessageID generates a UUIDv7 message ID, falling back to a
// nanosecond-timestamp string if the random source fails - mirrors
// internal/app's newID and internal/tui2's newRequestID.
func NewMessageID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

func (m ChatMessage) Validate() error {
	if !m.Role.Valid() {
		return fmt.Errorf("invalid chat role %q", m.Role)
	}
	// Tool result messages carry a tool_call_id instead of requiring content.
	if m.Role == ChatRoleTool {
		if strings.TrimSpace(m.ToolCallID) == "" {
			return errors.New("tool message requires tool_call_id")
		}
		return nil
	}
	// Assistant messages may have tool_calls without content.
	if m.Role == ChatRoleAssistant && len(m.ToolCalls) > 0 {
		return nil
	}
	if strings.TrimSpace(m.Content) == "" {
		return errors.New("chat message content is required")
	}
	return nil
}

// ChatToolCall is an OpenAI-compatible tool call from the assistant.
type ChatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // always "function"
	Function ChatFunctionCall `json:"function"`
}

// ChatFunctionCall is the function name + arguments within a tool call.
type ChatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ChatToolDef is the OpenAI function-calling tool definition sent in requests.
type ChatToolDef struct {
	Type     string              `json:"type"` // "function"
	Function ChatToolDefFunction `json:"function"`
}

// ChatToolDefFunction describes one function tool for the API request.
type ChatToolDefFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ChatModelRef identifies the selected model route for a session.
//
// Provider tags the model with the provider it was discovered from so the TUI
// can switch the session's provider alongside the model when the user selects a
// model from the aggregated, cross-provider list. It is empty for models that
// were not aggregated (e.g. a single-provider headless run).
type ChatModelRef struct {
	ID       string             `json:"id"`
	URL      string             `json:"url"`
	Provider string             `json:"provider,omitempty"`
	Config   config.ModelConfig `json:"-"`
}

func (m ChatModelRef) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("chat model id is required")
	}
	return nil
}

// MarshalJSON surfaces the resolved model's context-window size, pricing, and
// reasoning effort levels to wire consumers (e.g. the web UI). The Config
// field itself stays off the wire; only these derived fields are exposed
// alongside the existing id/url.
func (m ChatModelRef) MarshalJSON() ([]byte, error) {
	type alias ChatModelRef
	return json.Marshal(struct {
		alias
		ContextWindow    int               `json:"context_window,omitempty"`
		Cost             config.CostConfig `json:"cost,omitzero"`
		ReasoningEfforts []string          `json:"reasoning_efforts,omitempty"`
	}{
		alias:            alias(m),
		ContextWindow:    m.Config.ContextWindow,
		Cost:             m.Config.Cost,
		ReasoningEfforts: m.Config.ReasoningEfforts,
	})
}

// ChatParameters are the tunable per-request controls for a session.
type ChatParameters struct {
	MaxTokens       int     `json:"max_tokens"`
	Temperature     float64 `json:"temperature"`
	ReasoningEffort string  `json:"reasoning_effort,omitempty"`
}

func defaultChatParameters() ChatParameters {
	return ChatParameters{
		MaxTokens:   defaultChatMaxTokens,
		Temperature: defaultChatTemperature,
	}
}

func (p ChatParameters) withDefaults() ChatParameters {
	defaults := defaultChatParameters()
	if p.Temperature == 0 {
		p.Temperature = defaults.Temperature
	}
	return p
}

func (p ChatParameters) Validate() error {
	if p.MaxTokens < 0 {
		return errors.New("max_tokens must be greater than or equal to 0")
	}
	if p.Temperature < 0 || p.Temperature > 2 {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

// ChatUsage stores token counts surfaced by the backend.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	// CachedTokens is the portion of PromptTokens served from a provider
	// cache (OpenAI's automatic prefix cache, or an Anthropic cache_control
	// read), billed at a reduced rate. Zero when the provider didn't
	// report it or nothing was served from cache.
	CachedTokens int `json:"cached_tokens,omitempty"`
	// CacheCreationTokens is Anthropic-specific: tokens written to the
	// cache on this call, billed at a premium over normal input tokens.
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
}

// ChatSessionConfig is the immutable setup for a newly created session.
type ChatSessionConfig struct {
	Provider     config.ProviderConfig `json:"-"`
	Model        ChatModelRef          `json:"model"`
	SystemPrompt string                `json:"system_prompt"`
	Parameters   ChatParameters        `json:"parameters"`
	// ParentSessionID links this session to its parent for conversation
	// branching / traceability. Empty string means this is a root session.
	// The relationship is immutable after session creation.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// AgentInstanceID links this session to the agent instance that ran it.
	// Empty for pre-migration sessions. Immutable after creation.
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
}

func (c ChatSessionConfig) withDefaults() ChatSessionConfig {
	c.Parameters = c.Parameters.withDefaults()
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = defaultChatSystemPrompt
	}
	return c
}

func (c ChatSessionConfig) Validate() error {
	// An empty provider is permitted, the same way an empty model already is:
	// the session launches unconfigured and the user picks a provider with
	// /provider (and then a model with /model). A partially-set provider -
	// one field present, the other missing - is still rejected, since that
	// can only be a real mistake, not a deliberate "nothing configured yet".
	name := strings.TrimSpace(c.Provider.Name)
	baseURL := strings.TrimSpace(c.Provider.BaseURL)
	if (name == "") != (baseURL == "") {
		return errors.New("chat provider is required")
	}
	// A model that is set must still be valid, whether or not a provider is.
	if strings.TrimSpace(c.Model.ID) != "" {
		if err := c.Model.Validate(); err != nil {
			return err
		}
	}
	return c.Parameters.Validate()
}

// ChatSessionPatch holds optional config changes for an existing session.
type ChatSessionPatch struct {
	Model           *ChatModelRef `json:"model,omitempty"`
	Provider        *string       `json:"provider,omitempty"`
	SystemPrompt    *string       `json:"system_prompt,omitempty"`
	MaxTokens       *int          `json:"max_tokens,omitempty"`
	Temperature     *float64      `json:"temperature,omitempty"`
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"`
}

func (p ChatSessionPatch) Validate() error {
	if p.Model != nil {
		if err := p.Model.Validate(); err != nil {
			return err
		}
	}
	if p.MaxTokens != nil && *p.MaxTokens < 0 {
		return errors.New("max_tokens must be greater than or equal to 0")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

// ChatCommand is the input contract from the UI to the runtime.
type ChatCommand interface{ IsChatCommand() }

type StartChatSessionCommand struct {
	SessionID string            `json:"session_id"`
	Config    ChatSessionConfig `json:"config"`
}

func (StartChatSessionCommand) IsChatCommand() {}

type SubmitChatPromptCommand struct {
	SessionID   string    `json:"session_id"`
	RequestID   string    `json:"request_id"`
	Prompt      string    `json:"prompt"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (SubmitChatPromptCommand) IsChatCommand() {}

type UpdateChatSessionCommand struct {
	SessionID string           `json:"session_id"`
	Patch     ChatSessionPatch `json:"patch"`
}

func (UpdateChatSessionCommand) IsChatCommand() {}

type CancelChatRequestCommand struct {
	SessionID   string    `json:"session_id"`
	RequestID   string    `json:"request_id,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

func (CancelChatRequestCommand) IsChatCommand() {}

type ResetChatSessionCommand struct {
	SessionID   string    `json:"session_id"`
	RequestedAt time.Time `json:"requested_at"`
}

func (ResetChatSessionCommand) IsChatCommand() {}

type CloseChatSessionCommand struct {
	SessionID   string    `json:"session_id"`
	RequestedAt time.Time `json:"requested_at"`
}

func (CloseChatSessionCommand) IsChatCommand() {}

type ReloadExtensionsCommand struct {
	RequestedAt time.Time `json:"requested_at"`
}

func (ReloadExtensionsCommand) IsChatCommand() {}

type RunExtensionCommandCommand struct {
	Name        string    `json:"name"`
	Args        string    `json:"args,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

func (RunExtensionCommandCommand) IsChatCommand() {}

type SteerChatPromptCommand struct {
	SessionID   string    `json:"session_id"`
	RequestID   string    `json:"request_id"`
	Prompt      string    `json:"prompt"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (SteerChatPromptCommand) IsChatCommand() {}

type RespondInteractivePromptCommand struct {
	RequestID   string    `json:"request_id"`
	Response    string    `json:"response,omitempty"`
	Confirmed   bool      `json:"confirmed"`
	Canceled    bool      `json:"canceled"`
	RespondedAt time.Time `json:"responded_at"`
}

func (RespondInteractivePromptCommand) IsChatCommand() {}

// ListSessionsCommand requests a paginated list of saved sessions. Silent,
// when true, is echoed back on the resulting SessionsListedEvent so a caller
// that's just refreshing a cache (e.g. a completion dropdown's data source)
// can be distinguished from an explicit user-facing "/session list" that
// should print its result to scrollback.
type ListSessionsCommand struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
	Silent bool   `json:"silent,omitempty"`
}

func (ListSessionsCommand) IsChatCommand() {}

// LoadSessionCommand requests loading a saved session by ID. RuntimeSessionID
// optionally identifies the currently-running session whose provider/model
// config should be used as a template when activating a loaded session from the
// TUI.
type LoadSessionCommand struct {
	SessionID        string `json:"session_id"`
	RuntimeSessionID string `json:"runtime_session_id,omitempty"`
}

func (LoadSessionCommand) IsChatCommand() {}

// LoadChildTranscriptCommand requests a read-only load of a finished child
// agent's session, for TUI/WebUI drill-down. Unlike LoadSessionCommand, it
// must never replace the runtime's active session state.
type LoadChildTranscriptCommand struct {
	SessionID string `json:"session_id"`
}

func (LoadChildTranscriptCommand) IsChatCommand() {}

// DeleteSessionCommand deletes a saved session and its messages.
type DeleteSessionCommand struct {
	SessionID string `json:"session_id"`
}

func (DeleteSessionCommand) IsChatCommand() {}

// ExportSessionCommand exports a session to a file or stdout.
type ExportSessionCommand struct {
	SessionID string `json:"session_id"`
	Format    string `json:"format"`           // "jsonl" or "html"
	Output    string `json:"output,omitempty"` // file path, empty = stdout
}

func (ExportSessionCommand) IsChatCommand() {}

// RunSkillCommand activates a skill by name from the catalog (user-invoked
// via the /skill:<name> slash command). The coordinator activates the skill,
// injects its instructions into the session system prompt, and emits a
// Skill-tool-style box event so the TUI renders the lilac "loaded" feedback.
type RunSkillCommand struct {
	SessionID   string    `json:"session_id"`
	SkillName   string    `json:"skill_name"`
	Args        string    `json:"args,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

func (RunSkillCommand) IsChatCommand() {}

// RunAgentCommand activates a built-in agent command (e.g. /plan, /research)
// by name (user-invoked via the matching slash command). The coordinator
// renders the agent's prompt template, replaces the session's system prompt
// with it, and restricts the tool registry per the agent's spec. Any trailing
// prompt text typed alongside the command is submitted separately via
// SubmitChatPromptCommand so it goes through the normal turn/queue bookkeeping.
type RunAgentCommand struct {
	SessionID   string    `json:"session_id"`
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requested_at"`
}

func (RunAgentCommand) IsChatCommand() {}

// RunBashCommand runs a "!" (or "!!") bash-mode command entered directly at
// the chat input, outside the normal LLM turn loop. CallID is generated by
// the TUI (not the coordinator) so it can be recorded before the command is
// even sent, closing the window where a ChatToolExecutionStartedEvent could
// otherwise arrive before the TUI knows which CallID is "ours." Exclude
// corresponds to the "!!" prefix: the command still runs and renders exactly
// like a normal "!" command, but its result is not appended to the
// conversation history the model sees.
type RunBashCommand struct {
	SessionID   string    `json:"session_id"`
	CallID      string    `json:"call_id"`
	Command     string    `json:"command"`
	Exclude     bool      `json:"exclude"`
	RequestedAt time.Time `json:"requested_at"`
}

func (RunBashCommand) IsChatCommand() {}

// CancelBashCommand stops the session's in-flight bash-mode command, if any.
type CancelBashCommand struct {
	SessionID   string    `json:"session_id"`
	RequestedAt time.Time `json:"requested_at"`
}

func (CancelBashCommand) IsChatCommand() {}

// ReloadSkillsCommand triggers a skills catalog refresh from disk.
type ReloadSkillsCommand struct {
	RequestedAt time.Time
}

func (ReloadSkillsCommand) IsChatCommand() {}

// ListSkillsCommand requests a listing of available skills.
type ListSkillsCommand struct {
	RequestedAt time.Time
}

func (ListSkillsCommand) IsChatCommand() {}

// ChatEvent is the output contract from the runtime back to the UI.
type ChatEvent interface{ IsChatEvent() }

// ChatRuntime is the interface the TUI uses to interact with the coordinator.
// Events are published through the event bus; the TUI subscribes directly.
type ChatRuntime interface {
	Send(cmd ChatCommand) error
	Close()
}

type ExtensionDiagnostic struct {
	Path          string `json:"path,omitempty"`
	ExtensionName string `json:"extension_name,omitempty"`
	Severity      string `json:"severity"`
	Message       string `json:"message"`
}

type ExtensionReloadResult struct {
	ExtensionCount int                   `json:"extension_count"`
	Diagnostics    []ExtensionDiagnostic `json:"diagnostics,omitempty"`
	Commands       []ExtensionCommand    `json:"commands,omitempty"`
}

type ExtensionReloader interface {
	ReloadExtensions(ctx context.Context, idle bool) (ExtensionReloadResult, error)
	ExtensionCommands() []ExtensionCommand
	RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (output string, view *ExtensionView, err error)
}

type ChatSessionSnapshotEvent struct {
	State ChatSessionState `json:"state"`
}

func (ChatSessionSnapshotEvent) IsChatEvent() {}

type ChatResponseStartedEvent struct {
	SessionID string    `json:"session_id"`
	RequestID string    `json:"request_id"`
	StartedAt time.Time `json:"started_at"`
}

func (ChatResponseStartedEvent) IsChatEvent() {}

type ChatResponseDeltaEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id"`
	Delta      string    `json:"delta"`
	Snapshot   string    `json:"snapshot"`
	ReceivedAt time.Time `json:"received_at"`
}

func (ChatResponseDeltaEvent) IsChatEvent() {}

type ChatReasoningDeltaEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id"`
	Delta      string    `json:"delta"`
	Snapshot   string    `json:"snapshot"`
	ReceivedAt time.Time `json:"received_at"`
}

func (ChatReasoningDeltaEvent) IsChatEvent() {}

type ChatToolCallDeltaEvent struct {
	SessionID        string    `json:"session_id"`
	RequestID        string    `json:"request_id"`
	CallID           string    `json:"call_id"`
	Index            int       `json:"index"`
	ToolName         string    `json:"tool_name"`
	ArgumentsSummary string    `json:"arguments_summary"`
	Truncated        bool      `json:"truncated"`
	ReceivedAt       time.Time `json:"received_at"`
}

func (ChatToolCallDeltaEvent) IsChatEvent() {}

type ChatToolExecutionStartedEvent struct {
	SessionID        string `json:"session_id"`
	RequestID        string `json:"request_id"`
	CallID           string `json:"call_id"`
	ToolName         string `json:"tool_name"`
	ArgumentsSummary string `json:"arguments_summary"`
	// Summary is a short, model-authored one-liner describing what this
	// call is doing (e.g. "Checking installed packages"), sourced from an
	// optional "summary" argument injected into every tool's schema. Empty
	// when the model didn't supply one - never populated with anything
	// that failed sanitization, so consumers can render it as-is.
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

func (ChatToolExecutionStartedEvent) IsChatEvent() {}

type ChatToolExecutionCompletedEvent struct {
	SessionID     string        `json:"session_id"`
	RequestID     string        `json:"request_id"`
	CallID        string        `json:"call_id"`
	ToolName      string        `json:"tool_name"`
	Status        string        `json:"status"`
	Duration      time.Duration `json:"duration"`
	ResultSummary string        `json:"result_summary"`
	IsError       bool          `json:"is_error"`
	Truncated     bool          `json:"truncated"`
	CompletedAt   time.Time     `json:"completed_at"`

	// Details carries tool-specific structured data (e.g.
	// tools.DiffDetails for edit/write) alongside the plain-text summary,
	// so consumers like the TUI can render richer views. nil when the tool
	// has nothing structured to offer.
	Details any `json:"details,omitempty"`
}

func (ChatToolExecutionCompletedEvent) IsChatEvent() {}

type ChatToolOutputEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id"`
	CallID     string    `json:"call_id"`
	Chunk      string    `json:"chunk"`
	ReceivedAt time.Time `json:"received_at"`
}

func (ChatToolOutputEvent) IsChatEvent() {}

type ChatResponseCompletedEvent struct {
	State        ChatSessionState `json:"state"`
	RequestID    string           `json:"request_id"`
	FinishReason string           `json:"finish_reason"`
	Usage        ChatUsage        `json:"usage"`
	CompletedAt  time.Time        `json:"completed_at"`
	// BudgetExhausted is true when the turn ended due to max-turns or
	// max-tokens budget exhaustion (per-task limit, per P2.4).
	BudgetExhausted bool `json:"budget_exhausted,omitempty"`
	// TimedOut is true when the turn ended due to a wall-clock deadline
	// or timeout (per-task limit, per P2.4).
	TimedOut bool `json:"timed_out,omitempty"`
	// Partial is true when the response contains best-effort partial output
	// from a limit-breached turn.
	Partial bool `json:"partial,omitempty"`
}

func (ChatResponseCompletedEvent) IsChatEvent() {}

type ChatResponseCancelledEvent struct {
	State       ChatSessionState `json:"state"`
	RequestID   string           `json:"request_id"`
	CancelledAt time.Time        `json:"cancelled_at"`
}

func (ChatResponseCancelledEvent) IsChatEvent() {}

type ChatRuntimeErrorEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id,omitempty"`
	Message    string    `json:"message"`
	Fatal      bool      `json:"fatal"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ChatRuntimeErrorEvent) IsChatEvent() {}

type ChatNotificationLevel string

const (
	ChatNotificationInfo  ChatNotificationLevel = "info"
	ChatNotificationWarn  ChatNotificationLevel = "warn"
	ChatNotificationError ChatNotificationLevel = "error"
)

type ChatNotificationEvent struct {
	Message    string                `json:"message"`
	Level      ChatNotificationLevel `json:"level"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (ChatNotificationEvent) IsChatEvent() {}

type ExtensionsReloadedEvent struct {
	Result     ExtensionReloadResult `json:"result"`
	OccurredAt time.Time             `json:"occurred_at"`
}

func (ExtensionsReloadedEvent) IsChatEvent() {}

type ExtensionCommand struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	ExtensionName string `json:"extension_name,omitempty"`
	// ArgsHint is an optional usage hint shown in completions, e.g. "<server>".
	ArgsHint string `json:"args_hint,omitempty"`
	// Subcommands are nested sub-actions of this command, e.g. list / reconnect
	// / reload under an `mcp` group. Empty for flat commands.
	Subcommands []ExtensionCommand `json:"subcommands,omitempty"`
}

type ExtensionCommandsChangedEvent struct {
	Commands   []ExtensionCommand `json:"commands"`
	OccurredAt time.Time          `json:"occurred_at"`
}

func (ExtensionCommandsChangedEvent) IsChatEvent() {}

// CommandRef is a lightweight command descriptor published by the registry
// so that consumers (TUI, etc.) can render completions without importing
// the full registry package.
type CommandRef struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	AcceptsArgs bool   `json:"accepts_args,omitempty"`
}

// CommandsChangedEvent is published by the command registry whenever the
// set of available non-extension commands changes (startup, reload, etc.).
// Extension commands continue to arrive via [ExtensionCommandsChangedEvent].
type CommandsChangedEvent struct {
	Commands   []CommandRef `json:"commands"`
	OccurredAt time.Time    `json:"occurred_at"`
}

func (CommandsChangedEvent) IsChatEvent() {}

// SkillInfo is a lightweight skill descriptor published over the WebSocket
// bridge to the web UI. It omits security-sensitive fields such as file paths.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

// SkillsChangedEvent is published when the skill catalog changes following a
// refresh cycle. Consumers use it to keep their skill listings in sync.
type SkillsChangedEvent struct {
	Skills []SkillInfo `json:"skills"`
}

func (SkillsChangedEvent) IsChatEvent() {}

// ChildAgentStateEvent is published on the parent bus when an agent-scoped
// event or usage update arrives from a spawned child. It carries the live
// state of a running child agent so both UIs can render the state block
// from the same data (see docs/specs/agents/05-ui.md). Terminal states
// collapse to a summary line; the UI chooses how much to show.
type ChildAgentStateEvent struct {
	InstanceID   string    `json:"instance_id"`   // child instance, e.g. "research#k3v9qp"
	CallID       string    `json:"call_id"`       // spawning agent tool call id
	SpecName     string    `json:"spec_name"`     // e.g. "research"
	Activity     string    `json:"activity"`      // "thinking" or tool verb + argument
	Turns        int       `json:"turns"`         // from agent.usage
	InputTokens  int       `json:"input_tokens"`  // cumulative from agent.usage
	OutputTokens int       `json:"output_tokens"` // cumulative from agent.usage
	ElapsedMs    int64     `json:"elapsed_ms"`    // wall clock since spawn
	Status       string    `json:"status"`        // "working", "completed", "failed", "cancelled", "budget_exhausted", "timed_out"
	Error        string    `json:"error,omitempty"`
	Partial      bool      `json:"partial,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

func (ChildAgentStateEvent) IsChatEvent() {}

// ChildAgentMessageEvent forwards a child agent's inner chat event to the
// parent bus so the TUI/WebUI can render a live transcript for a running
// child, mirroring the events the child's own session would emit.
type ChildAgentMessageEvent struct {
	InstanceID string    `json:"instance_id"`
	CallID     string    `json:"call_id"`
	Event      ChatEvent `json:"event"`
}

func (ChildAgentMessageEvent) IsChatEvent() {}

// WidgetKind identifies which field of Widget is populated.
type WidgetKind string

const (
	WidgetKindText     WidgetKind = "text"
	WidgetKindStack    WidgetKind = "stack"
	WidgetKindKeyValue WidgetKind = "key_value"
	WidgetKindList     WidgetKind = "list"
	WidgetKindTable    WidgetKind = "table"
	WidgetKindProgress WidgetKind = "progress"
	WidgetKindDivider  WidgetKind = "divider"
	WidgetKindStatus   WidgetKind = "status"
)

// StyleTone is a semantic presentation hint resolved to tau's theme palette
// host-side, so plugin-rendered widgets stay visually consistent across
// theme changes. FgHex/BgHex are an escape hatch, not the primary mechanism.
type StyleTone string

const (
	ToneDefault StyleTone = "default"
	ToneInfo    StyleTone = "info"
	ToneSuccess StyleTone = "success"
	ToneWarn    StyleTone = "warn"
	ToneError   StyleTone = "error"
	ToneMuted   StyleTone = "muted"
)

type Style struct {
	Tone      StyleTone `json:"tone,omitempty"`
	FgHex     string    `json:"fg_hex,omitempty"`
	BgHex     string    `json:"bg_hex,omitempty"`
	Bold      bool      `json:"bold,omitempty"`
	Dim       bool      `json:"dim,omitempty"`
	Italic    bool      `json:"italic,omitempty"`
	Underline bool      `json:"underline,omitempty"`
}

type TextWidget struct {
	Text  string `json:"text"`
	Style *Style `json:"style,omitempty"`
}

type StackDirection string

const (
	StackVertical   StackDirection = "vertical"
	StackHorizontal StackDirection = "horizontal"
)

type StackWidget struct {
	Direction StackDirection `json:"direction,omitempty"`
	Children  []Widget       `json:"children,omitempty"`
	Gap       int            `json:"gap,omitempty"`
}

type KeyValueEntry struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	ValueStyle *Style `json:"value_style,omitempty"`
}

type KeyValueWidget struct {
	Entries []KeyValueEntry `json:"entries,omitempty"`
}

type ListWidget struct {
	Items   []string `json:"items,omitempty"`
	Ordered bool     `json:"ordered,omitempty"`
	Style   *Style   `json:"style,omitempty"`
}

type TableRow struct {
	Cells []string `json:"cells,omitempty"`
}

type TableWidget struct {
	Headers []string   `json:"headers,omitempty"`
	Rows    []TableRow `json:"rows,omitempty"`
}

type ProgressWidget struct {
	Label    string  `json:"label,omitempty"`
	Fraction float64 `json:"fraction"` // negative = indeterminate
	Style    *Style  `json:"style,omitempty"`
}

type DividerWidget struct {
	Label string `json:"label,omitempty"`
}

type StatusState string

const (
	StatusRunning StatusState = "running"
	StatusSuccess StatusState = "success"
	StatusFailed  StatusState = "failed"
	StatusNeutral StatusState = "neutral"
)

type StatusWidget struct {
	State  StatusState `json:"state,omitempty"`
	Label  string      `json:"label,omitempty"`
	Detail string      `json:"detail,omitempty"`
}

// Widget is one renderable element of an ExtensionView. Exactly one of the
// kind-specific fields is populated, selected by Kind.
type Widget struct {
	Kind     WidgetKind      `json:"kind"`
	Text     *TextWidget     `json:"text,omitempty"`
	Stack    *StackWidget    `json:"stack,omitempty"`
	KeyValue *KeyValueWidget `json:"key_value,omitempty"`
	List     *ListWidget     `json:"list,omitempty"`
	Table    *TableWidget    `json:"table,omitempty"`
	Progress *ProgressWidget `json:"progress,omitempty"`
	Divider  *DividerWidget  `json:"divider,omitempty"`
	Status   *StatusWidget   `json:"status,omitempty"`
}

// ExtensionView is a structured panel a plugin renders into the host UI,
// either attached to a command result (sync) or pushed independently via
// ExtensionViewRenderedEvent (async). Re-rendering a View with the same ID
// replaces its content in place.
type ExtensionView struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"`
	Widgets []Widget `json:"widgets,omitempty"`
	Style   *Style   `json:"style,omitempty"`
}

type ExtensionCommandResultEvent struct {
	Name       string         `json:"name"`
	Output     string         `json:"output"`
	View       *ExtensionView `json:"view,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
}

func (ExtensionCommandResultEvent) IsChatEvent() {}

// ExtensionViewRenderedEvent is published when a plugin opens or updates a
// persistent panel via Host.RenderView. ViewID is host-qualified
// (pluginName + ":" + the plugin-local View.ID) to prevent collisions and
// spoofing between plugins.
type ExtensionViewRenderedEvent struct {
	PluginName string        `json:"plugin_name"`
	ViewID     string        `json:"view_id"`
	View       ExtensionView `json:"view"`
	OccurredAt time.Time     `json:"occurred_at"`
}

func (ExtensionViewRenderedEvent) IsChatEvent() {}

// ExtensionViewClosedEvent is published when a plugin closes a panel via
// Host.CloseView, or when the host closes it on the plugin's behalf
// (unload/reload). ViewID is host-qualified, matching
// ExtensionViewRenderedEvent.ViewID.
type ExtensionViewClosedEvent struct {
	PluginName string    `json:"plugin_name"`
	ViewID     string    `json:"view_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ExtensionViewClosedEvent) IsChatEvent() {}

type InteractivePromptKind string

const (
	InteractivePromptConfirm  InteractivePromptKind = "confirm"
	InteractivePromptQuestion InteractivePromptKind = "question"
)

type InteractivePromptRequestedEvent struct {
	RequestID   string                `json:"request_id"`
	Kind        InteractivePromptKind `json:"kind"`
	Title       string                `json:"title"`
	Message     string                `json:"message"`
	RequestedAt time.Time             `json:"requested_at"`
}

func (InteractivePromptRequestedEvent) IsChatEvent() {}

// SessionSummary is the wire type for session list rows. It carries
// metadata without the full message history.
type SessionSummary struct {
	ID              string    `json:"id"`
	ModelID         string    `json:"model_id"`
	Provider        string    `json:"provider"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Status          string    `json:"status"`
	MessageCount    int       `json:"message_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	TotalTokens     int       `json:"total_tokens"`
	Cost            float64   `json:"cost"`
	DurationMs      int64     `json:"duration_ms"`
	ToolCalls       int       `json:"tool_calls"`
	ToolErrors      int       `json:"tool_errors"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	AgentSpecName   string    `json:"agent_spec_name,omitempty"`
}

// SessionsListedEvent carries paginated session summaries back to the TUI.
// Silent mirrors the triggering ListSessionsCommand.Silent - see its doc
// comment.
type SessionsListedEvent struct {
	Sessions   []SessionSummary `json:"sessions"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Silent     bool             `json:"silent,omitempty"`
}

func (SessionsListedEvent) IsChatEvent() {}

// SessionLoadedEvent carries a fully reconstructed session state.
type SessionLoadedEvent struct {
	State ChatSessionState `json:"state"`
}

func (SessionLoadedEvent) IsChatEvent() {}

// ChildTranscriptLoadedEvent carries a finished child agent's message
// history for read-only drill-down. It intentionally omits the rest of
// ChatSessionState (provider, model, status) since the viewer never treats
// the child as the active session.
type ChildTranscriptLoadedEvent struct {
	SessionID string        `json:"session_id"`
	Messages  []ChatMessage `json:"messages"`
}

func (ChildTranscriptLoadedEvent) IsChatEvent() {}

// SessionDeletedEvent confirms that a session was deleted.
type SessionDeletedEvent struct {
	SessionID string `json:"session_id"`
}

func (SessionDeletedEvent) IsChatEvent() {}

// SessionExportedEvent signals that a session export completed.
type SessionExportedEvent struct {
	SessionID string `json:"session_id"`
	Format    string `json:"format"`
	Path      string `json:"path,omitempty"` // file path if written to file
}

func (SessionExportedEvent) IsChatEvent() {}

// ScheduleTickEvent is published on the event bus at a configurable
// interval (TAU_SCHEDULE_INTERVAL). Subscribers (e.g. plugin manager)
// use it to poll external services or trigger background work.
type ScheduleTickEvent struct {
	OccurredAt time.Time
}

// PluginLifecycleEvent is published on the event bus for fire-and-forget
// plugin lifecycle notifications. It is not a ChatEvent - it routes as a
// separate bus topic consumed by the plugin manager.
type PluginLifecycleEvent struct {
	Event     string
	SessionID string
	Payload   any // *api.EventPayload at rest; kept as any to avoid chat→api import
}

// ChatCompletionRequest is the wire format sent to the OpenAI-compatible endpoint.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	Tools       []ChatToolDef `json:"tools,omitempty"`
}

// ChatCompletionResponse is the non-streaming response shape.
type ChatCompletionResponse struct {
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *ChatUsage             `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// ChatCompletionChunk is one SSE data frame from the streaming endpoint.
type ChatCompletionChunk struct {
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *ChatUsage                  `json:"usage,omitempty"`
}

type ChatCompletionChunkChoice struct {
	Delta        ChatCompletionDelta `json:"delta"`
	FinishReason *string             `json:"finish_reason,omitempty"`
}

type ChatCompletionDelta struct {
	Content          string              `json:"content,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatToolCallDelta is an incremental tool call chunk from the streaming endpoint.
// The first chunk for a tool call carries ID, Type, and Function.Name;
// subsequent chunks append to Function.Arguments.
type ChatToolCallDelta struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function ChatFunctionCallDelta `json:"function"`
}

// ChatFunctionCallDelta carries incremental function name/arguments data.
type ChatFunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ChatSessionState is the runtime-owned mutable state for one conversation.
type ChatSessionState struct {
	SessionID string `json:"session_id"`
	// Provider holds the full provider configuration (including auth). It is
	// never written to the wire to avoid leaking credentials.
	Provider config.ProviderConfig `json:"-"`
	// ProviderName mirrors Provider.Name and is the field consumed by wire
	// consumers (web UI, bridge) to know which provider is active.
	ProviderName     string            `json:"provider,omitempty"`
	Model            ChatModelRef      `json:"model"`
	SystemPrompt     string            `json:"system_prompt"`
	Parameters       ChatParameters    `json:"parameters"`
	Status           ChatSessionStatus `json:"status"`
	Messages         []ChatMessage     `json:"messages"`
	Tools            []ChatToolDef     `json:"tools,omitempty"`
	PendingAssistant string            `json:"pending_assistant,omitempty"`
	ActiveRequestID  string            `json:"active_request_id,omitempty"`
	LastFinishReason string            `json:"last_finish_reason,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	LastUsage        ChatUsage         `json:"last_usage"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	// ParentSessionID links this session to its parent for conversation
	// branching / traceability. Empty means root session. Immutable after creation.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// AgentInstanceID links this session to the agent instance that ran it.
	// Empty for pre-migration sessions. Immutable after creation.
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
}

func NewChatSessionState(sessionID string, cfg ChatSessionConfig, now time.Time) (*ChatSessionState, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("chat session id is required")
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	now = normalizeChatTime(now)
	return &ChatSessionState{
		SessionID:       sessionID,
		Provider:        cfg.Provider,
		ProviderName:    cfg.Provider.Name,
		Model:           cfg.Model,
		SystemPrompt:    cfg.SystemPrompt,
		Parameters:      cfg.Parameters,
		ParentSessionID: cfg.ParentSessionID,
		AgentInstanceID: cfg.AgentInstanceID,
		Status:          ChatSessionIdle,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (s *ChatSessionState) ApplyPatch(patch ChatSessionPatch, at time.Time) error {
	if err := patch.Validate(); err != nil {
		return err
	}
	if s.Status == ChatSessionStreaming || s.Status == ChatSessionCancelling {
		return fmt.Errorf("cannot update session while %s", s.Status)
	}
	if s.Status == ChatSessionClosed {
		return errors.New("cannot update a closed session")
	}
	if patch.Model != nil {
		prevModelID := s.Model.ID
		s.Model = *patch.Model
		if s.Model.Config.MaxTokens > 0 {
			s.Parameters.MaxTokens = ClampMaxTokensForModel(s.Parameters.MaxTokens, s.Model)
		} else if s.Model.ID != prevModelID {
			// The new model's output-token ceiling is unknown (e.g. a
			// live-fetched provider like Ollama, which carries no per-model
			// metadata) - ClampMaxTokensForModel can't validate against it,
			// so a MaxTokens value tuned for the PREVIOUS model would
			// otherwise be forwarded as-is and get rejected once it exceeds
			// the new model's real limit. Reset to 0 (omitted from the wire
			// request via omitempty) so the provider applies its own
			// default instead of inheriting a number that was never meant
			// for this model.
			s.Parameters.MaxTokens = 0
		}
	}
	if patch.Provider != nil {
		s.Provider.Name = *patch.Provider
		s.ProviderName = *patch.Provider
	}
	if patch.SystemPrompt != nil {
		s.SystemPrompt = *patch.SystemPrompt
	}
	if patch.MaxTokens != nil {
		s.Parameters.MaxTokens = ClampMaxTokensForModel(*patch.MaxTokens, s.Model)
	}
	if patch.Temperature != nil {
		s.Parameters.Temperature = *patch.Temperature
	}
	if patch.ReasoningEffort != nil {
		s.Parameters.ReasoningEffort = *patch.ReasoningEffort
	}
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s ChatSessionState) HasActiveRequest() bool {
	return s.ActiveRequestID != ""
}

func (s *ChatSessionState) BeginTurn(requestID, prompt string, at time.Time) error {
	if s.Status == ChatSessionClosed {
		return errors.New("cannot start a turn on a closed session")
	}
	if s.HasActiveRequest() {
		return fmt.Errorf("request %q is already in flight", s.ActiveRequestID)
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return errors.New("request id is required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return errors.New("prompt is required")
	}
	s.Messages = append(s.Messages, ChatMessage{ID: NewMessageID(), Role: ChatRoleUser, Content: prompt, CreatedAt: normalizeChatTime(at)})
	s.Status = ChatSessionStreaming
	s.ActiveRequestID = requestID
	s.PendingAssistant = ""
	s.LastFinishReason = ""
	s.LastError = ""
	s.LastUsage = ChatUsage{}
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) AppendAssistantDelta(delta string, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if s.Status != ChatSessionStreaming && s.Status != ChatSessionCancelling {
		return fmt.Errorf("cannot append assistant delta while %s", s.Status)
	}
	if delta == "" {
		return nil
	}
	s.PendingAssistant += delta
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

// AppendAssistantToolCallMessage commits the current assistant turn with tool calls.
// This is called when the LLM produces a response containing tool_calls.
func (s *ChatSessionState) AppendAssistantToolCallMessage(content string, calls []ChatToolCall, at time.Time) error {
	return s.AppendAssistantToolCallMessageWithReasoning(content, "", calls, at)
}

// AppendAssistantToolCallMessageWithReasoning commits the current assistant turn
// with tool calls and provider-supplied hidden reasoning content.
func (s *ChatSessionState) AppendAssistantToolCallMessageWithReasoning(content, reasoningContent string, calls []ChatToolCall, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	s.Messages = append(s.Messages, ChatMessage{
		ID:               NewMessageID(),
		Role:             ChatRoleAssistant,
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        calls,
		CreatedAt:        normalizeChatTime(at),
	})
	s.PendingAssistant = ""
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

// AppendToolResultMessage appends a tool result to the conversation.
func (s *ChatSessionState) AppendToolResultMessage(callID, content string, at time.Time) error {
	return s.AppendToolResultMessageWithMetadata(callID, content, nil, at)
}

// AppendToolResultMessageWithMetadata appends a tool result and its
// authoritative execution metadata to the conversation.
func (s *ChatSessionState) AppendToolResultMessageWithMetadata(callID, content string, metadata *ToolResultMetadata, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if strings.TrimSpace(callID) == "" {
		return errors.New("tool_call_id is required")
	}
	s.Messages = append(s.Messages, ChatMessage{
		ID:         NewMessageID(),
		Role:       ChatRoleTool,
		Content:    content,
		ToolCallID: callID,
		ToolResult: metadata,
		CreatedAt:  normalizeChatTime(at),
	})
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

// AppendStandaloneMessage appends a message outside the normal turn
// lifecycle - unlike AppendAssistantToolCallMessage/AppendToolResultMessage,
// it does not require an active request. Used for bash-mode ("!") results,
// which run independently of the LLM turn loop but still need to land in
// history for a later turn to see.
func (s *ChatSessionState) AppendStandaloneMessage(role ChatRole, content string, at time.Time) error {
	s.Messages = append(s.Messages, ChatMessage{
		ID:        NewMessageID(),
		Role:      role,
		Content:   content,
		CreatedAt: normalizeChatTime(at),
	})
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

// ClearPendingAssistant resets the in-flight assistant buffer between tool-loop iterations.
func (s *ChatSessionState) ClearPendingAssistant(at time.Time) {
	s.PendingAssistant = ""
	s.UpdatedAt = normalizeChatTime(at)
}

func (s *ChatSessionState) MarkCancelling(at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if s.Status != ChatSessionStreaming {
		return fmt.Errorf("cannot cancel while %s", s.Status)
	}
	s.Status = ChatSessionCancelling
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) CompleteTurn(finishReason string, usage ChatUsage, at time.Time) error {
	return s.CompleteTurnWithReasoning(finishReason, usage, "", at)
}

// CompleteTurnWithReasoning completes an assistant turn and persists
// provider-supplied hidden reasoning content with the final assistant message.
func (s *ChatSessionState) CompleteTurnWithReasoning(finishReason string, usage ChatUsage, reasoningContent string, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if s.Status != ChatSessionStreaming && s.Status != ChatSessionCancelling {
		return fmt.Errorf("cannot complete turn while %s", s.Status)
	}
	if strings.TrimSpace(s.PendingAssistant) != "" || reasoningContent != "" {
		s.Messages = append(s.Messages, ChatMessage{
			ID:               NewMessageID(),
			Role:             ChatRoleAssistant,
			Content:          s.PendingAssistant,
			ReasoningContent: reasoningContent,
			CreatedAt:        normalizeChatTime(at),
		})
	}
	s.Status = ChatSessionIdle
	s.PendingAssistant = ""
	s.ActiveRequestID = ""
	s.LastFinishReason = finishReason
	s.LastUsage = usage
	s.LastError = ""
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) CancelTurn(at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if s.Status != ChatSessionStreaming && s.Status != ChatSessionCancelling {
		return fmt.Errorf("cannot cancel turn while %s", s.Status)
	}
	s.Status = ChatSessionIdle
	s.PendingAssistant = ""
	s.ActiveRequestID = ""
	s.LastFinishReason = "cancelled"
	s.LastError = ""
	s.LastUsage = ChatUsage{}
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) FailTurn(err error, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if s.Status != ChatSessionStreaming && s.Status != ChatSessionCancelling {
		return fmt.Errorf("cannot fail turn while %s", s.Status)
	}
	if err == nil {
		return errors.New("turn error is required")
	}
	s.Status = ChatSessionIdle
	s.PendingAssistant = ""
	s.ActiveRequestID = ""
	s.LastFinishReason = ""
	s.LastError = err.Error()
	s.LastUsage = ChatUsage{}
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) ResetConversation(at time.Time) error {
	if s.HasActiveRequest() {
		return errors.New("cannot reset while a request is active")
	}
	if s.Status == ChatSessionClosed {
		return errors.New("cannot reset a closed session")
	}
	s.Messages = nil
	s.PendingAssistant = ""
	s.ActiveRequestID = ""
	s.LastFinishReason = ""
	s.LastError = ""
	s.LastUsage = ChatUsage{}
	s.Status = ChatSessionIdle
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s *ChatSessionState) Close(at time.Time) error {
	if s.HasActiveRequest() {
		return errors.New("cannot close while a request is active")
	}
	s.Status = ChatSessionClosed
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

func (s ChatSessionState) RequestMessages() []ChatMessage {
	messages := make([]ChatMessage, 0, len(s.Messages)+1)
	if strings.TrimSpace(s.SystemPrompt) != "" {
		messages = append(messages, ChatMessage{
			Role:    ChatRoleSystem,
			Content: s.SystemPrompt,
		})
	}

	for _, m := range s.Messages {
		if len(messages) > 0 && m.Role == ChatRoleUser && messages[len(messages)-1].Role == ChatRoleUser {
			// Merge consecutive user messages to guarantee alternating roles
			// (system, user, assistant, user, assistant...) for providers that
			// strictly enforce it (like OpenAI/Anthropic/DeepSeek).
			messages[len(messages)-1].Content += "\n\n" + m.Content
		} else {
			messages = append(messages, m)
		}
	}
	return messages
}

func (s ChatSessionState) CompletionRequest() ChatCompletionRequest {
	return ChatCompletionRequest{
		Model:       s.Model.ID,
		Messages:    s.RequestMessages(),
		MaxTokens:   s.Parameters.MaxTokens,
		Temperature: s.Parameters.Temperature,
		Stream:      true,
		Tools:       s.Tools,
	}
}

func normalizeChatTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
