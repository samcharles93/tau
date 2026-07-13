package tui2

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCtrlOFetchesWhenCacheEmpty(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	cmd := m.dispatchKey(key('o', tea.ModCtrl))
	drainCmd(cmd)

	if m.sessionTreeOverlay == nil {
		t.Fatal("expected Ctrl+O to open m.sessionTreeOverlay")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.ListSessionsCommand)
	if !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
	if !sent.Silent {
		t.Fatal("expected the prefetch to be silent")
	}
	if !m.sessionsFetchInFlight {
		t.Fatal("expected sessionsFetchInFlight=true while the fetch is outstanding")
	}
}

func TestCtrlODoesNotRefetchWhenCachePopulated(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.sessionSummaries = []tauchat.SessionSummary{{ID: "sess-1", ModelID: "gpt-4"}}

	m.dispatchKey(key('o', tea.ModCtrl))

	if m.sessionTreeOverlay == nil {
		t.Fatal("expected Ctrl+O to open m.sessionTreeOverlay")
	}
	if len(rt.sent) != 0 {
		t.Fatalf("expected no fetch when the cache is already populated, got %d sends", len(rt.sent))
	}
}

func TestSessionTreeRowsNestChildUnderParent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "sess-1"
	m.sessionSummaries = []tauchat.SessionSummary{
		{ID: "sess-1", ModelID: "gpt-4", UpdatedAt: time.Now()},
		{ID: "sess-2", ModelID: "gpt-4", ParentSessionID: "sess-1", UpdatedAt: time.Now()},
	}

	rows := m.sessionTreeRows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].id != "sess-1" || rows[0].depth != 0 {
		t.Fatalf("rows[0] = %+v, want root sess-1 at depth 0", rows[0])
	}
	if !rows[0].isActive {
		t.Fatal("expected sess-1 to be marked isActive (matches m.sessionID)")
	}
	if rows[1].id != "sess-2" || rows[1].depth != 1 {
		t.Fatalf("rows[1] = %+v, want child sess-2 at depth 1", rows[1])
	}
}

func TestSessionTreeEnterLoadsSelectedSession(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.sessionSummaries = []tauchat.SessionSummary{
		{ID: "sess-1", ModelID: "gpt-4"},
		{ID: "sess-2", ModelID: "gpt-4"},
	}
	m.dispatchKey(key('o', tea.ModCtrl))
	m.sessionTreeOverlay.selected = 1

	cmd := m.dispatchKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.sessionTreeOverlay != nil {
		t.Fatal("expected Enter to close the session navigator")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.LoadSessionCommand)
	if !ok {
		t.Fatalf("expected LoadSessionCommand, got %T", rt.sent[0])
	}
	if sent.SessionID != "sess-2" {
		t.Fatalf("SessionID = %q, want %q", sent.SessionID, "sess-2")
	}
}

func TestSessionTreeEscCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionSummaries = []tauchat.SessionSummary{{ID: "sess-1", ModelID: "gpt-4"}}
	m.dispatchKey(key('o', tea.ModCtrl))
	if m.sessionTreeOverlay == nil {
		t.Fatal("setup: expected session navigator to be open")
	}

	m.dispatchKey(key(tea.KeyEscape, 0))

	if m.sessionTreeOverlay != nil {
		t.Fatal("expected Esc to close the session navigator")
	}
}
