package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"github.com/stretchr/testify/require"
)

// newTestBus returns a Bus that is automatically closed when the test completes.
func newTestBus(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	return bus
}

type noopStreamer struct{}

func (noopStreamer) StreamChatCompletionFull(
	context.Context,
	chat.ChatSessionState,
	string,
	map[string]string,
	chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	return chat.CompletionResult{}, nil
}

type scriptedStreamer struct {
	calls int
}

var longToolArgument = strings.Repeat("x", toolSummaryMaxBytes+50)

func (s *scriptedStreamer) StreamChatCompletionFull(
	_ context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.calls++
	if s.calls == 1 {
		_ = cb.OnReasoningDelta("provider thought")
		_ = cb.OnToolCallDelta(chat.ChatToolCallDelta{
			Index: 0,
			ID:    "call_1",
			Type:  "function",
			Function: chat.ChatFunctionCallDelta{
				Name:      "echo",
				Arguments: `{"msg":"`,
			},
		})
		_ = cb.OnToolCallDelta(chat.ChatToolCallDelta{
			Index:    0,
			Function: chat.ChatFunctionCallDelta{Arguments: longToolArgument + `"}`},
		})
		args := `{"msg":"` + longToolArgument + `"}`
		return chat.CompletionResult{
			FinishReason:     "tool_calls",
			ReasoningContent: "provider thought",
			ToolCalls: []chat.ChatToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: chat.ChatFunctionCall{
					Name:      "echo",
					Arguments: args,
				},
			}},
		}, nil
	}
	_ = cb.OnDelta("done")
	return chat.CompletionResult{FinishReason: "stop"}, nil
}

type testSteerStreamer struct {
	mu            sync.Mutex
	calls         int
	lastMessages  []chat.ChatMessage
	firstCallDone chan struct{}
	steerSent     chan struct{}
}

func (s *testSteerStreamer) recordMessages(messages []chat.ChatMessage) {
	s.mu.Lock()
	s.lastMessages = messages
	s.mu.Unlock()
}

func (s *testSteerStreamer) getLastMessages() []chat.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastMessages
}

func (s *testSteerStreamer) StreamChatCompletionFull(
	ctx context.Context,
	session chat.ChatSessionState,
	bearerToken string,
	extraHeaders map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.calls++
	s.recordMessages(session.Messages)
	if s.calls == 1 {
		close(s.firstCallDone)
		return chat.CompletionResult{
			ToolCalls: []chat.ChatToolCall{
				{ID: "call_1", Type: "function", Function: chat.ChatFunctionCall{Name: "test_tool"}},
			},
		}, nil
	}
	if s.calls == 2 {
		// Return a second tool call so the turn loop enters another
		// iteration, giving injectSteering a chance to pick up the
		// steer that was sent after firstCallDone.
		<-s.steerSent
		return chat.CompletionResult{
			ToolCalls: []chat.ChatToolCall{
				{ID: "call_2", Type: "function", Function: chat.ChatFunctionCall{Name: "test_tool"}},
			},
		}, nil
	}
	return chat.CompletionResult{FinishReason: "stop"}, nil
}

func TestCoordinatorSteeringInjection(t *testing.T) {
	bus := newTestBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	firstCallDone := make(chan struct{})
	steerSent := make(chan struct{})

	streamer := &testSteerStreamer{
		firstCallDone: firstCallDone,
		steerSent:     steerSent,
	}

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Schema: tools.Schema{Name: "test_tool"},
		Execute: func(ctx context.Context, args json.RawMessage, bridge tools.UIBridge) (tools.Result, error) {
			return tools.Result{Content: "tool result"}, nil
		},
	})

	c, err := NewCoordinator(ctx, CoordinatorConfig{
		Bus: bus,
		TokenSource: func(ctx context.Context, p config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer: streamer,
		Registry: reg,
	})
	require.NoError(t, err)

	sessionID := "session_steer"
	err = c.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "http://test"},
			Model:    chat.ChatModelRef{ID: "test-model", URL: "http://test"},
		},
	})
	require.NoError(t, err)

	err = c.Send(chat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   "req_1",
		Prompt:      "initial prompt",
		SubmittedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	<-firstCallDone

	err = c.Send(chat.SteerChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   "req_1",
		Prompt:      "steering message",
		SubmittedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	close(steerSent)

	// Wait for coordinator turn loop to complete.
	time.Sleep(100 * time.Millisecond)

	// The last messages should contain the steering message.
	found := false
	for _, msg := range streamer.getLastMessages() {
		if msg.Role == chat.ChatRoleUser && msg.Content == "steering message" {
			found = true
			break
		}
	}
	require.True(t, found, "steering message should be injected in conversation history")
}

func TestCoordinatorDispatchesSessionLifecycleHooks(t *testing.T) {
	started := make(chan map[string]any, 1)
	shutdown := make(chan map[string]any, 1)

	bus := newTestBus(t)
	// Subscribe to fire-and-forget lifecycle events via the bus.
	pluginSub := eventbus.SubscribeFunc(bus.Client("test-observer"), func(evt chat.PluginLifecycleEvent) {
		ctx := map[string]any{
			"event":      evt.Event,
			"session_id": evt.SessionID,
		}
		switch evt.Event {
		case "session_start":
			started <- ctx
		case "session_shutdown":
			shutdown <- ctx
		}
	})
	defer pluginSub.Close()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: tools.NewRegistry(),
		OnPluginEvent: func(event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
			// Request-response events only; fire-and-forget events go
			// through the bus subscription above.
			return nil
		},
	})
	require.NoError(t, err)

	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: "session-1",
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))
	require.Equal(t, "session_start", receiveLifecycle(t, started)["event"])

	coordinator.Close()
	require.Equal(t, "session_shutdown", receiveLifecycle(t, shutdown)["event"])
}

func TestCoordinatorPublishesToolAndReasoningObservabilityEvents(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)},
		Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
			return tools.Result{Content: "echo result"}, nil
		},
	}))
	streamer := &scriptedStreamer{}
	parallel := false

	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:               bus,
		TokenSource:       func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:          streamer,
		Registry:          registry,
		ParallelToolCalls: &parallel,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "use a tool",
		SubmittedAt: time.Now().UTC(),
	}))

	var sawReasoning, sawDelta, sawStarted, sawToolCompleted, sawResponseCompleted bool
	deadline := time.After(time.Second)
	for !sawReasoning || !sawDelta || !sawStarted || !sawToolCompleted || !sawResponseCompleted {
		select {
		case event := <-sub.Events():
			switch ev := event.(type) {
			case chat.ChatReasoningDeltaEvent:
				require.Equal(t, "session-1", ev.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				require.Equal(t, "provider thought", ev.Snapshot)
				sawReasoning = true
			case chat.ChatToolCallDeltaEvent:
				require.Equal(t, "session-1", ev.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				require.Equal(t, "call_1", ev.CallID)
				require.Equal(t, "echo", ev.ToolName)
				if ev.Truncated {
					require.NotContains(t, ev.ArgumentsSummary, longToolArgument)
					sawDelta = true
				}
			case chat.ChatToolExecutionStartedEvent:
				require.Equal(t, "session-1", ev.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				require.Equal(t, "call_1", ev.CallID)
				require.Equal(t, "echo", ev.ToolName)
				require.NotContains(t, ev.ArgumentsSummary, longToolArgument)
				sawStarted = true
			case chat.ChatToolExecutionCompletedEvent:
				require.Equal(t, "session-1", ev.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				require.Equal(t, "call_1", ev.CallID)
				require.Equal(t, "success", ev.Status)
				require.False(t, ev.IsError)
				require.Contains(t, ev.ResultSummary, "echo result")
				sawToolCompleted = true
			case chat.ChatResponseCompletedEvent:
				require.Equal(t, "session-1", ev.State.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				sawResponseCompleted = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for observability events")
		}
	}
}

func TestCoordinatorEmitsReasoningDeltaEvent(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: &reasoningOnlyStreamer{},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()
	defer coordinator.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "think",
		SubmittedAt: time.Now().UTC(),
	}))

	var sawReasoning, sawCompleted bool
	deadline := time.After(time.Second)
	for !sawReasoning || !sawCompleted {
		select {
		case event := <-sub.Events():
			switch ev := event.(type) {
			case chat.ChatReasoningDeltaEvent:
				require.Equal(t, "hidden", ev.Snapshot)
				sawReasoning = true
			case chat.ChatResponseCompletedEvent:
				require.Equal(t, "session-1", ev.State.SessionID)
				require.Equal(t, "request-1", ev.RequestID)
				sawCompleted = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for reasoning event")
		}
	}
}

func TestCoordinatorUnknownToolPublishesCompletedError(t *testing.T) {
	bus := newTestBus(t)
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: &unknownToolStreamer{},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "use missing",
		SubmittedAt: time.Now().UTC(),
	}))

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-sub.Events():
			completed, ok := event.(chat.ChatToolExecutionCompletedEvent)
			if !ok {
				continue
			}
			require.Equal(t, "missing", completed.ToolName)
			require.Equal(t, "error", completed.Status)
			require.True(t, completed.IsError)
			require.Contains(t, completed.ResultSummary, "unknown tool")
			return
		case <-deadline:
			t.Fatal("timed out waiting for unknown tool completion event")
		}
	}
}

func TestCoordinatorReloadExtensionsWhileIdle(t *testing.T) {
	reloader := &fakeExtensionReloader{
		result: chat.ExtensionReloadResult{
			ExtensionCount: 2,
			Diagnostics: []chat.ExtensionDiagnostic{{
				ExtensionName: "demo",
				Severity:      "warning",
				Message:       "heads up",
			}},
		},
	}
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:          noopStreamer{},
		Registry:          tools.NewRegistry(),
		ExtensionReloader: reloader,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.ReloadExtensionsCommand{RequestedAt: time.Now().UTC()}))

	var sawReload, sawSuccess bool
	deadline := time.After(time.Second)
	for !sawReload || !sawSuccess {
		select {
		case event := <-sub.Events():
			switch ev := event.(type) {
			case chat.ExtensionsReloadedEvent:
				require.Equal(t, 2, ev.Result.ExtensionCount)
				require.Len(t, ev.Result.Diagnostics, 1)
				sawReload = true
			case chat.ChatNotificationEvent:
				if strings.Contains(ev.Message, "Reloaded extensions") {
					sawSuccess = true
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for reload events")
		}
	}
	require.Equal(t, 1, reloader.calls)
	require.True(t, reloader.lastIdle)
}

func TestCoordinatorReloadExtensionsWhileActiveRejects(t *testing.T) {
	reloader := &fakeExtensionReloader{}
	streamer := &blockingFullStreamer{started: make(chan struct{})}
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:          streamer,
		Registry:          tools.NewRegistry(),
		ExtensionReloader: reloader,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "hold",
		SubmittedAt: time.Now().UTC(),
	}))
	select {
	case <-streamer.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for streamer")
	}
	require.NoError(t, coordinator.Send(chat.ReloadExtensionsCommand{RequestedAt: time.Now().UTC()}))

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-sub.Events():
			notification, ok := event.(chat.ChatNotificationEvent)
			if !ok {
				continue
			}
			if strings.Contains(notification.Message, "only available while idle") {
				require.Equal(t, 0, reloader.calls)
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for busy reload notification")
		}
	}
}

func TestCoordinatorConfirmBridgeResponseAndCancel(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:      noopStreamer{},
		Registry:      tools.NewRegistry(),
		InteractiveUI: true,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	resultCh := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := coordinator.uiBridge.Confirm(context.Background(), "Title", "Continue?")
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	var requestID string
	select {
	case event := <-sub.Events():
		request, ok := event.(chat.InteractivePromptRequestedEvent)
		require.True(t, ok)
		require.Equal(t, chat.InteractivePromptConfirm, request.Kind)
		requestID = request.RequestID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt request")
	}
	require.NoError(t, coordinator.Send(chat.RespondInteractivePromptCommand{
		RequestID: requestID,
		Confirmed: true,
	}))
	select {
	case result := <-resultCh:
		require.True(t, result)
	case err := <-errCh:
		t.Fatalf("confirm returned error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt result")
	}

	errCh = make(chan error, 1)
	go func() {
		_, err := coordinator.uiBridge.Confirm(context.Background(), "Title", "Cancel?")
		errCh <- err
	}()
	select {
	case event := <-sub.Events():
		request := event.(chat.InteractivePromptRequestedEvent)
		require.NoError(t, coordinator.Send(chat.RespondInteractivePromptCommand{
			RequestID: request.RequestID,
			Canceled:  true,
		}))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancel prompt request")
	}
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, tools.ErrInteractiveCanceled)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled prompt result")
	}
}

func TestCoordinatorQuestionBridgeResponse(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:      noopStreamer{},
		Registry:      tools.NewRegistry(),
		InteractiveUI: true,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := coordinator.uiBridge.Input(context.Background(), "Question", "Your name?")
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	var requestID string
	select {
	case event := <-sub.Events():
		request, ok := event.(chat.InteractivePromptRequestedEvent)
		require.True(t, ok)
		require.Equal(t, chat.InteractivePromptQuestion, request.Kind)
		requestID = request.RequestID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt request")
	}
	require.NoError(t, coordinator.Send(chat.RespondInteractivePromptCommand{
		RequestID: requestID,
		Response:  "Tau",
	}))
	select {
	case result := <-resultCh:
		require.Equal(t, "Tau", result)
	case err := <-errCh:
		t.Fatalf("question returned error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt result")
	}
}

func TestCoordinatorPromptBridgeUnblocksOnClose(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:      noopStreamer{},
		Registry:      tools.NewRegistry(),
		InteractiveUI: true,
	})
	require.NoError(t, err)

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := coordinator.uiBridge.Confirm(context.Background(), "Title", "Continue?")
		errCh <- err
	}()

	select {
	case event := <-sub.Events():
		_, ok := event.(chat.InteractivePromptRequestedEvent)
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt request")
	}

	coordinator.Close()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, tools.ErrInteractiveCanceled) {
			t.Fatalf("confirm error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt cancellation")
	}
}

func TestCoordinatorRunsExtensionCommand(t *testing.T) {
	reloader := &fakeExtensionReloader{commandOutput: "hello from extension"}
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:          noopStreamer{},
		Registry:          tools.NewRegistry(),
		ExtensionReloader: reloader,
		InteractiveUI:     true,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()
	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.RunExtensionCommandCommand{Name: "hello", Args: "sam"}))

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-sub.Events():
			result, ok := event.(chat.ExtensionCommandResultEvent)
			if !ok {
				continue
			}
			require.Equal(t, "hello", result.Name)
			require.Equal(t, "hello from extension", result.Output)
			require.Equal(t, "hello", reloader.commandName)
			require.Equal(t, "sam", reloader.commandArgs)
			require.NotNil(t, reloader.commandUI)
			return
		case <-deadline:
			t.Fatal("timed out waiting for extension command result")
		}
	}
}

type reasoningOnlyStreamer struct{}

func (*reasoningOnlyStreamer) StreamChatCompletionFull(
	_ context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	_ = cb.OnReasoningDelta("hidden")
	_ = cb.OnDelta("answer")
	return chat.CompletionResult{FinishReason: "stop", ReasoningContent: "hidden"}, nil
}

type unknownToolStreamer struct {
	done bool
}

func (s *unknownToolStreamer) StreamChatCompletionFull(
	_ context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	if s.done {
		_ = cb.OnDelta("done")
		return chat.CompletionResult{FinishReason: "stop"}, nil
	}
	s.done = true
	_ = cb.OnToolCallDelta(chat.ChatToolCallDelta{
		Index:    0,
		ID:       "call_missing",
		Type:     "function",
		Function: chat.ChatFunctionCallDelta{Name: "missing", Arguments: `{}`},
	})
	return chat.CompletionResult{
		FinishReason: "tool_calls",
		ToolCalls: []chat.ChatToolCall{{
			ID:   "call_missing",
			Type: "function",
			Function: chat.ChatFunctionCall{
				Name:      "missing",
				Arguments: `{}`,
			},
		}},
	}, nil
}

type fakeExtensionReloader struct {
	calls         int
	lastIdle      bool
	result        chat.ExtensionReloadResult
	err           error
	commandName   string
	commandArgs   string
	commandUI     any
	commandOutput string
}

func (r *fakeExtensionReloader) ReloadExtensions(_ context.Context, idle bool) (chat.ExtensionReloadResult, error) {
	r.calls++
	r.lastIdle = idle
	return r.result, r.err
}

func (r *fakeExtensionReloader) ExtensionCommands() []chat.ExtensionCommand {
	return r.result.Commands
}

func (r *fakeExtensionReloader) RunExtensionCommand(_ context.Context, name, args string, ui any) (string, error) {
	r.commandName = name
	r.commandArgs = args
	r.commandUI = ui
	return r.commandOutput, nil
}

type blockingFullStreamer struct {
	started chan struct{}
	once    sync.Once
}

func (s *blockingFullStreamer) StreamChatCompletionFull(
	ctx context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	_ chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return chat.CompletionResult{}, ctx.Err()
}

func startTestSession(t *testing.T, coordinator *Coordinator) {
	t.Helper()
	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: "session-1",
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))
}

func receiveLifecycle(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case ctx := <-ch:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle event")
		return nil
	}
}
