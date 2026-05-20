package chat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

const (
	defaultChatSystemPrompt = "You are a helpful assistant."
	defaultChatMaxTokens    = 1024
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
)

func (r ChatRole) Valid() bool {
	switch r {
	case ChatRoleSystem, ChatRoleUser, ChatRoleAssistant:
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
	Role    ChatRole `json:"role"`
	Content string   `json:"content"`
}

func (m ChatMessage) Validate() error {
	if !m.Role.Valid() {
		return fmt.Errorf("invalid chat role %q", m.Role)
	}
	if strings.TrimSpace(m.Content) == "" {
		return errors.New("chat message content is required")
	}
	return nil
}

// ChatModelRef identifies the selected model route for a session.
type ChatModelRef struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func (m ChatModelRef) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("chat model id is required")
	}
	if strings.TrimSpace(m.URL) == "" {
		return errors.New("chat model url is required")
	}
	return nil
}

// ChatParameters are the tunable per-request controls for a session.
type ChatParameters struct {
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

func defaultChatParameters() ChatParameters {
	return ChatParameters{
		MaxTokens:   defaultChatMaxTokens,
		Temperature: defaultChatTemperature,
	}
}

func (p ChatParameters) withDefaults() ChatParameters {
	defaults := defaultChatParameters()
	if p.MaxTokens == 0 {
		p.MaxTokens = defaults.MaxTokens
	}
	if p.Temperature == 0 {
		p.Temperature = defaults.Temperature
	}
	return p
}

func (p ChatParameters) Validate() error {
	if p.MaxTokens <= 0 {
		return errors.New("max_tokens must be greater than 0")
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
	Endpoint     platform.Endpoint `json:"-"`
	Model        ChatModelRef      `json:"model"`
	SystemPrompt string            `json:"system_prompt"`
	Parameters   ChatParameters    `json:"parameters"`
}

func (c ChatSessionConfig) withDefaults() ChatSessionConfig {
	c.Parameters = c.Parameters.withDefaults()
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = defaultChatSystemPrompt
	}
	return c
}

func (c ChatSessionConfig) Validate() error {
	if strings.TrimSpace(c.Endpoint.Key()) == "/" || c.Endpoint.MaaSGateway == "" {
		return errors.New("chat endpoint is required")
	}
	if err := c.Model.Validate(); err != nil {
		return err
	}
	return c.Parameters.Validate()
}

// ChatSessionPatch holds optional config changes for an existing session.
type ChatSessionPatch struct {
	Model        *ChatModelRef `json:"model,omitempty"`
	SystemPrompt *string       `json:"system_prompt,omitempty"`
	MaxTokens    *int          `json:"max_tokens,omitempty"`
	Temperature  *float64      `json:"temperature,omitempty"`
}

func (p ChatSessionPatch) Validate() error {
	if p.Model != nil {
		if err := p.Model.Validate(); err != nil {
			return err
		}
	}
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return errors.New("max_tokens must be greater than 0")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

// ChatCommand is the input contract from the UI to the runtime.
type ChatCommand interface{ isChatCommand() }

type StartChatSessionCommand struct {
	SessionID string            `json:"session_id"`
	Config    ChatSessionConfig `json:"config"`
}

func (StartChatSessionCommand) isChatCommand() {}

type SubmitChatPromptCommand struct {
	SessionID   string    `json:"session_id"`
	RequestID   string    `json:"request_id"`
	Prompt      string    `json:"prompt"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (SubmitChatPromptCommand) isChatCommand() {}

type UpdateChatSessionCommand struct {
	SessionID string           `json:"session_id"`
	Patch     ChatSessionPatch `json:"patch"`
}

func (UpdateChatSessionCommand) isChatCommand() {}

type CancelChatRequestCommand struct {
	SessionID   string    `json:"session_id"`
	RequestID   string    `json:"request_id,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

func (CancelChatRequestCommand) isChatCommand() {}

type ResetChatSessionCommand struct {
	SessionID   string    `json:"session_id"`
	RequestedAt time.Time `json:"requested_at"`
}

func (ResetChatSessionCommand) isChatCommand() {}

type CloseChatSessionCommand struct {
	SessionID   string    `json:"session_id"`
	RequestedAt time.Time `json:"requested_at"`
}

func (CloseChatSessionCommand) isChatCommand() {}

// ChatEvent is the output contract from the runtime back to the UI.
type ChatEvent interface{ isChatEvent() }

type ChatSessionSnapshotEvent struct {
	State ChatSessionState `json:"state"`
}

func (ChatSessionSnapshotEvent) isChatEvent() {}

type ChatResponseStartedEvent struct {
	SessionID string    `json:"session_id"`
	RequestID string    `json:"request_id"`
	StartedAt time.Time `json:"started_at"`
}

func (ChatResponseStartedEvent) isChatEvent() {}

type ChatResponseDeltaEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id"`
	Delta      string    `json:"delta"`
	Snapshot   string    `json:"snapshot"`
	ReceivedAt time.Time `json:"received_at"`
}

func (ChatResponseDeltaEvent) isChatEvent() {}

type ChatResponseCompletedEvent struct {
	State        ChatSessionState `json:"state"`
	RequestID    string           `json:"request_id"`
	FinishReason string           `json:"finish_reason"`
	Usage        ChatUsage        `json:"usage"`
	CompletedAt  time.Time        `json:"completed_at"`
}

func (ChatResponseCompletedEvent) isChatEvent() {}

type ChatResponseCancelledEvent struct {
	State       ChatSessionState `json:"state"`
	RequestID   string           `json:"request_id"`
	CancelledAt time.Time        `json:"cancelled_at"`
}

func (ChatResponseCancelledEvent) isChatEvent() {}

type ChatRuntimeErrorEvent struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id,omitempty"`
	Message    string    `json:"message"`
	Fatal      bool      `json:"fatal"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ChatRuntimeErrorEvent) isChatEvent() {}

// ChatCompletionRequest is the wire format sent to the OpenAI-compatible endpoint.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
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
	Content string `json:"content,omitempty"`
}

// ChatSessionState is the runtime-owned mutable state for one conversation.
type ChatSessionState struct {
	SessionID        string            `json:"session_id"`
	Endpoint         platform.Endpoint `json:"-"`
	Model            ChatModelRef      `json:"model"`
	SystemPrompt     string            `json:"system_prompt"`
	Parameters       ChatParameters    `json:"parameters"`
	Status           ChatSessionStatus `json:"status"`
	Messages         []ChatMessage     `json:"messages"`
	PendingAssistant string            `json:"pending_assistant,omitempty"`
	ActiveRequestID  string            `json:"active_request_id,omitempty"`
	LastFinishReason string            `json:"last_finish_reason,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	LastUsage        ChatUsage         `json:"last_usage"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
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
		SessionID:    sessionID,
		Endpoint:     cfg.Endpoint,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
		Parameters:   cfg.Parameters,
		Status:       ChatSessionIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
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
	if patch.SystemPrompt != nil {
		s.SystemPrompt = *patch.SystemPrompt
	}
	if patch.MaxTokens != nil {
		s.Parameters.MaxTokens = *patch.MaxTokens
	}
	if patch.Temperature != nil {
		s.Parameters.Temperature = *patch.Temperature
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
	if !s.HasActiveRequest() {
		return errors.New("no active request")
	}
	if strings.TrimSpace(s.PendingAssistant) != "" {
		s.Messages = append(s.Messages, ChatMessage{Role: ChatRoleAssistant, Content: s.PendingAssistant})
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
	}
}

func normalizeChatTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
