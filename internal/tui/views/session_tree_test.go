package views

import (
	"testing"
	"time"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestSessionTree_BuildFlat(t *testing.T) {
	// All root sessions — no parent relationships.
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "a", ParentSessionID: "", Status: "idle"},
		{ID: "b", ParentSessionID: "", Status: "idle"},
		{ID: "c", ParentSessionID: "", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	items := v.visibleItems.Get()
	if len(items) != 3 {
		t.Fatalf("visible count = %d, want 3", len(items))
	}
	for _, node := range items {
		if node.Depth != 0 {
			t.Errorf("node %s depth = %d, want 0", node.ID, node.Depth)
		}
		if node.HasChildren {
			t.Errorf("node %s has children, want false", node.ID)
		}
	}
}

func TestSessionTree_BuildHierarchy(t *testing.T) {
	// Stagger created_at so sort order is deterministic.
	now := time.Now()
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "root", ParentSessionID: "", Status: "idle", CreatedAt: now},
		{ID: "child1", ParentSessionID: "root", Status: "idle", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "child2", ParentSessionID: "root", Status: "idle", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "grandchild", ParentSessionID: "child1", Status: "idle", CreatedAt: now.Add(-3 * time.Hour)},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	items := v.visibleItems.Get()
	// Only root visible initially (children collapsed by default).
	if len(items) != 1 {
		t.Fatalf("visible count = %d, want 1 (only root, children collapsed)", len(items))
	}
	if items[0].ID != "root" {
		t.Errorf("root node = %s, want 'root'", items[0].ID)
	}
}

func TestSessionTree_ExpandCollapse(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "root", ParentSessionID: "", Status: "idle"},
		{ID: "child1", ParentSessionID: "root", Status: "idle"},
		{ID: "child2", ParentSessionID: "root", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	// Expand root.
	v.expanded["root"] = true
	v.computeVisible()
	items := v.visibleItems.Get()
	if len(items) != 3 {
		t.Fatalf("after expand: visible = %d, want 3", len(items))
	}
	if items[1].Depth != 1 {
		t.Errorf("child depth = %d, want 1", items[1].Depth)
	}

	// Collapse root.
	v.expanded["root"] = false
	v.computeVisible()
	items = v.visibleItems.Get()
	if len(items) != 1 {
		t.Fatalf("after collapse: visible = %d, want 1", len(items))
	}
}

func TestSessionTree_FilterAll(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "a", ParentSessionID: "", Status: "idle"},
		{ID: "b", ParentSessionID: "", Status: "idle"},
		{ID: "c", ParentSessionID: "", Status: "closed"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)

	v.filter.Set("all")
	v.computeVisible()
	items := v.visibleItems.Get()
	if len(items) != 3 {
		t.Fatalf("filter=all: visible = %d, want 3", len(items))
	}
}

func TestSessionTree_FilterActive(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "a", ParentSessionID: "", Status: "idle"},
		{ID: "b", ParentSessionID: "", Status: "streaming"},
		{ID: "c", ParentSessionID: "", Status: "closed"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)

	v.filter.Set("active")
	v.computeVisible()
	items := v.visibleItems.Get()
	if len(items) != 2 {
		t.Fatalf("filter=active: visible = %d, want 2 (idle + streaming)", len(items))
	}
}

func TestSessionTree_FilterClosed(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "a", ParentSessionID: "", Status: "idle"},
		{ID: "b", ParentSessionID: "", Status: "closed"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)

	v.filter.Set("closed")
	v.computeVisible()
	items := v.visibleItems.Get()
	if len(items) != 1 {
		t.Fatalf("filter=closed: visible = %d, want 1", len(items))
	}
	if items[0].ID != "b" {
		t.Errorf("closed node = %s, want 'b'", items[0].ID)
	}
}

func TestSessionTree_CycleFilter(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{})
	v := NewSessionTreeView(sessions, nil, nil, nil)

	if v.filter.Get() != "all" {
		t.Fatalf("initial filter = %s, want 'all'", v.filter.Get())
	}
	v.cycleFilter()
	if v.filter.Get() != "active" {
		t.Errorf("after 1st cycle: filter = %s, want 'active'", v.filter.Get())
	}
	v.cycleFilter()
	if v.filter.Get() != "closed" {
		t.Errorf("after 2nd cycle: filter = %s, want 'closed'", v.filter.Get())
	}
	v.cycleFilter()
	if v.filter.Get() != "all" {
		t.Errorf("after 3rd cycle: filter = %s, want 'all'", v.filter.Get())
	}
}

func TestSessionTree_OrphanNodes(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "orphan", ParentSessionID: "missing-parent", Status: "idle"},
		{ID: "real-root", ParentSessionID: "", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	items := v.visibleItems.Get()
	if len(items) != 2 {
		t.Fatalf("visible = %d, want 2 (orphan + root)", len(items))
	}
}

func TestSessionTree_EmptySessions(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	items := v.visibleItems.Get()
	if len(items) != 0 {
		t.Fatalf("visible = %d, want 0", len(items))
	}
	if v.selected.Get() != 0 {
		t.Errorf("selected = %d, want 0", v.selected.Get())
	}
}

func TestSessionTree_KeyUpDownNavigation(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "a", ParentSessionID: "", Status: "idle"},
		{ID: "b", ParentSessionID: "", Status: "idle"},
		{ID: "c", ParentSessionID: "", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	km := v.KeyMap()
	down := findBinding(km, gt.KeyDown)
	up := findBinding(km, gt.KeyUp)
	if down == nil || up == nil {
		t.Fatal("missing up/down key bindings")
	}

	// Down: 0 → 1 → 2 → 0.
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if v.selected.Get() != 1 {
		t.Errorf("after 1st down: selected = %d, want 1", v.selected.Get())
	}
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if v.selected.Get() != 2 {
		t.Errorf("after 2nd down: selected = %d, want 2", v.selected.Get())
	}
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if v.selected.Get() != 0 {
		t.Errorf("after 3rd down (wrap): selected = %d, want 0", v.selected.Get())
	}

	// Up: 0 → 2 → 1 → 0.
	up.Handler(gt.KeyEvent{Key: gt.KeyUp})
	if v.selected.Get() != 2 {
		t.Errorf("after 1st up (wrap): selected = %d, want 2", v.selected.Get())
	}
	up.Handler(gt.KeyEvent{Key: gt.KeyUp})
	if v.selected.Get() != 1 {
		t.Errorf("after 2nd up: selected = %d, want 1", v.selected.Get())
	}
	up.Handler(gt.KeyEvent{Key: gt.KeyUp})
	if v.selected.Get() != 0 {
		t.Errorf("after 3rd up: selected = %d, want 0", v.selected.Get())
	}
}

func TestSessionTree_EnterExpandAndSelect(t *testing.T) {
	var selected tauchat.SessionSummary
	now := time.Now()
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "parent", ParentSessionID: "", Status: "idle", CreatedAt: now},
		{ID: "child", ParentSessionID: "parent", Status: "idle", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "leaf", ParentSessionID: "", Status: "idle", CreatedAt: now.Add(-2 * time.Hour)},
	})
	v := NewSessionTreeView(sessions, func(s tauchat.SessionSummary) {
		selected = s
	}, nil, nil)
	v.computeVisible()

	km := v.KeyMap()
	enter := findBinding(km, gt.KeyEnter)
	if enter == nil {
		t.Fatal("missing Enter binding")
	}

	items := v.visibleItems.Get()
	// "parent" should be first (newest), then "leaf".
	if items[0].ID != "parent" {
		t.Fatalf("first visible = %s, want 'parent'", items[0].ID)
	}

	// Select "parent" — it has children, should expand.
	v.selected.Set(0)
	enter.Handler(gt.KeyEvent{Key: gt.KeyEnter})
	if !v.expanded["parent"] {
		t.Error("parent should be expanded after Enter")
	}
	v.computeVisible()
	items = v.visibleItems.Get()
	// After expand: parent, child (depth 1), leaf (depth 0).
	if len(items) != 3 {
		t.Fatalf("visible after expand = %d, want 3", len(items))
	}

	// Find "leaf" in the visible list and select it.
	leafIdx := -1
	for i, n := range items {
		if n.ID == "leaf" {
			leafIdx = i
			break
		}
	}
	if leafIdx < 0 {
		t.Fatal("leaf not found in visible list")
	}
	v.selected.Set(leafIdx)
	enter.Handler(gt.KeyEvent{Key: gt.KeyEnter})
	if selected.ID != "leaf" {
		t.Errorf("selected = %s, want 'leaf'", selected.ID)
	}
}

func TestSessionTree_EnterOnExpandedCollapses(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "parent", ParentSessionID: "", Status: "idle"},
		{ID: "child", ParentSessionID: "parent", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.expanded["parent"] = true
	v.computeVisible()

	items := v.visibleItems.Get()
	if len(items) != 2 {
		t.Fatalf("visible after expand = %d, want 2", len(items))
	}

	km := v.KeyMap()
	enter := findBinding(km, gt.KeyEnter)
	v.selected.Set(0) // parent
	enter.Handler(gt.KeyEvent{Key: gt.KeyEnter})

	if v.expanded["parent"] {
		t.Error("parent should be collapsed after second Enter")
	}
	v.computeVisible()
	items = v.visibleItems.Get()
	if len(items) != 1 {
		t.Fatalf("visible after collapse = %d, want 1", len(items))
	}
}

func TestSessionTree_LeftRightNavigation(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "parent", ParentSessionID: "", Status: "idle"},
		{ID: "child", ParentSessionID: "parent", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	km := v.KeyMap()
	right := findBinding(km, gt.KeyRight)
	left := findBinding(km, gt.KeyLeft)
	if right == nil || left == nil {
		t.Fatal("missing left/right bindings")
	}

	// Right on collapsed parent → expands.
	v.selected.Set(0)
	right.Handler(gt.KeyEvent{Key: gt.KeyRight})
	if !v.expanded["parent"] {
		t.Error("parent should expand on Right")
	}
	v.computeVisible()

	// Left on expanded parent → collapses.
	left.Handler(gt.KeyEvent{Key: gt.KeyLeft})
	if v.expanded["parent"] {
		t.Error("parent should collapse on Left")
	}
	v.computeVisible()

	// Left on collapsed parent → no-op.
	left.Handler(gt.KeyEvent{Key: gt.KeyLeft})
	if v.expanded["parent"] {
		t.Error("parent should stay collapsed on Left when already collapsed")
	}
}

func TestSessionTree_FilterKey(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{})
	v := NewSessionTreeView(sessions, nil, nil, nil)

	km := v.KeyMap()
	f := findBindingRune(km, 'f')
	if f == nil {
		t.Fatal("missing 'f' binding")
	}

	if v.filter.Get() != "all" {
		t.Fatalf("initial = %s", v.filter.Get())
	}
	f.Handler(gt.KeyEvent{Key: gt.KeyRune, Rune: 'f'})
	if v.filter.Get() != "active" {
		t.Errorf("after f: filter = %s, want 'active'", v.filter.Get())
	}
}

func TestSessionTree_DetailPopup(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "leaf", ParentSessionID: "", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	km := v.KeyMap()
	right := findBinding(km, gt.KeyRight)

	// Right on a leaf opens detail.
	v.selected.Set(0)
	right.Handler(gt.KeyEvent{Key: gt.KeyRight})
	if !v.showDetail.Get() {
		t.Error("detail should open on Right for leaf node")
	}
	if v.detailInfo.Get().ID != "leaf" {
		t.Errorf("detail ID = %s, want 'leaf'", v.detailInfo.Get().ID)
	}

	// Esc closes detail.
	esc := findBinding(km, gt.KeyEscape)
	if esc == nil {
		t.Fatal("missing Esc binding")
	}
	esc.Handler(gt.KeyEvent{Key: gt.KeyEscape})
	if v.showDetail.Get() {
		t.Error("detail should close on Esc")
	}
}

func TestSessionTree_StatusIcons(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status    string
		wantIcon  string
		wantLabel string
	}{
		"idle":       {status: "idle", wantIcon: "○", wantLabel: "idle"},
		"streaming":  {status: "streaming", wantIcon: "●", wantLabel: "streaming"},
		"cancelling": {status: "cancelling", wantIcon: "●", wantLabel: "cancelling"},
		"closed":     {status: "closed", wantIcon: "✓", wantLabel: "closed"},
		"unknown":    {status: "unknown", wantIcon: "○", wantLabel: "unknown"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			icon, label := statusDisplay(tc.status)
			if icon != tc.wantIcon {
				t.Errorf("icon = %q, want %q", icon, tc.wantIcon)
			}
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
		})
	}
}

func TestSessionTree_EdgeCaseSingleNode(t *testing.T) {
	sessions := gt.NewState([]tauchat.SessionSummary{
		{ID: "only", ParentSessionID: "", Status: "idle"},
	})
	v := NewSessionTreeView(sessions, nil, nil, nil)
	v.computeVisible()

	items := v.visibleItems.Get()
	if len(items) != 1 {
		t.Fatalf("visible = %d, want 1", len(items))
	}
	// Navigation should wrap to self.
	km := v.KeyMap()
	down := findBinding(km, gt.KeyDown)
	down.Handler(gt.KeyEvent{Key: gt.KeyDown})
	if v.selected.Get() != 0 {
		t.Errorf("selected = %d, want 0 (wrapped to self)", v.selected.Get())
	}
	up := findBinding(km, gt.KeyUp)
	up.Handler(gt.KeyEvent{Key: gt.KeyUp})
	if v.selected.Get() != 0 {
		t.Errorf("selected = %d, want 0 (wrapped to self)", v.selected.Get())
	}
}

// --- helpers ---

func findBinding(km gt.KeyMap, key gt.Key) *gt.KeyBinding {
	for i := range km {
		if km[i].Pattern.Key == key {
			return &km[i]
		}
	}
	return nil
}

func findBindingRune(km gt.KeyMap, r rune) *gt.KeyBinding {
	for i := range km {
		if km[i].Pattern.Rune == r {
			return &km[i]
		}
	}
	return nil
}
