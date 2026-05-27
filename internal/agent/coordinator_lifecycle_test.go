package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/stretchr/testify/require"
)

type noopStreamer struct{}

func (noopStreamer) StreamChatCompletionFull(
	context.Context,
	chat.ChatSessionState,
	string,
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
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: tools.NewRegistry(),
		OnSessionStart: func(ctx map[string]any) {
			started <- ctx
		},
		OnSessionShutdown: func(ctx map[string]any) {
			shutdown <- ctx
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

	sub, err := coordinator.SubscribeEvents(1)
	require.NoError(t, err)
	defer sub.Unsubscribe()

	select {
	case event := <-sub.Channel():
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

	sub, err := coordinator.SubscribeEvents(16)
	require.NoError(t, err)
	defer sub.Unsubscribe()
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
		case event := <-sub.Channel():
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

	sub, err := coordinator.SubscribeEvents(8)
	require.NoError(t, err)
	defer sub.Unsubscribe()
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
		case event := <-sub.Channel():
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
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: &unknownToolStreamer{},
		Registry: tools.NewRegistry(),
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents(16)
	require.NoError(t, err)
	defer sub.Unsubscribe()

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
		case event := <-sub.Channel():
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

type reasoningOnlyStreamer struct{}

func (*reasoningOnlyStreamer) StreamChatCompletionFull(
	_ context.Context,
	_ chat.ChatSessionState,
	_ string,
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
