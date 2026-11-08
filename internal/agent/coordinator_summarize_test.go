package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
)

// summarizeTestStreamer records every session state it is called with and
// streams a fixed summary via OnDelta (or fails when err is set).
type summarizeTestStreamer struct {
	mu   sync.Mutex
	seen []chat.ChatSessionState
	err  error
}

func (s *summarizeTestStreamer) StreamChatCompletionFull(
	_ context.Context,
	session chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.mu.Lock()
	s.seen = append(s.seen, chat.CloneChatSessionState(&session))
	s.mu.Unlock()
	if s.err != nil {
		return chat.CompletionResult{}, s.err
	}
	if cb.OnDelta != nil {
		_ = cb.OnDelta("TL;DR: we fixed config validation")
	}
	return chat.CompletionResult{FinishReason: "stop", Usage: chat.ChatUsage{TotalTokens: 123}}, nil
}

func (s *summarizeTestStreamer) lastSeen() chat.ChatSessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[len(s.seen)-1]
}

func (s *summarizeTestStreamer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func newSummarizeTestCoordinator(t *testing.T, streamer Streamer, appCfg *config.Config) (*Coordinator, *eventbus.Subscriber[chat.ChatEvent]) {
	t.Helper()
	bus := newTestBus(t)
	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer:  streamer,
		Registry:  tools.NewRegistry(),
		AppConfig: appCfg,
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	sub, err := c.SubscribeEvents()
	require.NoError(t, err)
	return c, sub
}

// addSummarizeTestSession injects a session with one message per content
// string. Contents prefixed with "a:" become assistant messages, "t:" tool
// messages (ToolCallID set), everything else user messages.
func addSummarizeTestSession(t *testing.T, c *Coordinator, sessionID string, contents ...string) *chat.ChatSessionState {
	t.Helper()
	now := time.Now().UTC()
	state, err := chat.NewChatSessionState(sessionID, chat.ChatSessionConfig{
		Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test", Auth: config.AuthConfig{Type: config.AuthTypeNone}},
		Model:    chat.ChatModelRef{ID: "test-model"},
	}, now)
	require.NoError(t, err)
	for _, content := range contents {
		role := chat.ChatRoleUser
		switch {
		case strings.HasPrefix(content, "a:"):
			role = chat.ChatRoleAssistant
			content = strings.TrimSpace(strings.TrimPrefix(content, "a:"))
		case strings.HasPrefix(content, "t:"):
			role = chat.ChatRoleTool
			content = strings.TrimSpace(strings.TrimPrefix(content, "t:"))
		}
		msg := chat.ChatMessage{ID: chat.NewMessageID(), Role: role, Content: content, CreatedAt: now}
		if role == chat.ChatRoleTool {
			msg.ToolCallID = "call_1"
		}
		require.NoError(t, state.AppendStandaloneMessage(role, content, now))
		_ = msg
	}
	c.mu.Lock()
	c.sessions[sessionID] = &coordinatorSession{state: state}
	c.mu.Unlock()
	return state
}

func sessionMessagesSnapshot(c *Coordinator, sessionID string) []chat.ChatMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]chat.ChatMessage(nil), c.sessions[sessionID].state.Messages...)
}

func TestSummarizeSessionEmitsEventWithoutMutatingState(t *testing.T) {
	streamer := &summarizeTestStreamer{}
	c, sub := newSummarizeTestCoordinator(t, streamer, nil)
	state := addSummarizeTestSession(t, c, "sess-sum",
		"fix the login bug", "a: I found the root cause in auth.go", "ship it?", "a: yes, merged")

	before := sessionMessagesSnapshot(c, "sess-sum")

	require.NoError(t, c.Send(chat.SummarizeSessionCommand{
		SessionID:   "sess-sum",
		RequestedAt: time.Now().UTC(),
	}))

	evt := drainUntil[chat.SessionSummarizedEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionSummarizedEvent)
		return ok
	})
	require.Equal(t, "sess-sum", evt.SessionID)
	require.Equal(t, "TL;DR: we fixed config validation", evt.Summary)
	require.Equal(t, 4, evt.MessageCount)
	require.Equal(t, 123, evt.Usage.TotalTokens)

	// Non-destructive: the session history is byte-for-byte unchanged and no
	// summary message was appended.
	after := sessionMessagesSnapshot(c, "sess-sum")
	require.Equal(t, before, after)
	require.Equal(t, state.Messages, after)

	// The LLM call saw a tool-less clone with the summarise prompt and an
	// explicit display-only instruction as the final user message.
	require.Equal(t, 1, streamer.callCount())
	seen := streamer.lastSeen()
	require.Empty(t, seen.Tools)
	require.Empty(t, seen.ActiveRequestID)
	require.Contains(t, seen.SystemPrompt, "summar")
	last := seen.Messages[len(seen.Messages)-1]
	require.Equal(t, chat.ChatRoleUser, last.Role)
	require.Contains(t, last.Content, "NOT being modified")
	// The whole conversation (4 messages) plus the instruction went in.
	require.Len(t, seen.Messages, 5)
}

func TestSummarizeSessionLastNWindow(t *testing.T) {
	streamer := &summarizeTestStreamer{}
	c, sub := newSummarizeTestCoordinator(t, streamer, nil)
	addSummarizeTestSession(t, c, "sess-n",
		"one", "a: two", "three", "a: four", "five")

	require.NoError(t, c.Send(chat.SummarizeSessionCommand{
		SessionID:   "sess-n",
		LastN:       2,
		RequestedAt: time.Now().UTC(),
	}))

	evt := drainUntil[chat.SessionSummarizedEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionSummarizedEvent)
		return ok
	})
	require.Equal(t, 2, evt.MessageCount)

	seen := streamer.lastSeen()
	// Last 2 messages + instruction; the instruction mentions the window.
	require.Len(t, seen.Messages, 3)
	require.Equal(t, "four", seen.Messages[0].Content)
	require.Equal(t, "five", seen.Messages[1].Content)
	require.Contains(t, seen.Messages[2].Content, "last 2 of 5")
}

func TestSummarizeSessionNotFound(t *testing.T) {
	c, sub := newSummarizeTestCoordinator(t, &summarizeTestStreamer{}, nil)
	require.NoError(t, c.Send(chat.SummarizeSessionCommand{
		SessionID:   "nope",
		RequestedAt: time.Now().UTC(),
	}))
	evt := drainUntil[chat.ChatRuntimeErrorEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		errEvt, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok && errEvt.SessionID == "nope"
	})
	require.False(t, evt.Fatal)
	require.Contains(t, evt.Message, "session not found")
}

func TestSummarizeSessionEmptyConversation(t *testing.T) {
	c, sub := newSummarizeTestCoordinator(t, &summarizeTestStreamer{}, nil)
	addSummarizeTestSession(t, c, "sess-empty")
	require.NoError(t, c.Send(chat.SummarizeSessionCommand{
		SessionID:   "sess-empty",
		RequestedAt: time.Now().UTC(),
	}))
	notif := drainUntil[chat.ChatNotificationEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	require.Contains(t, notif.Message, "nothing to summarize")
}

func TestSummarizeSessionStreamerError(t *testing.T) {
	streamer := &summarizeTestStreamer{err: errors.New("provider exploded")}
	c, sub := newSummarizeTestCoordinator(t, streamer, nil)
	addSummarizeTestSession(t, c, "sess-err", "hello")
	require.NoError(t, c.Send(chat.SummarizeSessionCommand{
		SessionID:   "sess-err",
		RequestedAt: time.Now().UTC(),
	}))
	evt := drainUntil[chat.ChatRuntimeErrorEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	require.Contains(t, evt.Message, "summarize failed")
	require.Contains(t, evt.Message, "provider exploded")
}

func TestSummarizeWindowDropsLeadingToolMessages(t *testing.T) {
	now := time.Now().UTC()
	msgs := []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "u1", CreatedAt: now},
		{Role: chat.ChatRoleAssistant, Content: "a1", CreatedAt: now},
		{Role: chat.ChatRoleTool, Content: "tool result", ToolCallID: "c1", CreatedAt: now},
		{Role: chat.ChatRoleUser, Content: "u2", CreatedAt: now},
		{Role: chat.ChatRoleAssistant, Content: "a2", CreatedAt: now},
	}

	// No limit: everything is kept.
	require.Len(t, summarizeWindow(msgs, 0), 5)
	require.Len(t, summarizeWindow(msgs, 99), 5)

	// Last 3 starts on the tool message; it is dropped so the window begins
	// on a well-formed user message.
	got := summarizeWindow(msgs, 3)
	require.Len(t, got, 2)
	require.Equal(t, "u2", got[0].Content)
	require.Equal(t, "a2", got[1].Content)
}

func TestConfigCheckEmitsAllIssuesAndNotification(t *testing.T) {
	appCfg := &config.Config{
		DefaultProvider: "ghost", // warning: not in providers list
		Providers: []config.ProviderConfig{
			{Name: "broken", BaseURL: "", Auth: config.AuthConfig{Type: config.AuthTypeNone}}, // error
		},
		Updates: config.UpdatesConfig{Mode: "bogus"}, // error
	}
	c, sub := newSummarizeTestCoordinator(t, &summarizeTestStreamer{}, appCfg)

	require.NoError(t, c.Send(chat.ConfigCheckCommand{RequestedAt: time.Now().UTC()}))

	evt := drainUntil[chat.ConfigCheckedEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ConfigCheckedEvent)
		return ok
	})
	require.False(t, evt.Valid)
	require.Len(t, evt.Issues, 3)
	require.True(t, evt.Issues[0].IsError())
	require.Contains(t, evt.Issues[0].Message, "base_url is required")
	require.True(t, evt.Issues[1].IsError())
	require.Contains(t, evt.Issues[1].Message, "unsupported mode")
	require.Equal(t, config.IssueSeverityWarning, evt.Issues[2].Severity)
	require.Contains(t, evt.Issues[2].Message, "default_provider")

	notif := drainUntil[chat.ChatNotificationEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	require.Equal(t, chat.ChatNotificationError, notif.Level)
	require.Contains(t, notif.Message, "2 error(s), 1 warning(s)")
}

func TestConfigCheckValidConfig(t *testing.T) {
	appCfg := &config.Config{
		DefaultProvider: "local",
		Providers: []config.ProviderConfig{
			{Name: "local", BaseURL: "http://localhost:11434", Auth: config.AuthConfig{Type: config.AuthTypeNone}},
		},
	}
	c, sub := newSummarizeTestCoordinator(t, &summarizeTestStreamer{}, appCfg)

	require.NoError(t, c.Send(chat.ConfigCheckCommand{RequestedAt: time.Now().UTC()}))

	evt := drainUntil[chat.ConfigCheckedEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ConfigCheckedEvent)
		return ok
	})
	require.True(t, evt.Valid)
	require.Empty(t, evt.Issues)

	notif := drainUntil[chat.ChatNotificationEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	require.Equal(t, chat.ChatNotificationInfo, notif.Level)
	require.Contains(t, notif.Message, "no issues found")
}

func TestConfigCheckWithoutLoadedConfig(t *testing.T) {
	c, sub := newSummarizeTestCoordinator(t, &summarizeTestStreamer{}, nil)
	require.NoError(t, c.Send(chat.ConfigCheckCommand{RequestedAt: time.Now().UTC()}))
	notif := drainUntil[chat.ChatNotificationEvent](t, sub, 3*time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	require.Equal(t, chat.ChatNotificationWarn, notif.Level)
	require.Contains(t, notif.Message, "unavailable")
}
