package agent

import (
	"context"
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
