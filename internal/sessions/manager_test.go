package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(context.Background(), dir+"/sessions.db", dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewManager(s)
}

// TestManagerListCarriesLineageAndAttribution verifies that List() maps
// parent_session_id and agent_instance_id (and the previously-dropped
// tool_calls/tool_errors) from the store row through to the chat-level
// SessionSummary — this is what the TUI/WebUI session tree is built from.
func TestManagerListCarriesLineageAndAttribution(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	root := chat.ChatSessionState{
		SessionID: "root",
		Status:    chat.ChatSessionIdle,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := m.Save(ctx, root, 0); err != nil {
		t.Fatalf("save root: %v", err)
	}

	if err := m.store.SaveAgentInstance(ctx, store.AgentInstance{
		ID:        "research#k3v9qp",
		SpecName:  "research",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save agent instance: %v", err)
	}

	child := chat.ChatSessionState{
		SessionID:       "child",
		ParentSessionID: "root",
		AgentInstanceID: "research#k3v9qp",
		Status:          chat.ChatSessionIdle,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := m.Save(ctx, child, 0); err != nil {
		t.Fatalf("save child: %v", err)
	}

	summaries, _, err := m.List(ctx, 10, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	byID := make(map[string]chat.SessionSummary, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = s
	}

	gotChild, ok := byID["child"]
	if !ok {
		t.Fatalf("child summary missing")
	}
	if gotChild.ParentSessionID != "root" {
		t.Errorf("ParentSessionID = %q, want %q", gotChild.ParentSessionID, "root")
	}
	if gotChild.AgentInstanceID != "research#k3v9qp" {
		t.Errorf("AgentInstanceID = %q, want %q", gotChild.AgentInstanceID, "research#k3v9qp")
	}

	gotRoot, ok := byID["root"]
	if !ok {
		t.Fatalf("root summary missing")
	}
	if gotRoot.ParentSessionID != "" || gotRoot.AgentInstanceID != "" {
		t.Errorf("root should have no lineage, got parent=%q agent=%q", gotRoot.ParentSessionID, gotRoot.AgentInstanceID)
	}
}
