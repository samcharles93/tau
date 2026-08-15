package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestCoordinatorSubagentToolResolvesSessionModel(t *testing.T) {
	bus := newTestBus(t)
	registry := tools.NewRegistry()

	var gotProvider, gotModel, gotPrompt string
	executor := func(_ context.Context, provider, model, prompt string) (string, error) {
		gotProvider, gotModel, gotPrompt = provider, model, prompt
		return "nested conclusion", nil
	}

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:              bus,
		TokenSource:      func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:         noopStreamer{},
		Registry:         registry,
		SubagentExecutor: executor,
	})
	require.NoError(t, err)
	t.Cleanup(coordinator.Close)

	// The "subagent" tool must be registered whenever an executor is wired.
	tool, ok := registry.Get("subagent")
	require.True(t, ok, "subagent tool must be registered when SubagentExecutor is set")

	// Start a session with a known provider/model.
	sessionID := "subagent-test-session"
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("subagent-test"))
	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test-provider", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "test-model", URL: "https://example.test"},
		},
	}))
	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		snap, ok := e.(chat.ChatSessionSnapshotEvent)
		return ok && snap.State.SessionID == sessionID
	})

	// Execute the tool with a bridge scoped to that session.
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"do the thing"}`),
		&loggingUIBridge{UIBridge: coordinator.uiBridge, sessionID: sessionID})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "nested conclusion", res.Content)
	require.Equal(t, "test-provider", gotProvider, "sub-agent must run on the session's provider")
	require.Equal(t, "test-model", gotModel, "sub-agent must run on the session's model")
	require.Equal(t, "do the thing", gotPrompt)
}

func TestCoordinatorSubagentToolRejectsUnknownSession(t *testing.T) {
	bus := newTestBus(t)
	registry := tools.NewRegistry()

	called := false
	executor := func(context.Context, string, string, string) (string, error) {
		called = true
		return "", nil
	}

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:              bus,
		TokenSource:      func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:         noopStreamer{},
		Registry:         registry,
		SubagentExecutor: executor,
	})
	require.NoError(t, err)
	t.Cleanup(coordinator.Close)

	tool, ok := registry.Get("subagent")
	require.True(t, ok)

	// No session exists for this ID: the runner must fail in-band without
	// invoking the executor, so the model gets a clear, retryable message.
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"anything"}`),
		&loggingUIBridge{UIBridge: coordinator.uiBridge, sessionID: "no-such-session"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Content, "no model selected")
	require.False(t, called, "executor must not run without a resolvable model")
}

func TestCoordinatorWithoutExecutorHasNoSubagentTool(t *testing.T) {
	bus := newTestBus(t)
	registry := tools.NewRegistry()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:    noopStreamer{},
		Registry:    registry,
	})
	require.NoError(t, err)
	t.Cleanup(coordinator.Close)

	_, ok := registry.Get("subagent")
	require.False(t, ok, "subagent tool must be absent when SubagentExecutor is nil")
}
