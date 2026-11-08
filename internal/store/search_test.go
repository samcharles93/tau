package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
)

// saveSearchFixture stores a session whose messages use distinctive terms.
func saveSearchFixture(t *testing.T, s *store.SQLiteStore, id, userText, assistantText string) {
	t.Helper()
	state := chatSessionFixture(id, "model", "prov")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: userText},
		{Role: chat.ChatRoleAssistant, Content: assistantText},
	}
	require.NoError(t, s.Save(context.Background(), state, time.Second))
}

func TestSQLiteStore_Search(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saveSearchFixture(t, s, "sess-a", "how do I deploy the kubernetes cluster", "use helm upgrade")
	saveSearchFixture(t, s, "sess-b", "kubernetes networking question", "check the ingress controller")
	saveSearchFixture(t, s, "sess-c", "unrelated lunch plans", "tacos sound great")

	hits, err := s.Search(ctx, "kubernetes", 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 2, "porter stemming + content column: both kubernetes messages match")
	for _, h := range hits {
		assert.Contains(t, h.Snippet, "**kubernetes**")
		assert.NotEmpty(t, h.SessionID)
		assert.Equal(t, "user", h.Role)
		assert.False(t, h.CreatedAt.IsZero())
	}
	sessionIDs := []string{hits[0].SessionID, hits[1].SessionID}
	assert.ElementsMatch(t, []string{"sess-a", "sess-b"}, sessionIDs)

	// Multi-term queries AND the terms.
	hits, err = s.Search(ctx, "kubernetes deploy", 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "sess-a", hits[0].SessionID)
	assert.Contains(t, hits[0].Snippet, "**deploy**")

	// Porter stemming: "run" matches "running".
	saveSearchFixture(t, s, "sess-d", "the job is running now", "ok")
	hits, err = s.Search(ctx, "run", 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "sess-d", hits[0].SessionID)

	// Empty / whitespace queries return nothing, no error.
	hits, err = s.Search(ctx, "   ", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)

	// FTS5 syntax characters in user input are quoted, never a syntax error:
	// an operator-only query matches nothing but must not error, and embedded
	// quotes are escaped so the term still matches literally.
	hits, err = s.Search(ctx, `AND (OR NOT`, 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
	hits, err = s.Search(ctx, `use "helm"`, 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "sess-a", hits[0].SessionID)
	assert.Equal(t, "assistant", hits[0].Role)

	// No matches.
	hits, err = s.Search(ctx, "zzzznotfound", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestSQLiteStore_SearchPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2", "p3", "p4", "p5"} {
		saveSearchFixture(t, s, id, "pagination target word", "reply")
	}

	page1, err := s.Search(ctx, "pagination", 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)

	page2, err := s.Search(ctx, "pagination", 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)

	page3, err := s.Search(ctx, "pagination", 2, 4)
	require.NoError(t, err)
	require.Len(t, page3, 1)

	// Pages are disjoint and cover all five sessions.
	seen := map[string]bool{}
	for _, h := range append(append(page1, page2...), page3...) {
		assert.False(t, seen[h.SessionID], "duplicate hit for %s", h.SessionID)
		seen[h.SessionID] = true
	}
	assert.Len(t, seen, 5)
}

func TestSQLiteStore_SearchIndexStaysInSync(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saveSearchFixture(t, s, "sync-sess", "original zebra content", "reply")
	hits, err := s.Search(ctx, "zebra", 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1)

	// Re-saving replaces messages (DELETE + INSERT): the old term must
	// disappear and the new one appear exactly once - no duplicate index
	// rows from the replace.
	saveSearchFixture(t, s, "sync-sess", "replaced giraffe content", "reply")
	hits, err = s.Search(ctx, "zebra", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits, "stale term must be gone after re-save")
	hits, err = s.Search(ctx, "giraffe", 0, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1, "new term indexed exactly once")

	// Deleting the session removes its rows from the index.
	require.NoError(t, s.Delete(ctx, "sync-sess"))
	hits, err = s.Search(ctx, "giraffe", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestMigrateV11_BackfillsFTSIndex(t *testing.T) {
	// Simulate a pre-v11 database: schema_version at 10 with a messages
	// table holding rows the FTS index has never seen.
	ctx := context.Background()
	dbPath := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')));
		INSERT INTO schema_version (version) VALUES (10);
		CREATE TABLE messages (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    session_id TEXT NOT NULL,
		    seq INTEGER NOT NULL,
		    role TEXT NOT NULL,
		    content TEXT NOT NULL DEFAULT '',
		    reasoning_content TEXT NOT NULL DEFAULT '',
		    tool_calls TEXT NOT NULL DEFAULT '',
		    tool_call_id TEXT NOT NULL DEFAULT '',
		    created_at TEXT NOT NULL,
		    client_id TEXT NOT NULL DEFAULT '',
		    tool_result TEXT NOT NULL DEFAULT '',
		    attachments TEXT NOT NULL DEFAULT '',
		    UNIQUE(session_id, seq)
		);
		INSERT INTO messages (session_id, seq, role, content, created_at) VALUES
		    ('old-sess', 0, 'user', 'ancient buffalo roamed here', '2026-01-01T00:00:00Z'),
		    ('old-sess', 1, 'assistant', 'indeed', '2026-01-01T00:00:01Z');
	`)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(context.Background(), db))

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages_fts`).Scan(&n))
	assert.Equal(t, 2, n, "migration v11 backfills pre-existing messages")

	var snip string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT snippet(messages_fts, 0, '**', '**', '…', 8) FROM messages_fts WHERE messages_fts MATCH 'content:"buffalo"'`,
	).Scan(&snip))
	assert.Contains(t, snip, "**buffalo**")

	// Triggers keep the index in sync after the migration.
	_, err = db.ExecContext(ctx, `INSERT INTO messages (session_id, seq, role, content, created_at) VALUES ('old-sess', 2, 'user', 'fresh yak content', '2026-01-02T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT snippet(messages_fts, 0, '**', '**', '…', 8) FROM messages_fts WHERE messages_fts MATCH 'content:"yak"'`,
	).Scan(&snip))
	assert.Contains(t, snip, "**yak**")
}

func TestFtsMatchQuery(t *testing.T) {
	// ftsMatchQuery is unexported; exercise it through Search behavior in the
	// tests above. Here we only assert the store-level contract indirectly:
	// a query that is pure FTS5 operators must not error.
	s := newTestStore(t)
	saveSearchFixture(t, s, "q-sess", "hello world", "hi")
	hits, err := s.Search(context.Background(), `NEAR ( " OR *`, 0, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}
