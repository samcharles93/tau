package views

import (
	"testing"
	"time"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestSessionListView_SelectsSession(t *testing.T) {
	var selectedID string
	show := gt.NewState(true)
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "session-a", ModelID: "model-a", MessageCount: 3, CreatedAt: time.Now(), TotalTokens: 120},
		{ID: "session-b", ModelID: "model-b", MessageCount: 7, CreatedAt: time.Now(), TotalTokens: 2400},
	})
	selected := gt.NewState(0)

	v := NewSessionListView(
		show,
		sessions,
		selected,
		func(s tauchat.SessionSummary) { selectedID = s.ID },
		func() {},
	)

	selected.Set(1)
	v.handleListSelect(v.buildItems()[1])

	if selectedID != "session-b" {
		t.Fatalf("selected session = %q, want session-b", selectedID)
	}
}

func TestSessionListView_LoadMoreItem(t *testing.T) {
	loaded := false
	show := gt.NewState(true)
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "session-a", ModelID: "model-a", MessageCount: 1, CreatedAt: time.Now()},
	})
	selected := gt.NewState(0)

	v := NewSessionListView(
		show,
		sessions,
		selected,
		func(s tauchat.SessionSummary) { t.Fatalf("should not select a session") },
		func() { loaded = true },
	)
	v.SetCursor("cursor-1", true)

	items := v.buildItems()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[1].ID != "_load_more" {
		t.Fatalf("last item = %q, want _load_more", items[1].ID)
	}

	v.handleListSelect(items[1])
	if !loaded {
		t.Error("load more was not triggered")
	}
}

func TestSessionListView_KeyMapEscCloses(t *testing.T) {
	show := gt.NewState(true)
	v := NewSessionListView(
		show,
		gt.NewState([]tauchat.SessionSummary{}),
		gt.NewState(0),
		nil,
		nil,
	)

	for _, b := range v.KeyMap() {
		if b.Pattern.Key == gt.KeyEscape {
			b.Handler(gt.KeyEvent{Key: gt.KeyEscape})
		}
	}

	if show.Get() {
		t.Error("session list should be closed after Esc")
	}
}
