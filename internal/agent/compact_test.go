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
