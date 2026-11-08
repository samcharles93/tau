package tui2

import (
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// --- tau-px4 regression: "/session info <id>" must refresh the session list
// before looking the ID up, instead of answering from the stale
// m.sessionSummaries cache (which reported valid sessions as "not found"
// in a fresh or stale TUI process). -----------------------------------------

func scrollbackText(m *model) string {
	return strings.Join(m.renderedLines, "\n")
}

func TestSessionInfoRefreshesBeforeLookupOnEmptyCache(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	// Fresh TUI process: the cache starts empty.

	drainCmd(m.cmdSession("info sess-9"))

	// The old code answered "session not found" immediately from the empty
	// cache; now it must fire a silent refresh and wait for the event.
	if m.notification != "" {
		t.Fatalf("expected no immediate notification, got %q", m.notification)
	}
	if m.pendingSessionInfo != "sess-9" {
		t.Fatalf("pendingSessionInfo = %q, want %q", m.pendingSessionInfo, "sess-9")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	list, ok := rt.sent[0].(tauchat.ListSessionsCommand)
	if !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
	if !list.Silent {
		t.Error("expected the refresh to be Silent so it doesn't print a list to scrollback")
	}

	// The refresh delivers the session: its info prints to scrollback.
	m.handleChatEvent(tauchat.SessionsListedEvent{
		Silent: true,
		Sessions: []tauchat.SessionSummary{{
			ID: "sess-9", ModelID: "gpt-4", MessageCount: 7,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	})

	if m.pendingSessionInfo != "" {
		t.Fatalf("expected pendingSessionInfo cleared after resolution, got %q", m.pendingSessionInfo)
	}
	if got := scrollbackText(m); !strings.Contains(got, "Session sess-9") || !strings.Contains(got, "Messages: 7") {
		t.Fatalf("expected fresh session info in scrollback, got:\n%s", got)
	}
	if m.notification != "" {
		t.Fatalf("expected no not-found notification, got %q", m.notification)
	}
}

func TestSessionInfoIgnoresStaleCacheEntry(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	// Stale cache: claims sess-9 has 1 message; the fresh list says 42.
	m.sessionSummaries = []tauchat.SessionSummary{{ID: "sess-9", ModelID: "gpt-4", MessageCount: 1}}

	drainCmd(m.cmdSession("info sess-9"))

	if len(rt.sent) != 1 {
		t.Fatalf("expected a refresh even with a populated cache, got %d commands", len(rt.sent))
	}
	m.handleChatEvent(tauchat.SessionsListedEvent{
		Silent: true,
		Sessions: []tauchat.SessionSummary{{
			ID: "sess-9", ModelID: "gpt-4", MessageCount: 42,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	})

	got := scrollbackText(m)
	if !strings.Contains(got, "Messages: 42") {
		t.Fatalf("expected info from the fresh list (Messages: 42), got:\n%s", got)
	}
	if strings.Contains(got, "Messages: 1") {
		t.Fatalf("stale cache data leaked into the info output:\n%s", got)
	}
}

func TestSessionInfoPagesThroughCursor(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("info sess-deep"))

	// Page 1 lacks the ID but has a next page: a continuation fetch with the
	// cursor must fire, and the lookup stays pending.
	cmd := m.handleChatEvent(tauchat.SessionsListedEvent{
		Silent:     true,
		Sessions:   []tauchat.SessionSummary{{ID: "sess-other", ModelID: "gpt-4"}},
		NextCursor: "2024-01-01T00:00:00Z|sess-other",
	})
	drainCmd(cmd)

	if len(rt.sent) != 2 {
		t.Fatalf("expected a second ListSessionsCommand with the cursor, got %d sends", len(rt.sent))
	}
	page2, ok := rt.sent[1].(tauchat.ListSessionsCommand)
	if !ok || !page2.Silent || page2.Cursor != "2024-01-01T00:00:00Z|sess-other" {
		t.Fatalf("expected a silent continuation carrying the cursor, got %#v", rt.sent[1])
	}
	if m.pendingSessionInfo != "sess-deep" {
		t.Fatalf("expected the lookup to stay pending across pages, got %q", m.pendingSessionInfo)
	}
	if len(m.sessionSummaries) != 1 || m.sessionSummaries[0].ID != "sess-other" {
		t.Fatalf("expected page 1 in the cache, got %+v", m.sessionSummaries)
	}

	// Page 2 contains the ID: info prints and both pages accumulate.
	m.handleChatEvent(tauchat.SessionsListedEvent{
		Silent: true,
		Sessions: []tauchat.SessionSummary{{
			ID: "sess-deep", ModelID: "gpt-4", MessageCount: 3,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	})

	if got := scrollbackText(m); !strings.Contains(got, "Session sess-deep") {
		t.Fatalf("expected session info after the second page, got:\n%s", got)
	}
	if len(m.sessionSummaries) != 2 {
		t.Fatalf("expected both pages accumulated in the cache, got %d entries", len(m.sessionSummaries))
	}
	if m.pendingSessionInfo != "" {
		t.Fatalf("expected pendingSessionInfo cleared, got %q", m.pendingSessionInfo)
	}
}

func TestSessionInfoNotFoundAfterFullRefresh(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("info nope"))
	m.handleChatEvent(tauchat.SessionsListedEvent{
		Silent:   true,
		Sessions: []tauchat.SessionSummary{{ID: "sess-other"}},
	})

	if m.notification != "session not found: nope" {
		t.Fatalf("notification = %q, want %q", m.notification, "session not found: nope")
	}
	if m.pendingSessionInfo != "" {
		t.Fatalf("expected pendingSessionInfo cleared, got %q", m.pendingSessionInfo)
	}
	if strings.Contains(scrollbackText(m), "Session nope") {
		t.Fatal("did not expect info output for a missing session")
	}
}

func TestSessionInfoUsageWithoutID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("info"))

	if m.notification != "usage: /session info <id>" {
		t.Fatalf("notification = %q, want usage message", m.notification)
	}
	if len(rt.sent) != 0 {
		t.Fatalf("expected no command for a usage error, got %d sends", len(rt.sent))
	}
}

func TestSessionInfoAbandonedOnRuntimeError(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.cmdSession("info sess-9"))
	if m.pendingSessionInfo != "sess-9" || !m.sessionsFetchInFlight {
		t.Fatal("setup: expected a pending lookup with a fetch in flight")
	}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "listing sessions: db locked"})

	if m.pendingSessionInfo != "" {
		t.Fatalf("expected pendingSessionInfo cleared on fetch failure, got %q", m.pendingSessionInfo)
	}
	if m.sessionsFetchInFlight {
		t.Fatal("expected sessionsFetchInFlight cleared on fetch failure")
	}
}

func TestSessionInfoAbandonedOnSendFailure(t *testing.T) {
	rt := &fakeRuntime{err: errIntentional}
	m := newTestModel(rt, nil)

	msgs := drainCmd(m.cmdSession("info sess-9"))
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from the send, got %d", len(msgs))
	}
	m.Update(msgs[0]) // deliver the sendResultMsg carrying the error

	if m.pendingSessionInfo != "" {
		t.Fatalf("expected pendingSessionInfo cleared on send failure, got %q", m.pendingSessionInfo)
	}
	if m.sessionsFetchInFlight {
		t.Fatal("expected sessionsFetchInFlight cleared on send failure")
	}
}
