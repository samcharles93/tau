package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

type compactTestStreamer struct {
	seen []chat.ChatSessionState
}

func (s *compactTestStreamer) StreamChatCompletionFull(
	_ context.Context,
	session chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.seen = append(s.seen, chat.CloneChatSessionState(&session))
	if cb.OnDelta != nil {
		_ = cb.OnDelta("summary of previous work")
	}
	return chat.CompletionResult{FinishReason: "stop"}, nil
}

func TestMaybeAutoCompactReplacesHistoryAndKeepsActivePrompt(t *testing.T) {
	bus := newTestBus(t)
	streamer := &compactTestStreamer{}
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:    streamer,
		Registry:    tools.NewRegistry(),
		AutoCompact: config.AutoCompactConfig{
			Enabled:        true,
			ThresholdRatio: 0.01,
			TargetRatio:    0.01,
		},
	})
	require.NoError(t, err)
	t.Cleanup(coordinator.Close)

	now := time.Now().UTC()
	state, err := chat.NewChatSessionState("session-compact", chat.ChatSessionConfig{
		Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test", Auth: config.AuthConfig{Type: config.AuthTypeNone}},
		Model: chat.ChatModelRef{
			ID: "model",
			Config: config.ModelConfig{
				ContextWindow: 200,
			},
		},
		SystemPrompt: "system prompt",
	}, now)
	require.NoError(t, err)
	state.Status = chat.ChatSessionStreaming
	state.ActiveRequestID = "req-1"
	state.Messages = []chat.ChatMessage{
		{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: strings.Repeat("old user ", 20), CreatedAt: now},
		{ID: chat.NewMessageID(), Role: chat.ChatRoleAssistant, Content: strings.Repeat("old assistant ", 20), CreatedAt: now},
		{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: strings.Repeat("more old user ", 20), CreatedAt: now},
		{ID: chat.NewMessageID(), Role: chat.ChatRoleAssistant, Content: strings.Repeat("more old assistant ", 20), CreatedAt: now},
		{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: "current request", CreatedAt: now},
	}
	currentID := state.Messages[len(state.Messages)-1].ID

	coordinator.mu.Lock()
	coordinator.sessions[state.SessionID] = &coordinatorSession{state: state}
	coordinator.mu.Unlock()

	compacted, ok, err := coordinator.maybeAutoCompact(context.Background(), *state, "")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, compacted.Messages, 2)
	require.Contains(t, compacted.Messages[0].Content, "summary of previous work")
	require.Equal(t, chat.ChatRoleUser, compacted.Messages[0].Role)
	require.Equal(t, "current request", compacted.Messages[1].Content)
	require.Equal(t, currentID, compacted.Messages[1].ID)

	require.Len(t, streamer.seen, 1)
	require.Len(t, streamer.seen[0].Tools, 0)
	require.Empty(t, streamer.seen[0].ActiveRequestID)
	require.Contains(t, streamer.seen[0].SystemPrompt, "You are compacting")
	require.Contains(t, streamer.seen[0].Messages[len(streamer.seen[0].Messages)-1].Content, "Compact the conversation history")
}

// TestAutoCompactUsesCompactModelContextWindow guards tau-6dn: when auto-
// compact is configured with a custom model, the MaxTokens budget must be
// derived from that model's context window (via ModelLookup), not the
// session model's. A fallback to the session window applies when the lookup
// misses.
func TestAutoCompactUsesCompactModelContextWindow(t *testing.T) {
	newState := func(t *testing.T) chat.ChatSessionState {
		now := time.Now().UTC()
		state, err := chat.NewChatSessionState("session-compact-model", chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model: chat.ChatModelRef{
				ID:     "big",
				Config: config.ModelConfig{ContextWindow: 200},
			},
			SystemPrompt: "system prompt",
		}, now)
		require.NoError(t, err)
		state.Status = chat.ChatSessionStreaming
		state.ActiveRequestID = "req-1"
		state.Messages = []chat.ChatMessage{
			{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: strings.Repeat("old user ", 20), CreatedAt: now},
			{ID: chat.NewMessageID(), Role: chat.ChatRoleAssistant, Content: strings.Repeat("old assistant ", 20), CreatedAt: now},
			{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: strings.Repeat("more old user ", 20), CreatedAt: now},
			{ID: chat.NewMessageID(), Role: chat.ChatRoleUser, Content: "current request", CreatedAt: now},
		}
		return *state
	}

	newCoordinator := func(t *testing.T, streamer *compactTestStreamer, lookup ModelLookup) *Coordinator {
		bus := newTestBus(t)
		c, err := NewCoordinator(context.Background(), CoordinatorConfig{
			Bus:         bus,
			TokenSource: func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
			Streamer:    streamer,
			Registry:    tools.NewRegistry(),
			ModelLookup: lookup,
			AutoCompact: config.AutoCompactConfig{
				Enabled:        true,
				ThresholdRatio: 0.5,
				TargetRatio:    0.25,
				Model:          "small",
			},
		})
		require.NoError(t, err)
		t.Cleanup(c.Close)
		return c
	}

	t.Run("uses compact model window when lookup resolves", func(t *testing.T) {
		streamer := &compactTestStreamer{}
		c := newCoordinator(t, streamer, func(id string) *chat.ChatModelRef {
			if id == "small" {
				return &chat.ChatModelRef{ID: "small", Config: config.ModelConfig{ContextWindow: 50}}
			}
			return nil
		})
		state := newState(t)
		c.mu.Lock()
		c.sessions[state.SessionID] = &coordinatorSession{state: &state}
		c.mu.Unlock()

		_, ok, err := c.maybeAutoCompact(context.Background(), state, "")
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, streamer.seen, 1)
		require.Equal(t, "small", streamer.seen[0].Model.ID)
		require.Equal(t, 12, streamer.seen[0].Parameters.MaxTokens, "budget must come from the compact model's window (50 * 0.25), not the session model's (200 * 0.25 = 50)")
	})

	t.Run("falls back to session model window when lookup misses", func(t *testing.T) {
		streamer := &compactTestStreamer{}
		c := newCoordinator(t, streamer, func(string) *chat.ChatModelRef { return nil })
		state := newState(t)
		c.mu.Lock()
		c.sessions[state.SessionID] = &coordinatorSession{state: &state}
		c.mu.Unlock()

		_, ok, err := c.maybeAutoCompact(context.Background(), state, "")
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, streamer.seen, 1)
		require.Equal(t, 50, streamer.seen[0].Parameters.MaxTokens, "fallback must use the session model's window (200 * 0.25)")
	})
}
