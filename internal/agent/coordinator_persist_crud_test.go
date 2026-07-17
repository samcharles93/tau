package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/stretchr/testify/require"
)

// newTestCoordinatorWithManager creates a coordinator wired to the given
// session manager (nil is valid - it exercises the "persistence not
// available" paths). Unlike startAndCloseTestSession's throwaway
// coordinators, this one is left running so a test can issue several
// commands (list/load/delete/export) against a store populated earlier in
// the same test.
func newTestCoordinatorWithManager(t *testing.T, mgr *sessions.Manager) *Coordinator {
	t.Helper()
	bus := newTestBus(t)
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
	t.Cleanup(coordinator.Close)
	return coordinator
}

// drainUntil reads events off sub until match returns true, failing the test
// if timeout elapses first.
func drainUntil(t *testing.T, sub *eventbus.Subscriber[chat.ChatEvent], timeout time.Duration, match func(chat.ChatEvent) bool) chat.ChatEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case event := <-sub.Events():
			if match(event) {
				return event
			}
		case <-deadline:
			t.Fatal("timed out waiting for expected event")
			return nil
		}
	}
}

// persistSessionWithMessage starts a session, submits one prompt, and closes
// just that session (chat.CloseChatSessionCommand) rather than the whole
// coordinator - so the coordinator keeps running and the caller can issue
// further List/Load/Delete/Export commands against the now-persisted row.
func persistSessionWithMessage(t *testing.T, coordinator *Coordinator, sub *eventbus.Subscriber[chat.ChatEvent], sessionID, prompt string) {
	t.Helper()

	require.NoError(t, coordinator.Send(chat.StartChatSessionCommand{
		SessionID: sessionID,
		Config: chat.ChatSessionConfig{
			Provider: config.ProviderConfig{Name: "test", BaseURL: "https://example.test"},
			Model:    chat.ChatModelRef{ID: "model", URL: "https://example.test"},
		},
	}))
	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		snap, ok := e.(chat.ChatSessionSnapshotEvent)
		return ok && snap.State.SessionID == sessionID
	})

	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   "req_" + sessionID,
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	}))
	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		done, ok := e.(chat.ChatResponseCompletedEvent)
		return ok && done.State.SessionID == sessionID
	})

	require.NoError(t, coordinator.Send(chat.CloseChatSessionCommand{
		SessionID:   sessionID,
		RequestedAt: time.Now().UTC(),
	}))
	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		snap, ok := e.(chat.ChatSessionSnapshotEvent)
		return ok && snap.State.SessionID == sessionID && snap.State.Status == chat.ChatSessionClosed
	})
}

// --- ListSessionsCommand ---

func TestHandleListSessions_NoSessionManager_EmitsWarning(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.ListSessionsCommand{Limit: 10}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	notif := event.(chat.ChatNotificationEvent)
	require.Equal(t, chat.ChatNotificationWarn, notif.Level)
	require.Contains(t, notif.Message, "not available")
}

func TestHandleListSessions_ReturnsPersistedSessions(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "list-session-1", "hello one")
	persistSessionWithMessage(t, coordinator, sub, "list-session-2", "hello two")

	require.NoError(t, coordinator.Send(chat.ListSessionsCommand{Limit: 10}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionsListedEvent)
		return ok
	})
	listed := event.(chat.SessionsListedEvent)
	require.Len(t, listed.Sessions, 2)

	ids := make([]string, len(listed.Sessions))
	for i, s := range listed.Sessions {
		ids[i] = s.ID
	}
	require.ElementsMatch(t, []string{"list-session-1", "list-session-2"}, ids)
}

// --- LoadSessionCommand ---

func TestHandleLoadSession_NoSessionManager_EmitsError(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.LoadSessionCommand{SessionID: "missing"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	errEvent := event.(chat.ChatRuntimeErrorEvent)
	require.False(t, errEvent.Fatal)
	require.Contains(t, errEvent.Message, "not available")
}

func TestHandleLoadSession_UnknownSession_EmitsError(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.LoadSessionCommand{SessionID: "does-not-exist"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	errEvent := event.(chat.ChatRuntimeErrorEvent)
	require.Equal(t, "does-not-exist", errEvent.SessionID)
	require.False(t, errEvent.Fatal)
	require.Contains(t, errEvent.Message, "loading session")
}

func TestHandleLoadSession_LoadsPersistedStateIntoMemory(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "load-session-1", "remember this")

	// handleClose removes the session from the in-memory map once persisted.
	coordinator.mu.Lock()
	_, stillActive := coordinator.sessions["load-session-1"]
	coordinator.mu.Unlock()
	require.False(t, stillActive, "closed session must be evicted from the in-memory map before Load")

	require.NoError(t, coordinator.Send(chat.LoadSessionCommand{SessionID: "load-session-1"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionLoadedEvent)
		return ok
	})
	loaded := event.(chat.SessionLoadedEvent)
	require.Equal(t, "load-session-1", loaded.State.SessionID)
	require.Len(t, loaded.State.Messages, 1)
	require.Equal(t, "remember this", loaded.State.Messages[0].Content)
	require.Equal(t, chat.ChatSessionIdle, loaded.State.Status, "a loaded session must reset to idle regardless of its persisted status")

	// The coordinator must also re-activate the session in memory so
	// subsequent commands (submit, close, ...) can address it by ID.
	coordinator.mu.Lock()
	reloaded, ok := coordinator.sessions["load-session-1"]
	coordinator.mu.Unlock()
	require.True(t, ok, "Load must re-populate the in-memory session map")
	require.Equal(t, "load-session-1", reloaded.state.SessionID)
}

// --- LoadChildTranscriptCommand ---

func TestHandleLoadChildTranscript_NoSessionManager_EmitsError(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.LoadChildTranscriptCommand{SessionID: "child-1"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	require.Equal(t, "child-1", event.(chat.ChatRuntimeErrorEvent).SessionID)
}

func TestHandleLoadChildTranscript_IsReadOnly(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "child-session-1", "child said hi")

	require.NoError(t, coordinator.Send(chat.LoadChildTranscriptCommand{SessionID: "child-session-1"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChildTranscriptLoadedEvent)
		return ok
	})
	transcript := event.(chat.ChildTranscriptLoadedEvent)
	require.Equal(t, "child-session-1", transcript.SessionID)
	require.Len(t, transcript.Messages, 1)
	require.Equal(t, "child said hi", transcript.Messages[0].Content)

	// Unlike LoadSessionCommand, a child transcript read must never make the
	// child the runtime's active session.
	coordinator.mu.Lock()
	_, active := coordinator.sessions["child-session-1"]
	coordinator.mu.Unlock()
	require.False(t, active, "loading a child transcript must not activate it as a live session")
}

// --- DeleteSessionCommand ---

func TestHandleDeleteSession_NoSessionManager_EmitsError(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.DeleteSessionCommand{SessionID: "missing"}))

	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
}

func TestHandleDeleteSession_UnknownSession_EmitsError(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.DeleteSessionCommand{SessionID: "does-not-exist"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	errEvent := event.(chat.ChatRuntimeErrorEvent)
	require.Equal(t, "does-not-exist", errEvent.SessionID)
	require.Contains(t, errEvent.Message, "deleting session")
}

func TestHandleDeleteSession_RemovesFromStoreAndMemory(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "delete-session-1", "delete me")

	// Reload it so it is active in the in-memory map, mirroring a user
	// resuming a session before deleting it.
	require.NoError(t, coordinator.Send(chat.LoadSessionCommand{SessionID: "delete-session-1"}))
	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionLoadedEvent)
		return ok
	})
	coordinator.mu.Lock()
	_, active := coordinator.sessions["delete-session-1"]
	coordinator.mu.Unlock()
	require.True(t, active, "precondition: session must be active before delete")

	require.NoError(t, coordinator.Send(chat.DeleteSessionCommand{SessionID: "delete-session-1"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionDeletedEvent)
		return ok
	})
	require.Equal(t, "delete-session-1", event.(chat.SessionDeletedEvent).SessionID)

	coordinator.mu.Lock()
	_, stillActive := coordinator.sessions["delete-session-1"]
	coordinator.mu.Unlock()
	require.False(t, stillActive, "delete must evict the session from the in-memory map")

	count, err := mgr.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, count, "delete must remove the row from the store")

	_, err = mgr.Load(context.Background(), "delete-session-1", nil)
	require.Error(t, err, "a deleted session must no longer be loadable")
}

// --- ExportSessionCommand ---

func TestHandleExportSession_NoSessionManager_EmitsError(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.ExportSessionCommand{SessionID: "missing", Format: "jsonl"}))

	drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
}

// TestHandleExportSession_UnknownSession_EmitsError guards against a real
// gap found while writing these tests: ExportMessages used to query the
// messages table directly by session_id with no existence check (unlike
// Load/Delete, which do check), so a bad ID matched zero rows instead of
// erroring - ExportToJSONL would happily write and atomically rename an
// empty file and the coordinator would report success. A typo'd
// `tau session export <id>` silently produced a 0-byte file instead of
// "session not found". Fixed in SQLiteStore.ExportMessages by checking
// session existence before streaming messages.
func TestHandleExportSession_UnknownSession_EmitsError(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	require.NoError(t, coordinator.Send(chat.ExportSessionCommand{
		SessionID: "does-not-exist",
		Format:    "jsonl",
		Output:    outputPath,
	}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	errEvent := event.(chat.ChatRuntimeErrorEvent)
	require.Equal(t, "does-not-exist", errEvent.SessionID)
	require.Contains(t, errEvent.Message, "exporting session")

	_, statErr := os.Stat(outputPath)
	require.True(t, os.IsNotExist(statErr), "no file should be written when the session does not exist")
}

func TestHandleExportSession_WritesJSONLFile(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "export-session-1", "export me please")

	outputPath := filepath.Join(t.TempDir(), "export-session-1.jsonl")
	require.NoError(t, coordinator.Send(chat.ExportSessionCommand{
		SessionID: "export-session-1",
		Format:    "jsonl",
		Output:    outputPath,
	}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionExportedEvent)
		return ok
	})
	exported := event.(chat.SessionExportedEvent)
	require.Equal(t, "export-session-1", exported.SessionID)
	require.Equal(t, "jsonl", exported.Format)
	require.Equal(t, outputPath, exported.Path)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "export me please")

	// The atomic-write temp file must not be left behind.
	_, statErr := os.Stat(outputPath + ".tmp")
	require.True(t, os.IsNotExist(statErr), "temp file from the atomic rename must be cleaned up")
}

func TestHandleExportSession_StreamsToStdoutWhenNoOutputPath(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "export-session-stdout", "stream me to stdout")

	realStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	require.NoError(t, coordinator.Send(chat.ExportSessionCommand{
		SessionID: "export-session-stdout",
		Format:    "jsonl",
	}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SessionExportedEvent)
		return ok
	})

	require.NoError(t, w.Close())
	os.Stdout = realStdout

	var captured strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			captured.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}

	exported := event.(chat.SessionExportedEvent)
	require.Equal(t, "export-session-stdout", exported.SessionID)
	require.Empty(t, exported.Path, "stdout export must not report a file path")
	require.Contains(t, captured.String(), "stream me to stdout")
}
