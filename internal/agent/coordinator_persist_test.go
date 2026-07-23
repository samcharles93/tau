package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

// newTestSessionManager returns a Manager backed by a throwaway SQLite store
// under t.TempDir(), so persistence tests never touch the user's real
// session store.
func newTestSessionManager(t *testing.T) *sessions.Manager {
	t.Helper()
	mgr, _ := newTestSessionManagerAndStore(t)
	return mgr
}

func newTestSessionManagerAndStore(t *testing.T) (*sessions.Manager, *store.SQLiteStore) {
	t.Helper()
	dir := t.TempDir()
	rawStore, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "sessions.db"), dir)
	require.NoError(t, err)
	mgr := sessions.NewManager(rawStore)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, rawStore
}

// startAndCloseTestSession starts a session and waits for its
// ChatSessionSnapshotEvent (proof the coordinator's loop actually processed
// the StartChatSessionCommand) before calling Close(). Send() only enqueues
// onto a buffered channel and returns immediately, so closing right after
// Send() without this wait races Close()'s ctx.Done() against the loop still
// having the start command queued - select picks between the two
// pseudo-randomly, and a "session not found" outcome (nothing to persist)
// is a real possible result, not a hang.
func startAndCloseTestSession(t *testing.T, coordinator *Coordinator, sessionID string) {
	t.Helper()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-sub.Events():
			if snap, ok := event.(chat.ChatSessionSnapshotEvent); ok && snap.State.SessionID == sessionID {
				coordinator.Close()
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for session start snapshot")
		}
	}
}

// startSubmitAndCloseTestSession starts a session, submits one prompt, waits
// for its ChatResponseCompletedEvent (proof the message actually landed in
// session state before Close() snapshots it - same race Send() has for the
// start command, see startAndCloseTestSession), then closes.
func startSubmitAndCloseTestSession(t *testing.T, coordinator *Coordinator, sessionID string) {
	t.Helper()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))

	deadline := time.After(time.Second)
	promptSent := false
	for {
		select {
		case event := <-sub.Events():
			switch ev := event.(type) {
			case chat.ChatSessionSnapshotEvent:
				if !promptSent && ev.State.SessionID == sessionID {
					promptSent = true
					require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
						SessionID:   sessionID,
						RequestID:   "req_1",
						Prompt:      "hello",
						SubmittedAt: time.Now().UTC(),
					}))
				}
			case chat.ChatResponseCompletedEvent:
				if ev.State.SessionID == sessionID {
					coordinator.Close()
					return
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for prompt completion")
		}
	}
}

type shutdownBlockingStreamer struct {
	started   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
}

func newShutdownBlockingStreamer() *shutdownBlockingStreamer {
	return &shutdownBlockingStreamer{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (s *shutdownBlockingStreamer) StreamChatCompletionFull(
	ctx context.Context,
	_ chat.ChatSessionState,
	_ string,
	_ map[string]string,
	_ chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	return chat.CompletionResult{}, ctx.Err()
}

// TestCoordinatorNoPersistSkipsSessionStore guards --ephemeral: with
// NoPersist set, closing the coordinator must not write a row to the
// session store.
func TestCoordinatorNoPersistSkipsSessionStore(t *testing.T) {
	bus := newTestBus(t)
	mgr := newTestSessionManager(t)

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:       noopStreamer{},
		Registry:       tools.NewRegistry(),
		SessionManager: mgr,
		NoPersist:      true,
	})
	require.NoError(t, err)

	startAndCloseTestSession(t, coordinator, "ephemeral-session")

	count, err := mgr.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, count, "ephemeral session must not be written to the session store")
}

// TestCoordinatorPersistsByDefault guards the flag's default: every existing
// caller that leaves NoPersist unset (zero value false) must keep persisting,
// so --ephemeral is opt-in rather than a silent behaviour change. Submits a
// prompt before closing - a session with at least one message, matching
// persistSession's empty-session skip (see TestCoordinatorSkipsEmptySession).
func TestCoordinatorPersistsByDefault(t *testing.T) {
	bus := newTestBus(t)
	mgr := newTestSessionManager(t)

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:       noopStreamer{},
		Registry:       tools.NewRegistry(),
		SessionManager: mgr,
	})
	require.NoError(t, err)

	startSubmitAndCloseTestSession(t, coordinator, "persisted-session")

	count, err := mgr.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count, "sessions must persist by default")
}

// TestCoordinatorSkipsEmptySession guards against clutter in the session
// list: a session that was started (e.g. the TUI launched) but never sent a
// single message must not be written to the store - there's nothing to
// resume into, and an empty row previously showed a misleading "Session
// saved" summary on exit and a dead entry in /session and --resume.
func TestCoordinatorSkipsEmptySession(t *testing.T) {
	bus := newTestBus(t)
	mgr := newTestSessionManager(t)

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:       noopStreamer{},
		Registry:       tools.NewRegistry(),
		SessionManager: mgr,
	})
	require.NoError(t, err)

	startAndCloseTestSession(t, coordinator, "empty-session")

	count, err := mgr.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, count, "an empty session (no messages ever sent) must not be persisted")
}

// TestCoordinatorDoesNotDropMessageSentRightBeforeClose reproduces the real
// shutdown race: Send() only enqueues onto the buffered c.commands channel
// and returns - it never waits for loop() to actually process the command.
// A real TUI submits a prompt via a fire-and-forget tea.Cmd and, on the very
// next user action, may call Close() without ever waiting for that command
// to be handled. Before the drainPendingCommands fix, loop()'s
// select{<-ctx.Done(), <-c.commands} could pick shutdown over the
// already-enqueued SubmitChatPromptCommand, silently dropping it -
// BeginTurn never ran, so the message never reached session.state, and the
// persisted session came out empty even though the user typed something.
func TestCoordinatorDoesNotDropMessageSentRightBeforeClose(t *testing.T) {
	bus := newTestBus(t)
	mgr := newTestSessionManager(t)

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:       noopStreamer{},
		Registry:       tools.NewRegistry(),
		SessionManager: mgr,
	})
	require.NoError(t, err)

	sessionID := "race-session"
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))
	deadline := time.After(time.Second)
	for {
		event := <-sub.Events()
		if snap, ok := event.(chat.ChatSessionSnapshotEvent); ok && snap.State.SessionID == sessionID {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for session start snapshot")
		default:
		}
	}

	// The crux of the reproduction: submit, then close IMMEDIATELY - no
	// wait for any acknowledgment that the submit was processed, matching
	// what the real TUI does.
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   "req_1",
		Prompt:      "hello, are you there?",
		SubmittedAt: time.Now().UTC(),
	}))
	coordinator.Close()

	loaded, err := mgr.Load(context.Background(), sessionID, nil)
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 1, "the message sent right before Close() must survive to the persisted session")
	require.Equal(t, "hello, are you there?", loaded.Messages[0].Content)
}

func TestCoordinatorShutdownPersistsCancelledTurnState(t *testing.T) {
	bus := newTestBus(t)
	mgr, rawStore := newTestSessionManagerAndStore(t)
	streamer := newShutdownBlockingStreamer()

	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "token", nil
		},
		Streamer:       streamer,
		Registry:       tools.NewRegistry(),
		SessionManager: mgr,
	})
	require.NoError(t, err)

	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "persist this prompt",
		SubmittedAt: time.Now().UTC(),
	}))

	select {
	case <-streamer.started:
	case <-time.After(time.Second):
		t.Fatal("streamer never invoked")
	}

	closeDone := make(chan struct{})
	go func() {
		coordinator.Close()
		close(closeDone)
	}()

	select {
	case <-streamer.cancelled:
	case <-time.After(time.Second):
		t.Fatal("streamer context was not cancelled during shutdown")
	}

	var (
		persisted chat.ChatSessionState
		loadErr   error
	)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		persisted, loadErr = rawStore.Load(context.Background(), "session-1")
		if loadErr == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	close(streamer.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not finish closing")
	}

	require.NoError(t, loadErr)
	require.Equal(t, chat.ChatSessionIdle, persisted.Status)
	require.Len(t, persisted.Messages, 1)
	require.Equal(t, "persist this prompt", persisted.Messages[0].Content)
}
