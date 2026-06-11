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

func TestCoordinatorDispatchesSessionLifecycleHooks(t *testing.T) {
	started := make(chan map[string]any, 1)
	shutdown := make(chan map[string]any, 1)

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: tools.NewRegistry(),
		OnPluginEvent: func(event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
			ctx := map[string]any{
				"event":      event,
				"session_id": sessionID,
			}
			switch event {
			case "session_start":
				started <- ctx
			case "session_shutdown":
				shutdown <- ctx
			}
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

func TestCoordinatorPublishesStartupEventsOnSubscribe(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: tools.NewRegistry(),
		StartupEvents: []chat.ChatEvent{
			chat.ChatRuntimeErrorEvent{Message: "extension failed", Fatal: false},
		},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	select {
	case event := <-sub.Events():
		runtimeErr, ok := event.(chat.ChatRuntimeErrorEvent)
		require.True(t, ok)
		require.Equal(t, "extension failed", runtimeErr.Message)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for startup event")
	}
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
	var hooksMu sync.Mutex
	var toolHooks []map[string]any
	var reasoningHooks []map[string]any
	parallel := false

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:          streamer,
		Registry:          registry,
		ParallelToolCalls: &parallel,
		ShowReasoning:     true,
		OnToolStarted: func(ctx map[string]any) {
			hooksMu.Lock()
			defer hooksMu.Unlock()
			toolHooks = append(toolHooks, ctx)
		},
		OnToolCompleted: func(ctx map[string]any) {
			hooksMu.Lock()
			defer hooksMu.Unlock()
			toolHooks = append(toolHooks, ctx)
		},
		OnReasoningDelta: func(ctx map[string]any) {
			hooksMu.Lock()
			defer hooksMu.Unlock()
			reasoningHooks = append(reasoningHooks, ctx)
		},
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
	hooksMu.Lock()
	defer hooksMu.Unlock()
	require.Len(t, reasoningHooks, 1)
	require.Equal(t, "reasoning_delta", reasoningHooks[0]["event"])
	require.Len(t, toolHooks, 2)
	require.Equal(t, "tool_call_started", toolHooks[0]["event"])
	require.Equal(t, "tool_call_completed", toolHooks[1]["event"])
}

func TestCoordinatorEmitsReasoningEventWhenHookExposureDisabled(t *testing.T) {
	var hooksMu sync.Mutex
	var reasoningHooks []map[string]any
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: &reasoningOnlyStreamer{},
		Registry: tools.NewRegistry(),
		OnReasoningDelta: func(ctx map[string]any) {
			hooksMu.Lock()
			defer hooksMu.Unlock()
			reasoningHooks = append(reasoningHooks, ctx)
		},
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
	hooksMu.Lock()
	defer hooksMu.Unlock()
	require.Empty(t, reasoningHooks)
}

func TestCoordinatorUnknownToolPublishesCompletedError(t *testing.T) {
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: &unknownToolStreamer{},
		Registry: tools.NewRegistry(),
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
