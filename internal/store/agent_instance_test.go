package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_SaveAndGetAgentInstance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inst := store.AgentInstance{
		ID:               "research#aaaaaa",
		SpecName:         "research",
		SpecScope:        "builtin",
		SpecHash:         "abc123",
		SpecSnapshot:     `{"name":"research"}`,
		ResolvedProvider: "openai",
		ResolvedModel:    "gpt-5",
		Depth:            1,
		StartedAt:        time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, s.SaveAgentInstance(ctx, inst))

	got, err := s.GetAgentInstance(ctx, inst.ID)
	require.NoError(t, err)
	require.Equal(t, inst.SpecName, got.SpecName)
	require.Equal(t, inst.ResolvedModel, got.ResolvedModel)
	require.True(t, got.EndedAt.IsZero())
}

// TestSQLiteStore_SaveAgentInstance_DuplicateIDIsUniqueConstraintError
// verifies that an instance-ID collision on SaveAgentInstance is classified
// by IsUniqueConstraintError — this is what callers (saveInstanceWithIDRetry
// in internal/agent/tools, Instantiate in internal/agent) rely on to decide
// "retry with a fresh ID" vs. "genuine failure" (G3/G10: instance ID
// collision retry).
func TestSQLiteStore_SaveAgentInstance_DuplicateIDIsUniqueConstraintError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inst := store.AgentInstance{ID: "research#aaaaaa", SpecName: "research", SpecHash: "h", StartedAt: time.Now()}
	require.NoError(t, s.SaveAgentInstance(ctx, inst))

	err := s.SaveAgentInstance(ctx, inst)
	require.Error(t, err)
	require.True(t, store.IsUniqueConstraintError(err), "expected IsUniqueConstraintError, got %v", err)

	// A generic not-found error must not be misclassified as a collision.
	_, notFoundErr := s.GetAgentInstance(ctx, "does-not-exist")
	require.False(t, store.IsUniqueConstraintError(notFoundErr))
}

// TestSQLiteStore_ResumeSession_SucceedsWhenPriorInstanceEnded verifies the
// happy path: a session whose owning instance has ended can be resumed —
// the new instance is inserted and the session's agent_instance_id is
// repointed, atomically.
func TestSQLiteStore_ResumeSession_SucceedsWhenPriorInstanceEnded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	orig := store.AgentInstance{
		ID:        "research#aaaaaa",
		SpecName:  "research",
		SpecHash:  "h1",
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, s.SaveAgentInstance(ctx, orig))
	require.NoError(t, s.CloseAgentInstance(ctx, orig.ID, "completed", "", ""))

	require.NoError(t, s.Save(ctx, chat.ChatSessionState{
		SessionID:       "sess-1",
		Status:          chat.ChatSessionIdle,
		AgentInstanceID: orig.ID,
	}, 0))

	require.NoError(t, s.SaveAgentInstance(ctx, store.AgentInstance{
		ID:        "tau#root000",
		SpecName:  "tau",
		SpecHash:  "h0",
		StartedAt: time.Now().UTC(),
	}))

	newInst := store.AgentInstance{
		ID:               "research#bbbbbb",
		SpecName:         "research",
		SpecHash:         "h1",
		ParentInstanceID: "tau#root000",
		StartedAt:        time.Now().UTC(),
	}
	err := s.ResumeSession(ctx, "sess-1", newInst)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "sess-1")
	require.NoError(t, err)
	require.Equal(t, newInst.ID, loaded.AgentInstanceID)

	gotNew, err := s.GetAgentInstance(ctx, newInst.ID)
	require.NoError(t, err)
	require.Equal(t, "research", gotNew.SpecName)
}

// TestSQLiteStore_ResumeSession_RejectsActiveOwner verifies that a session
// whose current owning instance has NOT ended cannot be resumed — this is
// the ownership-exclusivity half of G2 (docs/specs/agents/
// 04-storage-and-sessions.md, Active session ownership).
func TestSQLiteStore_ResumeSession_RejectsActiveOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	orig := store.AgentInstance{
		ID:        "research#aaaaaa",
		SpecName:  "research",
		SpecHash:  "h1",
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, s.SaveAgentInstance(ctx, orig))
	// Deliberately not closed — the instance is still "active" (ended_at IS NULL).

	require.NoError(t, s.Save(ctx, chat.ChatSessionState{
		SessionID:       "sess-1",
		Status:          chat.ChatSessionIdle,
		AgentInstanceID: orig.ID,
	}, 0))

	newInst := store.AgentInstance{
		ID:        "research#bbbbbb",
		SpecName:  "research",
		SpecHash:  "h1",
		StartedAt: time.Now().UTC(),
	}
	err := s.ResumeSession(ctx, "sess-1", newInst)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrSessionActive), "expected ErrSessionActive, got %v", err)

	// No new instance row should have been created.
	_, getErr := s.GetAgentInstance(ctx, newInst.ID)
	require.Error(t, getErr)
}

// TestSQLiteStore_ResumeSession_RejectsMissingSession verifies resuming a
// nonexistent session id fails cleanly rather than silently creating one.
func TestSQLiteStore_ResumeSession_RejectsMissingSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	newInst := store.AgentInstance{
		ID:        "research#bbbbbb",
		SpecName:  "research",
		SpecHash:  "h1",
		StartedAt: time.Now().UTC(),
	}
	err := s.ResumeSession(ctx, "does-not-exist", newInst)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrSessionNotFound), "expected ErrSessionNotFound, got %v", err)
}

// TestSQLiteStore_ResumeSession_ConcurrentRaceYieldsOneOwner verifies that
// two goroutines racing to resume the same ended session produce exactly
// one winner — the SQLite transaction serialises the second attempt onto a
// session whose agent_instance_id already points to a non-ended instance.
func TestSQLiteStore_ResumeSession_ConcurrentRaceYieldsOneOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	orig := store.AgentInstance{
		ID:        "research#aaaaaa",
		SpecName:  "research",
		SpecHash:  "h1",
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, s.SaveAgentInstance(ctx, orig))
	require.NoError(t, s.CloseAgentInstance(ctx, orig.ID, "completed", "", ""))
	require.NoError(t, s.Save(ctx, chat.ChatSessionState{
		SessionID:       "sess-1",
		Status:          chat.ChatSessionIdle,
		AgentInstanceID: orig.ID,
	}, 0))

	racerA := store.AgentInstance{ID: "research#111111", SpecName: "research", SpecHash: "h1", StartedAt: time.Now().UTC()}
	racerB := store.AgentInstance{ID: "research#222222", SpecName: "research", SpecHash: "h1", StartedAt: time.Now().UTC()}

	results := make(chan error, 2)
	go func() { results <- s.ResumeSession(ctx, "sess-1", racerA) }()
	go func() { results <- s.ResumeSession(ctx, "sess-1", racerB) }()

	errA := <-results
	errB := <-results

	successes := 0
	for _, err := range []error{errA, errB} {
		if err == nil {
			successes++
		} else {
			require.True(t, errors.Is(err, store.ErrSessionActive), "loser should fail with ErrSessionActive, got %v", err)
		}
	}
	require.Equal(t, 1, successes, "exactly one racer should win")
}
