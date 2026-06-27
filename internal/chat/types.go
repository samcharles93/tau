package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	Role             ChatRole       `json:"role"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
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

// MarshalJSON surfaces the resolved model's context-window size and pricing to
// wire consumers (e.g. the web UI) so they can render context usage and cost.
// The Config field itself stays off the wire; only these derived fields are
// exposed alongside the existing id/url.
func (m ChatModelRef) MarshalJSON() ([]byte, error) {
	type alias ChatModelRef
	return json.Marshal(struct {
		alias
		ContextWindow int               `json:"context_window,omitempty"`
		Cost          config.CostConfig `json:"cost,omitzero"`
	}{
		alias:         alias(m),
		ContextWindow: m.Config.ContextWindow,
		Cost:          m.Config.Cost,
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
}

func (c ChatSessionConfig) withDefaults() ChatSessionConfig {
	c.Parameters = c.Parameters.withDefaults()
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = defaultChatSystemPrompt
	}
	return c
}

func (c ChatSessionConfig) Validate() error {
	if strings.TrimSpace(c.Provider.Name) == "" || strings.TrimSpace(c.Provider.BaseURL) == "" {
		return errors.New("chat provider is required")
	}
	// An empty model is permitted: the session launches unselected and the user
	// picks a model with /model. A model that is set must still be valid.
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

// ListSessionsCommand requests a paginated list of saved sessions.
type ListSessionsCommand struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
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
	RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error)
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
	SessionID        string    `json:"session_id"`
	RequestID        string    `json:"request_id"`
	CallID           string    `json:"call_id"`
	ToolName         string    `json:"tool_name"`
	ArgumentsSummary string    `json:"arguments_summary"`
	StartedAt        time.Time `json:"started_at"`
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

type ExtensionCommandResultEvent struct {
	Name       string    `json:"name"`
	Output     string    `json:"output"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ExtensionCommandResultEvent) IsChatEvent() {}

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
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
}

// SessionsListedEvent carries paginated session summaries back to the TUI.
type SessionsListedEvent struct {
	Sessions   []SessionSummary `json:"sessions"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (SessionsListedEvent) IsChatEvent() {}

// SessionLoadedEvent carries a fully reconstructed session state.
type SessionLoadedEvent struct {
	State ChatSessionState `json:"state"`
}

func (SessionLoadedEvent) IsChatEvent() {}

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
// plugin lifecycle notifications. It is not a ChatEvent — it routes as a
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
		s.Model = *patch.Model
	}
	if patch.Provider != nil {
		s.Provider.Name = *patch.Provider
		s.ProviderName = *patch.Provider
	}
	if patch.SystemPrompt != nil {
		s.SystemPrompt = *patch.SystemPrompt
	}
	if patch.MaxTokens != nil {
		s.Parameters.MaxTokens = *patch.MaxTokens
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
	s.Messages = append(s.Messages, ChatMessage{Role: ChatRoleUser, Content: prompt})
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
		Role:             ChatRoleAssistant,
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        calls,
	})
	s.PendingAssistant = ""
	s.UpdatedAt = normalizeChatTime(at)
	return nil
}

// AppendToolResultMessage appends a tool result to the conversation.
func (s *ChatSessionState) AppendToolResultMessage(callID, content string, at time.Time) error {
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if strings.TrimSpace(callID) == "" {
		return errors.New("tool_call_id is required")
	}
	s.Messages = append(s.Messages, ChatMessage{
		Role:       ChatRoleTool,
		Content:    content,
		ToolCallID: callID,
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
			Role:             ChatRoleAssistant,
			Content:          s.PendingAssistant,
			ReasoningContent: reasoningContent,
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

	messages = append(messages, s.Messages...)
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
