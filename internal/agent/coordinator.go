// Package agent implements the agent coordinator — the single runtime that
// mediates between the TUI and the LLM. It owns the agentic turn loop:
// stream a completion, detect tool_calls, execute tools in parallel,
// feed results back, and loop until the model produces a final text response.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/platform"
	"github.com/samcharles93/tau/internal/pubsub"
)

const (
	coordinatorEventTopic        = "agent.coordinator.events"
	commandBufferSize            = 16
	defaultMaxToolLoopIterations = 50
)

// TokenSource resolves a bearer token for the configured provider.
type TokenSource = platform.TokenSource

// Streamer is the interface for making streaming LLM calls.
// The coordinator calls StreamChatCompletionFull once per turn-loop iteration.
type Streamer interface {
	StreamChatCompletionFull(
		ctx context.Context,
		session chat.ChatSessionState,
		bearerToken string,
		cb chat.StreamCallbacks,
	) (chat.CompletionResult, error)
}

// Coordinator is the agent runtime that replaces chat.Runtime.
// It receives commands from the TUI, runs the agentic turn loop,
// and publishes events via pubsub.
type Coordinator struct {
	ctx               context.Context
	cancel            context.CancelFunc
	tokenSource       TokenSource
	streamer          Streamer
	registry          *tools.Registry
	maxToolIterations int
	parallelToolCalls bool
	onSessionStart    func(map[string]any)
	onSessionShutdown func(map[string]any)
	onClose           func()
	commands          chan chat.ChatCommand
	eventBus          *pubsub.Bus[chat.ChatEvent]

	mu       sync.Mutex
	sessions map[string]*coordinatorSession
	shutdown map[string]struct{}

	closeOnce sync.Once
	done      chan struct{}
	loopDone  chan struct{}
	turnWG    sync.WaitGroup
}

type coordinatorSession struct {
	state  *chat.ChatSessionState
	cancel context.CancelFunc
}

// CoordinatorConfig holds the dependencies for creating a Coordinator.
type CoordinatorConfig struct {
	TokenSource       TokenSource
	Streamer          Streamer
	Registry          *tools.Registry
	MaxToolIterations int   // 0 → default (50)
	ParallelToolCalls *bool // nil → default (true)
	OnSessionStart    func(map[string]any)
	OnSessionShutdown func(map[string]any)
	OnClose           func()
}

// NewCoordinator creates and starts the agent coordinator.
func NewCoordinator(ctx context.Context, cfg CoordinatorConfig) (*Coordinator, error) {
	if cfg.TokenSource == nil {
		return nil, errors.New("agent token source is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("agent tool registry is required")
	}
	if cfg.Streamer == nil {
		return nil, errors.New("agent streamer is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	maxIter := cfg.MaxToolIterations
	if maxIter <= 0 {
		maxIter = defaultMaxToolLoopIterations
	}
	parallel := true
	if cfg.ParallelToolCalls != nil {
		parallel = *cfg.ParallelToolCalls
	}

	c := &Coordinator{
		ctx:               ctx,
		cancel:            cancel,
		tokenSource:       cfg.TokenSource,
		streamer:          cfg.Streamer,
		registry:          cfg.Registry,
		maxToolIterations: maxIter,
		parallelToolCalls: parallel,
		onSessionStart:    cfg.OnSessionStart,
		onSessionShutdown: cfg.OnSessionShutdown,
		onClose:           cfg.OnClose,
		commands:          make(chan chat.ChatCommand, commandBufferSize),
		eventBus:          pubsub.New[chat.ChatEvent](),
		sessions:          make(map[string]*coordinatorSession),
		shutdown:          make(map[string]struct{}),
		done:              make(chan struct{}),
		loopDone:          make(chan struct{}),
	}

	go func() {
		defer close(c.loopDone)
		c.loop()
	}()

	return c, nil
}

// SubscribeEvents returns a subscription for coordinator events.
func (c *Coordinator) SubscribeEvents(buffer int) (*pubsub.Subscription[chat.ChatEvent], error) {
	return c.eventBus.Subscribe(coordinatorEventTopic, buffer)
}

// Send submits a command to the coordinator.
func (c *Coordinator) Send(cmd chat.ChatCommand) error {
	if cmd == nil {
		return errors.New("command is required")
	}
	select {
	case <-c.done:
		return errors.New("coordinator is closed")
	case <-c.ctx.Done():
		return errors.New("coordinator is closed")
	case c.commands <- cmd:
		return nil
	}
}

// Close shuts down the coordinator gracefully.
func (c *Coordinator) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		<-c.loopDone
		if c.onClose != nil {
			c.onClose()
		}
		c.eventBus.Close()
		close(c.done)
	})
}

func (c *Coordinator) loop() {
	for {
		select {
		case <-c.ctx.Done():
			c.cancelAllSessions()
			c.turnWG.Wait()
			return
		case cmd := <-c.commands:
			c.handleCommand(cmd)
		}
	}
}

func (c *Coordinator) handleCommand(cmd chat.ChatCommand) {
	switch command := cmd.(type) {
	case chat.StartChatSessionCommand:
		c.handleStart(command)
	case chat.SubmitChatPromptCommand:
		c.handleSubmit(command)
	case chat.UpdateChatSessionCommand:
		c.handleUpdate(command)
	case chat.CancelChatRequestCommand:
		c.handleCancel(command)
	case chat.ResetChatSessionCommand:
		c.handleReset(command)
	case chat.CloseChatSessionCommand:
		c.handleClose(command)
	default:
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("unsupported command %T", cmd),
			Fatal:      true,
			OccurredAt: time.Now().UTC(),
		})
	}
}

func (c *Coordinator) handleStart(cmd chat.StartChatSessionCommand) {
	now := time.Now().UTC()
	state, err := chat.NewChatSessionState(cmd.SessionID, cmd.Config, now)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}

	c.mu.Lock()
	if _, exists := c.sessions[state.SessionID]; exists {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  state.SessionID,
			Message:    "session already exists",
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}
	c.sessions[state.SessionID] = &coordinatorSession{state: state}
	snapshot := chat.CloneChatSessionState(state)
	c.mu.Unlock()

	c.dispatchSessionStart(snapshot.SessionID)
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleSubmit(cmd chat.SubmitChatPromptCommand) {
	now := normalizedTime(cmd.SubmittedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.BeginTurn(cmd.RequestID, cmd.Prompt, now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	turnState := chat.CloneChatSessionState(session.state)
	turnCtx, cancel := context.WithCancel(c.ctx)
	session.cancel = cancel
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emit(chat.ChatResponseStartedEvent{
		SessionID: cmd.SessionID,
		RequestID: cmd.RequestID,
		StartedAt: now,
	})

	c.turnWG.Add(1)
	go func() {
		defer c.turnWG.Done()
		c.runTurn(turnCtx, turnState)
	}()
}

func (c *Coordinator) handleUpdate(cmd chat.UpdateChatSessionCommand) {
	now := time.Now().UTC()

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.ApplyPatch(cmd.Patch, now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.dispatchSessionShutdown(snapshot.SessionID)
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleCancel(cmd chat.CancelChatRequestCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if cmd.RequestID != "" && session.state.ActiveRequestID != cmd.RequestID {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "requested turn is not active",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.MarkCancelling(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	cancel := session.cancel
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	if cancel != nil {
		cancel()
	}
}

func (c *Coordinator) handleReset(cmd chat.ResetChatSessionCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.ResetConversation(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleClose(cmd chat.CloseChatSessionCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.Close(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

// runTurn is the agentic turn loop. It streams a completion, and if the
// model returns tool_calls, executes them in parallel, appends results
// to the conversation, and loops. Stops when the model produces a final
// text response or an error occurs.
func (c *Coordinator) runTurn(ctx context.Context, state chat.ChatSessionState) {
	sessionID := state.SessionID
	requestID := state.ActiveRequestID
	now := time.Now().UTC()

	bearerToken, err := c.tokenSource(ctx, state.Provider)
	if err != nil {
		c.failTurn(sessionID, requestID, err, now)
		return
	}

	// Build tool definitions from registry.
	toolDefs := c.buildToolDefs()

	// The turn loop: call LLM, if tool_calls → execute → append → repeat.
	for iteration := 0; iteration < c.maxToolIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			c.cancelTurn(sessionID, requestID, time.Now().UTC())
			return
		}

		// Inject tools into the session state for this call.
		state.Tools = toolDefs

		result, err := c.streamer.StreamChatCompletionFull(ctx, state, bearerToken, chat.StreamCallbacks{
			OnDelta: func(delta string) error {
				return c.appendDelta(sessionID, requestID, delta, time.Now().UTC())
			},
			OnToolCallDelta: func(tcd chat.ChatToolCallDelta) error {
				// Emit tool-call progress for TUI observability.
				c.emit(AgentToolCallDeltaEvent{
					SessionID: sessionID,
					RequestID: requestID,
					Delta:     tcd,
				})
				return nil
			},
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
				c.cancelTurn(sessionID, requestID, time.Now().UTC())
				return
			}
			c.failTurn(sessionID, requestID, err, time.Now().UTC())
			return
		}

		// No tool calls → final response. Complete the turn.
		if len(result.ToolCalls) == 0 {
			c.completeTurn(sessionID, requestID, result, time.Now().UTC())
			return
		}

		// Tool calls detected. Commit the assistant's response (with tool_calls)
		// and execute them in parallel.
		c.commitAssistantMessage(sessionID, c.getPendingContent(sessionID), result.ToolCalls)

		// Execute tool calls in parallel.
		toolResults := c.executeToolsParallel(ctx, result.ToolCalls)

		// Append tool result messages to the session.
		for i, tc := range result.ToolCalls {
			c.appendToolResult(sessionID, tc.ID, toolResults[i].Content)
		}

		// Refresh state for the next iteration.
		state = c.getSessionState(sessionID)
		if state.SessionID == "" {
			return // session gone
		}

		// Clear pending assistant for the next LLM call.
		c.clearPending(sessionID)
	}

	// Safety: hit max iterations.
	c.failTurn(sessionID, requestID,
		fmt.Errorf("agent exceeded maximum tool loop iterations (%d)", c.maxToolIterations),
		time.Now().UTC())
}

// executeToolsParallel runs tool calls and returns results in input order.
// When parallelToolCalls is true, calls run concurrently; otherwise sequentially.
func (c *Coordinator) executeToolsParallel(ctx context.Context, calls []chat.ChatToolCall) []tools.Result {
	results := make([]tools.Result, len(calls))

	executeTool := func(i int, tc chat.ChatToolCall) {
		c.emit(AgentToolExecutionStartedEvent{
			SessionID: tc.ID,
			ToolName:  tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})

		tool, ok := c.registry.Get(tc.Function.Name)
		if !ok {
			results[i] = tools.Result{
				Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
				IsError: true,
			}
			return
		}

		result, err := tool.Execute(ctx, json.RawMessage(tc.Function.Arguments), nil)
		if err != nil {
			results[i] = tools.Result{
				Content: fmt.Sprintf("tool execution error: %v", err),
				IsError: true,
			}
			return
		}

		// Truncate tool output.
		tr := tools.TruncateHead(result.Content, tools.DefaultMaxLines, tools.DefaultMaxBytes)
		result.Content = tr.Content

		results[i] = result
	}

	if c.parallelToolCalls {
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc chat.ChatToolCall) {
				defer wg.Done()
				executeTool(i, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range calls {
			executeTool(i, tc)
		}
	}

	return results
}

func (c *Coordinator) buildToolDefs() []chat.ChatToolDef {
	schemas := c.registry.Schemas()
	defs := make([]chat.ChatToolDef, len(schemas))
	for i, s := range schemas {
		defs[i] = chat.ChatToolDef{
			Type: "function",
			Function: chat.ChatToolDefFunction{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  s.Parameters,
			},
		}
	}
	return defs
}

// Session state helpers — these operate under the coordinator mutex.

func (c *Coordinator) appendDelta(sessionID, requestID, delta string, at time.Time) error {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return errors.New("session not found")
	}
	if session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return context.Canceled
	}
	if err := session.state.AppendAssistantDelta(delta, at); err != nil {
		c.mu.Unlock()
		return err
	}
	snapshot := session.state.PendingAssistant
	c.mu.Unlock()

	c.emit(chat.ChatResponseDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
	return nil
}

func (c *Coordinator) getPendingContent(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return ""
	}
	return session.state.PendingAssistant
}

func (c *Coordinator) commitAssistantMessage(sessionID string, content string, calls []chat.ChatToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	_ = session.state.AppendAssistantToolCallMessage(content, calls, time.Now().UTC())
}

func (c *Coordinator) appendToolResult(sessionID string, callID, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	_ = session.state.AppendToolResultMessage(callID, content, time.Now().UTC())
}

func (c *Coordinator) clearPending(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	session.state.ClearPendingAssistant(time.Now().UTC())
}

func (c *Coordinator) getSessionState(sessionID string) chat.ChatSessionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return chat.ChatSessionState{}
	}
	return chat.CloneChatSessionState(session.state)
}

func (c *Coordinator) completeTurn(sessionID, requestID string, result chat.CompletionResult, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.CompleteTurn(result.FinishReason, result.Usage, at)
	session.cancel = nil
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emitMustDeliver(chat.ChatResponseCompletedEvent{
		State:        snapshot,
		RequestID:    requestID,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		CompletedAt:  at,
	})
}

func (c *Coordinator) cancelTurn(sessionID, requestID string, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.CancelTurn(at)
	session.cancel = nil
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emitMustDeliver(chat.ChatResponseCancelledEvent{
		State:       snapshot,
		RequestID:   requestID,
		CancelledAt: at,
	})
}

func (c *Coordinator) failTurn(sessionID, requestID string, err error, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.FailTurn(err, at)
	session.cancel = nil
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emitMustDeliver(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emitMustDeliver(chat.ChatRuntimeErrorEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Message:    err.Error(),
		Fatal:      false,
		OccurredAt: at,
	})
}

func (c *Coordinator) cancelAllSessions() {
	c.mu.Lock()
	sessionIDs := make([]string, 0, len(c.sessions))
	for _, session := range c.sessions {
		if session.cancel != nil {
			session.cancel()
		}
		sessionIDs = append(sessionIDs, session.state.SessionID)
	}
	c.mu.Unlock()

	for _, sessionID := range sessionIDs {
		c.dispatchSessionShutdown(sessionID)
	}
}

func (c *Coordinator) dispatchSessionStart(sessionID string) {
	if c.onSessionStart == nil {
		return
	}
	c.onSessionStart(map[string]any{
		"event":      "session_start",
		"session_id": sessionID,
	})
}

func (c *Coordinator) dispatchSessionShutdown(sessionID string) {
	if c.onSessionShutdown == nil {
		return
	}
	c.mu.Lock()
	if _, exists := c.shutdown[sessionID]; exists {
		c.mu.Unlock()
		return
	}
	c.shutdown[sessionID] = struct{}{}
	c.mu.Unlock()
	c.onSessionShutdown(map[string]any{
		"event":      "session_shutdown",
		"session_id": sessionID,
	})
}

func (c *Coordinator) emit(event chat.ChatEvent) {
	if err := c.eventBus.Publish(c.ctx, coordinatorEventTopic, event); err != nil {
		slog.Debug("coordinator: failed to publish event", "error", err)
	}
}

func (c *Coordinator) emitMustDeliver(event chat.ChatEvent) {
	if err := c.eventBus.PublishMustDeliver(coordinatorEventTopic, event); err != nil {
		slog.Error("coordinator: failed to deliver event", "error", err)
	}
}

func normalizedTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
