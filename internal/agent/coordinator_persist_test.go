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
	dir := t.TempDir()
	rawStore, err := store.NewSQLiteStore(filepath.Join(dir, "sessions.db"), dir)
	require.NoError(t, err)
	mgr := sessions.NewManager(rawStore)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// startAndCloseTestSession starts a session and waits for its
// ChatSessionSnapshotEvent (proof the coordinator's loop actually processed
// the StartChatSessionCommand) before calling Close(). Send() only enqueues
// onto a buffered channel and returns immediately, so closing right after
// Send() without this wait races Close()'s ctx.Done() against the loop still
// having the start command queued — select picks between the two
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
// so --ephemeral is opt-in rather than a silent behaviour change.
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

	startAndCloseTestSession(t, coordinator, "persisted-session")

	count, err := mgr.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count, "sessions must persist by default")
}
