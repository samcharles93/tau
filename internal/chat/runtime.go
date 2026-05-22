package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/pubsub"
)

const runtimeEventTopic = "chat.runtime.events"

// commandBufferSize is the capacity of the commands channel. This must be
// large enough to avoid blocking callers during normal usage but small
// enough that back-pressure surfaces promptly if the loop stalls.
const commandBufferSize = 16

type TokenSource func(ctx context.Context, endpoint platform.Endpoint) (string, error)

type CompletionResult struct {
	FinishReason string
	Usage        ChatUsage
}

type CompletionStreamer interface {
	StreamChatCompletion(
		ctx context.Context,
		session ChatSessionState,
		maasToken string,
		onDelta func(string) error,
	) (CompletionResult, error)
}

type Runtime struct {
	ctx         context.Context
	cancel      context.CancelFunc
	tokenSource TokenSource
	streamer    CompletionStreamer
	commands    chan ChatCommand
	eventBus    *pubsub.Bus[ChatEvent]

	mu       sync.Mutex
	sessions map[string]*runtimeSession

	closeOnce sync.Once
	done      chan struct{}
	loopDone  chan struct{}
	streamWG  sync.WaitGroup
}

type runtimeSession struct {
	state  *ChatSessionState
	cancel context.CancelFunc
}

func NewRuntime(ctx context.Context, tokenSource TokenSource, streamer CompletionStreamer) (*Runtime, error) {
	if tokenSource == nil {
		return nil, errors.New("chat token source is required")
	}
	if streamer == nil {
		return nil, errors.New("chat completion streamer is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	eventBus := pubsub.New[ChatEvent]()

	runtime := &Runtime{
		ctx:         ctx,
		cancel:      cancel,
		tokenSource: tokenSource,
		streamer:    streamer,
		commands:    make(chan ChatCommand, commandBufferSize),
		eventBus:    eventBus,
		sessions:    make(map[string]*runtimeSession),
		done:        make(chan struct{}),
		loopDone:    make(chan struct{}),
	}

	go func() {
		defer close(runtime.loopDone)
		runtime.loop()
	}()

	return runtime, nil
}

func (r *Runtime) SubscribeEvents(buffer int) (*pubsub.Subscription[ChatEvent], error) {
	if r.eventBus == nil {
		return nil, errors.New("chat event bus is not available")
	}
	return r.eventBus.Subscribe(runtimeEventTopic, buffer)
}

func (r *Runtime) Send(cmd ChatCommand) error {
	if cmd == nil {
		return errors.New("chat command is required")
	}

	select {
	case <-r.done:
		return errors.New("chat runtime is closed")
	case <-r.ctx.Done():
		return errors.New("chat runtime is closed")
	case r.commands <- cmd:
		return nil
	}
}

func (r *Runtime) Close() {
	r.closeOnce.Do(func() {
		r.cancel()
		<-r.loopDone
		if r.eventBus != nil {
			r.eventBus.Close()
		}
		close(r.done)
	})
}

func (r *Runtime) loop() {
	for {
		select {
		case <-r.ctx.Done():
			r.cancelAllSessions()
			r.streamWG.Wait()
			return
		case cmd := <-r.commands:
			r.handleCommand(cmd)
		}
	}
}

func (r *Runtime) handleCommand(cmd ChatCommand) {
	switch command := cmd.(type) {
	case StartChatSessionCommand:
		r.handleStart(command)
	case SubmitChatPromptCommand:
		r.handleSubmit(command)
	case UpdateChatSessionCommand:
		r.handleUpdate(command)
	case CancelChatRequestCommand:
		r.handleCancel(command)
	case ResetChatSessionCommand:
		r.handleReset(command)
	case CloseChatSessionCommand:
		r.handleClose(command)
	default:
		r.emit(ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("unsupported chat command %T", cmd),
			Fatal:      true,
			OccurredAt: time.Now().UTC(),
		})
	}
}

func (r *Runtime) handleStart(cmd StartChatSessionCommand) {
	now := time.Now().UTC()
	state, err := NewChatSessionState(cmd.SessionID, cmd.Config, now)
	if err != nil {
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}

	r.mu.Lock()
	if _, exists := r.sessions[state.SessionID]; exists {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  state.SessionID,
			Message:    "chat session already exists",
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}

	r.sessions[state.SessionID] = &runtimeSession{state: state}
	snapshot := cloneChatSessionState(state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
}

func (r *Runtime) handleSubmit(cmd SubmitChatPromptCommand) {
	now := normalizedCommandTime(cmd.SubmittedAt)

	r.mu.Lock()
	session, ok := r.sessions[cmd.SessionID]
	if !ok {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "chat session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.BeginTurn(cmd.RequestID, cmd.Prompt, now); err != nil {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	streamState := cloneChatSessionState(session.state)
	streamCtx, cancel := context.WithCancel(r.ctx)
	session.cancel = cancel
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
	r.emit(ChatResponseStartedEvent{
		SessionID: cmd.SessionID,
		RequestID: cmd.RequestID,
		StartedAt: now,
	})

	r.streamWG.Go(func() {
		r.runStream(streamCtx, streamState)
	})
}

func (r *Runtime) handleUpdate(cmd UpdateChatSessionCommand) {
	now := time.Now().UTC()

	r.mu.Lock()
	session, ok := r.sessions[cmd.SessionID]
	if !ok {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "chat session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.ApplyPatch(cmd.Patch, now); err != nil {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
}

func (r *Runtime) handleCancel(cmd CancelChatRequestCommand) {
	now := normalizedCommandTime(cmd.RequestedAt)

	r.mu.Lock()
	session, ok := r.sessions[cmd.SessionID]
	if !ok {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "chat session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if cmd.RequestID != "" && session.state.ActiveRequestID != cmd.RequestID {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "requested chat turn is not active",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.MarkCancelling(now); err != nil {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	cancel := session.cancel
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
	if cancel != nil {
		cancel()
	}
}

func (r *Runtime) handleReset(cmd ResetChatSessionCommand) {
	now := normalizedCommandTime(cmd.RequestedAt)

	r.mu.Lock()
	session, ok := r.sessions[cmd.SessionID]
	if !ok {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "chat session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.ResetConversation(now); err != nil {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
}

func (r *Runtime) handleClose(cmd CloseChatSessionCommand) {
	now := normalizedCommandTime(cmd.RequestedAt)

	r.mu.Lock()
	session, ok := r.sessions[cmd.SessionID]
	if !ok {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "chat session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.Close(now); err != nil {
		r.mu.Unlock()
		r.emit(ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emit(ChatSessionSnapshotEvent{State: snapshot})
}

func (r *Runtime) runStream(ctx context.Context, sessionState ChatSessionState) {
	now := time.Now().UTC()
	maasToken, err := r.tokenSource(ctx, sessionState.Endpoint)
	if err != nil {
		r.failActiveTurn(sessionState.SessionID, sessionState.ActiveRequestID, err, now)
		return
	}

	result, err := r.streamer.StreamChatCompletion(ctx, sessionState, maasToken, func(delta string) error {
		return r.appendDelta(sessionState.SessionID, sessionState.ActiveRequestID, delta, time.Now().UTC())
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			r.cancelActiveTurn(sessionState.SessionID, sessionState.ActiveRequestID, time.Now().UTC())
			return
		}
		r.failActiveTurn(sessionState.SessionID, sessionState.ActiveRequestID, err, time.Now().UTC())
		return
	}

	r.completeActiveTurn(sessionState.SessionID, sessionState.ActiveRequestID, result, time.Now().UTC())
}

func (r *Runtime) appendDelta(sessionID, requestID, delta string, at time.Time) error {
	r.mu.Lock()

	session, ok := r.sessions[sessionID]
	if !ok {
		r.mu.Unlock()
		return errors.New("chat session not found")
	}
	if session.state.ActiveRequestID != requestID {
		r.mu.Unlock()
		return context.Canceled
	}
	if err := session.state.AppendAssistantDelta(delta, at); err != nil {
		r.mu.Unlock()
		return err
	}
	snapshot := session.state.PendingAssistant
	r.mu.Unlock()

	r.emit(ChatResponseDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
	return nil
}

func (r *Runtime) completeActiveTurn(sessionID, requestID string, result CompletionResult, at time.Time) {
	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		r.mu.Unlock()
		return
	}
	_ = session.state.CompleteTurn(result.FinishReason, result.Usage, at)
	session.cancel = nil
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emitMustDeliver(ChatResponseCompletedEvent{
		State:        snapshot,
		RequestID:    requestID,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		CompletedAt:  at,
	})
}

func (r *Runtime) cancelActiveTurn(sessionID, requestID string, at time.Time) {
	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		r.mu.Unlock()
		return
	}
	_ = session.state.CancelTurn(at)
	session.cancel = nil
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emitMustDeliver(ChatResponseCancelledEvent{
		State:       snapshot,
		RequestID:   requestID,
		CancelledAt: at,
	})
}

func (r *Runtime) failActiveTurn(sessionID, requestID string, err error, at time.Time) {
	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		r.mu.Unlock()
		return
	}
	_ = session.state.FailTurn(err, at)
	session.cancel = nil
	snapshot := cloneChatSessionState(session.state)
	r.mu.Unlock()

	r.emitMustDeliver(ChatSessionSnapshotEvent{State: snapshot})
	r.emitMustDeliver(ChatRuntimeErrorEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Message:    err.Error(),
		Fatal:      false,
		OccurredAt: at,
	})
}

func (r *Runtime) cancelAllSessions() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, session := range r.sessions {
		if session.cancel != nil {
			session.cancel()
		}
	}
}

func (r *Runtime) emit(event ChatEvent) {
	if r.eventBus == nil {
		return
	}
	if err := r.eventBus.Publish(r.ctx, runtimeEventTopic, event); err != nil {
		slog.Debug("event publish failed", "err", err)
	}
}

// emitMustDeliver publishes a terminal event (finish, error, cancel) using the
// bounded-blocking delivery path so it cannot be silently dropped when the
// subscriber buffer is saturated from high-frequency streaming deltas.
func (r *Runtime) emitMustDeliver(event ChatEvent) {
	if r.eventBus == nil {
		return
	}
	if err := r.eventBus.PublishMustDeliver(runtimeEventTopic, event); err != nil {
		slog.Debug("must-deliver publish failed", "err", err)
	}
}

func cloneChatSessionState(state *ChatSessionState) ChatSessionState {
	clone := *state
	if state.Messages != nil {
		clone.Messages = append([]ChatMessage(nil), state.Messages...)
	}
	return clone
}

func normalizedCommandTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}
