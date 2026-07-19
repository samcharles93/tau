package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestConfirmPromptEnterUsesHighlightedOption(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	evt := tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm", Title: "Delete?", Message: "sure?",
	}
	drainCmd(m.handleChatEvent(evt))

	if m.activePrompt == nil {
		t.Fatal("expected activePrompt to be set")
	}
	if !m.activePrompt.confirmYes {
		t.Fatal("expected the default highlighted option to be Yes")
	}

	// Toggle to "No" before submitting.
	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activePrompt.confirmYes {
		t.Fatal("expected tab to toggle the highlighted option to No")
	}

	drainCmd(m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(rt.sent) != 1 {
		t.Fatalf("expected exactly 1 command sent, got %d", len(rt.sent))
	}
	resp, ok := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !ok {
		t.Fatalf("expected RespondInteractivePromptCommand, got %T", rt.sent[0])
	}
	if resp.Confirmed {
		t.Error("bare Enter must resolve to the highlighted option (No), not silently default to Confirmed=true")
	}
	if !resp.Canceled {
		t.Error("expected Canceled=true when the highlighted/submitted option is No")
	}
}

func TestConfirmPromptYKeyConfirmsRegardlessOfHighlight(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	evt := tauchat.InteractivePromptRequestedEvent{RequestID: "req-2", Kind: "confirm"}
	drainCmd(m.handleChatEvent(evt))

	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab}) // toggle to No
	drainCmd(m.handlePromptKey(tea.KeyPressMsg{Code: 'y', Text: "y"}))

	resp, ok := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !ok {
		t.Fatalf("expected RespondInteractivePromptCommand, got %T", rt.sent[0])
	}
	if !resp.Confirmed {
		t.Error("explicit 'y' must confirm regardless of the highlighted option")
	}
}

func TestHandleChatEventInteractivePromptQueued(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "input", Title: "API Key", Message: "enter key",
	})

	if m.activePrompt == nil {
		t.Fatal("expected activePrompt to be set")
	}
	if m.activePrompt.requestID != "req-1" {
		t.Fatalf("activePrompt.requestID = %q, want %q", m.activePrompt.requestID, "req-1")
	}
}

func TestHandleChatEventInteractivePromptQueueSecond(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1"})
	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{RequestID: "req-2"})

	// First should be active, second queued.
	if m.activePrompt.requestID != "req-1" {
		t.Fatalf("active prompt = %q, want %q", m.activePrompt.requestID, "req-1")
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "req-2" {
		t.Fatalf("expected [req-2] in prompt queue, got %+v", m.promptQueue)
	}
}

func TestEnqueuePromptQueuesWhenActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "first"})

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "second"})

	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "second" {
		t.Fatalf("expected [second] queued, got %+v", m.promptQueue)
	}
}

func TestPresentNextQueuedPromptNoQueue(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "current"})

	m.presentNextQueuedPrompt()

	if m.activePrompt != nil {
		t.Fatal("activePrompt should be nil when queue is empty")
	}
}

func TestPresentNextQueuedPromptDrains(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "current"})
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "next"})
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "last"})

	m.presentNextQueuedPrompt()

	if m.activePrompt == nil || m.activePrompt.requestID != "next" {
		t.Fatalf("activePrompt.requestID = %v, want 'next'", m.activePrompt.requestID)
	}
	if len(m.promptQueue) != 1 || m.promptQueue[0].requestID != "last" {
		t.Fatalf("promptQueue should have [last], got %+v", m.promptQueue)
	}
	if !m.activePrompt.confirmYes {
		t.Fatal("confirmYes should reset to true for new prompt")
	}
}

func TestResolvePromptCancel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-cancel"})

	drainCmd(m.resolvePromptCancel())

	if m.activePrompt != nil {
		t.Fatal("activePrompt should be nil after cancel")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Canceled {
		t.Fatal("expected Canceled=true")
	}
}

func TestResolvePromptCancelWithNoActivePrompt(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.resolvePromptCancel()
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

func TestResolvePromptWithNoActivePrompt(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.resolvePrompt()
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

func TestActivePanelEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	p := m.activePanel()
	if p != nil {
		t.Fatal("expected nil when no panels")
	}
}

func TestActivePanelReturnsFirst(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.panels["a"] = pluginPanel{id: "a", title: "Panel A"}
	m.panels["b"] = pluginPanel{id: "b", title: "Panel B"}

	p := m.activePanel()
	if p == nil {
		t.Fatal("expected a panel")
	}
}

func TestHandlePromptKeyNoActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.handlePromptKey(key(tea.KeyEnter, 0))
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

func TestHandlePromptKeyEsc(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})

	drainCmd(m.handlePromptKey(key(tea.KeyEscape, 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Canceled {
		t.Fatal("expected Canceled=true when pressing Esc")
	}
}

func TestHandlePromptKeyEnterOnConfirmWithInputKind(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "question", Message: "enter value",
	})
	m.activePrompt.field.SetValue("my value")

	drainCmd(m.handlePromptKey(key(tea.KeyEnter, 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if cmd.Response != "my value" {
		t.Fatalf("Response = %q, want %q", cmd.Response, "my value")
	}
}

func TestHandlePromptKeyYNonConfirmInserts(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})

	m.handlePromptKey(key('y', 0))
	if got := m.activePrompt.field.Value(); got != "y" {
		t.Fatalf("field value = %q, want %q (y should be inserted for non-confirm prompts)", got, "y")
	}
}

func TestHandlePromptKeyBackspace(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "question"})
	m.activePrompt.field.SetValue("abc")

	m.handlePromptKey(key(tea.KeyBackspace, 0))
	if got := m.activePrompt.field.Value(); got != "ab" {
		t.Fatalf("field value = %q, want %q", got, "ab")
	}
}

func TestOpenContextMenuOnLiveToolBoxRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.toolBoxes[0].startY})

	if m.contextMenu == nil {
		t.Fatal("expected right-click on a live tool box to open a context menu")
	}
	if m.contextMenu.target != contextMenuTargetTool || m.contextMenu.targetID != "t1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetTool targetID=t1", m.contextMenu)
	}
	if len(m.contextMenu.items) != 2 || m.contextMenu.items[1].label != "Expand" {
		t.Fatalf("items = %+v, want [Copy output, Expand]", m.contextMenu.items)
	}
}

func TestOpenContextMenuOnCommittedToolGroupRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)
	g := m.committedGroups[0]

	cm := m.buildCommittedToolContextMenu(g.lineIdx, 0, 0)
	if cm == nil {
		t.Fatal("expected a menu for a click on the (folded) committed group")
	}
	if cm.target != contextMenuTargetTool || cm.targetID != "t0" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetTool targetID=t0 (first tool)", cm)
	}
}

func TestOpenContextMenuOnCommittedToolGroupRowRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)
	g := m.committedGroups[0]

	// Unfold the group first (mirrors TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow).
	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected the header click to unfold the group")
	}
	if !g.expanded {
		t.Fatal("expected the group to be unfolded")
	}

	// Row layout per renderToolGroupBox: border(0) + header(1) + t0's row(2) + t1's row(3).
	cm := m.buildCommittedToolContextMenu(g.lineIdx+3, 0, 0)
	if cm == nil {
		t.Fatal("expected a menu for a click on t1's row")
	}
	if cm.target != contextMenuTargetToolRow || cm.targetID != "t1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetToolRow targetID=t1", cm)
	}
}

func TestOpenContextMenuOnMessageRightClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{
		Messages: []tauchat.ChatMessage{
			{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello there"},
		},
	}})
	m.View()

	geom := m.computeLayout()
	m.Update(tea.MouseClickMsg{Button: tea.MouseRight, Y: geom.viewportStartY})

	if m.contextMenu == nil {
		t.Fatal("expected right-click on a message to open a context menu")
	}
	if m.contextMenu.target != contextMenuTargetMessage || m.contextMenu.targetID != "u1" {
		t.Fatalf("contextMenu = %+v, want target=contextMenuTargetMessage targetID=u1", m.contextMenu)
	}
	if len(m.contextMenu.items) != 1 || m.contextMenu.items[0].label != "Copy" {
		t.Fatalf("items = %+v, want [Copy]", m.contextMenu.items)
	}
}

func TestContextMenuCopyMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.messageRanges = []messageLineRange{{id: "u1", content: "raw message text", startLine: 0, endLine: 1}}

	cmd := m.activateMessageContextAction("u1", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuMessageCopyUsesRawContentNotStyledLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{
		Messages: []tauchat.ChatMessage{
			{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "**bold** markdown"},
		},
	}})

	// renderedLines holds glamour-rendered (ANSI-styled) output, not the
	// raw "**bold** markdown" - Copy must read messageRanges' stored raw
	// content instead, the same reason lastAssistantText exists.
	var content string
	for _, r := range m.messageRanges {
		if r.id == "a1" {
			content = r.content
		}
	}
	if content != "**bold** markdown" {
		t.Fatalf("stored content = %q, want raw markdown %q", content, "**bold** markdown")
	}
}

// TestCompositeContextMenuPreservesBaseContentOutsideMenu is a regression
// test for a real bug: composing bare Layers directly onto a Canvas (rather
// than through a Compositor) ignores each Layer's X/Y and draws every layer
// starting at (0,0) filling the whole canvas area - which blanks out
// everything the menu layer's own small bounds don't cover, leaving only a
// tiny box in the top-left corner and wiping the rest of the screen.
func TestCompositeContextMenuPreservesBaseContentOutsideMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 40, 10
	m.contextMenu = &contextMenu{
		x: 20, y: 5,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	base := strings.Repeat("base-line-content\n", 9) + "base-line-content"
	out := stripANSI(m.compositeContextMenu(base))

	if !strings.Contains(out, "base-line-content") {
		t.Fatalf("expected base content to survive compositing, got:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected the top row (far from the click at y=5) to still show base content, got blank line: %q", lines[0])
	}
}

// TestCompositeContextMenuPositionsNearClick guards the other half of the
// same bug: the menu must render near m.contextMenu.x/y, not always at the
// canvas origin.
func TestCompositeContextMenuPositionsNearClick(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 60, 20
	m.contextMenu = &contextMenu{
		x: 30, y: 10,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	base := strings.Repeat(strings.Repeat(".", 60)+"\n", 19) + strings.Repeat(".", 60)
	out := stripANSI(m.compositeContextMenu(base))
	lines := strings.Split(out, "\n")

	for i := range 5 {
		if strings.Contains(lines[i], "Copy output") {
			t.Fatalf("expected the menu not to appear near the top of the screen (click was at y=10), found it on line %d: %q", i, lines[i])
		}
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Copy output") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the menu to appear somewhere in the composited output, got:\n%s", out)
	}
}

func TestContextMenuUpDownWraps(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.contextMenu = &contextMenu{items: []contextMenuItem{{label: "a"}, {label: "b"}, {label: "c"}}}

	m.handleContextMenuKey(key(tea.KeyUp, 0))
	if m.contextMenu.selected != 2 {
		t.Fatalf("selected = %d, want 2 (up from 0 wraps to last)", m.contextMenu.selected)
	}
	m.handleContextMenuKey(key(tea.KeyDown, 0))
	if m.contextMenu.selected != 0 {
		t.Fatalf("selected = %d, want 0 (down from last wraps to first)", m.contextMenu.selected)
	}
}

func TestContextMenuEscDismissesWithoutAction(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}

	m.handleContextMenuKey(key(tea.KeyEscape, 0))

	if m.contextMenu != nil {
		t.Fatal("expected esc to close the menu")
	}
	if m.notification != "" {
		t.Fatalf("expected esc to take no action, got notification %q", m.notification)
	}
}

func TestContextMenuEnterActivatesSelectedItemAndCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}

	cmd := m.handleContextMenuKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.contextMenu != nil {
		t.Fatal("expected enter to close the menu")
	}
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected the Copy action to fire, got notification %q", m.notification)
	}
}

func TestContextMenuCopyToolOutputLiveTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha output"}}

	cmd := m.activateToolContextAction("t1", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuCopyToolOutputCommittedGroupJoinsAllResults(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "alpha"},
		{id: "t1", name: "search", status: "done", result: "bravo"},
	}, nil)

	cmd := m.activateToolContextAction("t0", contextMenuActionCopy)
	drainCmd(cmd)

	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected a copied notification, got %q", m.notification)
	}
}

func TestContextMenuExpandCollapseLabelReflectsLiveState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}

	cm := m.buildLiveToolContextMenu("t1", 0, 0)
	if cm.items[1].label != "Expand" {
		t.Fatalf("label = %q, want Expand for a not-yet-expanded tool", cm.items[1].label)
	}

	m.expandedID = "t1"
	cm = m.buildLiveToolContextMenu("t1", 0, 0)
	if cm.items[1].label != "Collapse" {
		t.Fatalf("label = %q, want Collapse for an already-expanded tool", cm.items[1].label)
	}
}

func TestContextMenuToggleExpandLiveTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}

	m.activateToolContextAction("t1", contextMenuActionToggleExpand)
	if m.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1 after toggling expand", m.expandedID)
	}

	m.activateToolContextAction("t1", contextMenuActionToggleExpand)
	if m.expandedID != "" {
		t.Fatalf("expandedID = %q, want empty after toggling collapse", m.expandedID)
	}
}

func TestActivePromptTakesPriorityOverOpenContextMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}},
	}
	m.activePrompt = &formPrompt{kind: promptConfirm, title: "t", message: "m", confirmYes: true}

	m.dispatchKey(key(tea.KeyEnter, 0))

	// The prompt handler ran (resolved and cleared activePrompt via
	// resolvePrompt), not the context-menu handler - the menu must still
	// be open since handlePromptKey never touches it.
	if m.contextMenu == nil {
		t.Fatal("expected the context menu to be untouched by a key routed to the prompt handler")
	}
}

func TestEnqueuePromptClosesOpenContextMenu(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.contextMenu = &contextMenu{items: []contextMenuItem{{label: "x"}}}

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{Kind: "confirm", Title: "t", Message: "m"})

	if m.contextMenu != nil {
		t.Fatal("expected a newly enqueued prompt to close any open context menu")
	}
}

// TestEnqueuePromptClosesOtherExclusiveOverlays covers the tightened
// mutual exclusion introduced by closeOtherExclusiveOverlays
// (docs/specs/state-taxonomy.md, Category 2): previously each open site only
// remembered to clear whichever sibling had caused a problem before (usually
// just contextMenu) - a prompt arriving while a diff viewer or help overlay
// was open left them both set, merely shadowed by activePrompt's higher
// dispatch precedence rather than actually closed.
func TestEnqueuePromptClosesOtherExclusiveOverlays(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.diffViewer = &diffViewerState{title: "a.go"}
	m.helpOverlay = &helpOverlayState{expanded: map[helpRowKey]bool{}}

	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{Kind: "confirm", Title: "t", Message: "m"})

	if m.diffViewer != nil {
		t.Fatal("expected a newly enqueued prompt to close an open diff viewer")
	}
	if m.helpOverlay != nil {
		t.Fatal("expected a newly enqueued prompt to close an open help overlay")
	}
}

func TestCompletionsDoNotConsumeKeysWhileContextMenuOpen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.input = "/help"
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		items: []contextMenuItem{{label: "a"}, {label: "b"}},
	}

	m.dispatchKey(key(tea.KeyDown, 0))

	if m.contextMenu == nil {
		t.Fatal("expected the context menu to stay open")
	}
	if m.contextMenu.selected != 1 {
		t.Fatalf("selected = %d, want 1 - the menu, not the completions dropdown, should have consumed 'down'", m.contextMenu.selected)
	}
}

func TestContextMenuClickAwayDismisses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		x: 5, y: 5,
		items: []contextMenuItem{{label: "Copy output"}, {label: "Expand"}},
	}

	// Far outside the menu's small footprint near (5,5).
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 70, Y: 20})

	if m.contextMenu != nil {
		t.Fatal("expected a click outside the menu's bounds to close it")
	}
}

func TestContextMenuClickInsideActivatesItem(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{{id: "t1", name: "read", status: "done", result: "alpha output"}}
	m.contextMenu = &contextMenu{
		target: contextMenuTargetTool, targetID: "t1",
		x: 5, y: 5,
		items: []contextMenuItem{{label: "Copy output", action: contextMenuActionCopy}, {label: "Expand", action: contextMenuActionToggleExpand}},
	}

	// contextMenuStyle draws a 1-cell border, so item 0 ("Copy output")
	// sits one row below the menu's top-left corner (5,5) -> (5, 6).
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 6, Y: 6})

	if m.contextMenu != nil {
		t.Fatal("expected a click on an item to close the menu")
	}
	drainCmd(cmd)
	if !strings.Contains(m.notification, "copied") {
		t.Fatalf("expected the click to activate Copy output, got notification %q", m.notification)
	}
}

func TestHandlePromptKeyNOnConfirm(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	drainCmd(m.handlePromptKey(key('n', 0)))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if cmd.Confirmed {
		t.Fatal("'n' on confirm should resolve to Confirmed=false")
	}
}

func TestHandlePromptKeyTabTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	m.handlePromptKey(key(tea.KeyTab, 0))
	if m.activePrompt.confirmYes {
		t.Fatal("Tab should toggle confirmYes to false")
	}

	m.handlePromptKey(key(tea.KeyTab, 0))
	if !m.activePrompt.confirmYes {
		t.Fatal("Tab again should toggle back to true")
	}
}

func TestHandleChatEventInteractivePromptClearsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "typing..."
	m.inputCursor = 9

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "input",
	})

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

func TestHandleChatEventInteractivePromptSetsConfirmYes(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm",
	})

	if !m.activePrompt.confirmYes {
		t.Fatal("confirmYes should default to true")
	}
}

func TestHandlePromptKeyLeftTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"})

	m.handlePromptKey(key(tea.KeyLeft, 0))
	if m.activePrompt.confirmYes {
		t.Fatal("Left should toggle confirmYes to false")
	}
}

func TestHandlePromptKeyRightTogglesOnConfirm(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{RequestID: "req-1", Kind: "confirm"})
	m.activePrompt.confirmYes = false

	m.handlePromptKey(key(tea.KeyRight, 0))
	if !m.activePrompt.confirmYes {
		t.Fatal("Right should toggle confirmYes to true")
	}
}

func TestRenderPromptConfirm(t *testing.T) {
	p := &formPrompt{kind: promptConfirm, title: "Confirm?", message: "Are you sure?", confirmYes: true}
	out := stripANSI(renderPrompt(p, 80))
	if !strings.Contains(out, "Yes") || !strings.Contains(out, "No") {
		t.Fatalf("expected Yes/No in confirm prompt:\n%s", out)
	}
	if !strings.Contains(out, "Are you sure?") {
		t.Fatalf("expected message in prompt:\n%s", out)
	}
}

func TestRenderPromptInput(t *testing.T) {
	p := &formPrompt{kind: promptQuestion, title: "Name?", message: "What is your name?", field: newTextField("")}
	out := stripANSI(renderPrompt(p, 80))
	if strings.Contains(out, "Yes") || strings.Contains(out, "No") {
		t.Fatalf("input prompt should not show Yes/No:\n%s", out)
	}
	if !strings.Contains(out, "enter to continue") {
		t.Fatalf("input prompt should show hint:\n%s", out)
	}
}

func TestResolvePromptConfirmTrue(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.enqueuePrompt(tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-cf", Kind: "confirm",
	})

	drainCmd(m.resolvePromptConfirm(true))

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(rt.sent))
	}
	cmd := rt.sent[0].(tauchat.RespondInteractivePromptCommand)
	if !cmd.Confirmed || cmd.Canceled {
		t.Fatal("expected Confirmed=true, Canceled=false")
	}
}

func TestResolvePromptConfirmNoActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.activePrompt = nil

	cmd := m.resolvePromptConfirm(true)
	if cmd != nil {
		t.Fatal("expected nil Cmd when no active prompt")
	}
}

// TestOpenChildTranscriptViewerTerminalChild guards the happy path (CAT-65
// P4.2): a finished child with a recorded session ID opens the overlay and
// kicks off an async load via LoadChildTranscriptCommand.
func TestOpenChildTranscriptViewerTerminalChild(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.width, m.height = 100, 40
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "completed", sessionID: "sess-child-1"},
	}

	drainCmd(m.openChildTranscriptViewer("c1"))

	if m.childTranscriptViewer == nil {
		t.Fatal("expected childTranscriptViewer to be set")
	}
	if !m.childTranscriptViewer.loading {
		t.Error("expected loading=true immediately after open, before the load resolves")
	}
	if m.childTranscriptViewer.sessionID != "sess-child-1" {
		t.Errorf("sessionID = %q, want %q", m.childTranscriptViewer.sessionID, "sess-child-1")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected exactly 1 command sent, got %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.LoadChildTranscriptCommand)
	if !ok {
		t.Fatalf("expected LoadChildTranscriptCommand, got %T", rt.sent[0])
	}
	if cmd.SessionID != "sess-child-1" {
		t.Errorf("LoadChildTranscriptCommand.SessionID = %q, want %q", cmd.SessionID, "sess-child-1")
	}
}

// TestOpenChildTranscriptViewerLiveChild opens the overlay for a running
// (non-terminal) child — live drill-down via in-memory childMessages, per
// CAT-107 (P4.2b). No store command is sent; the overlay renders from the
// live buffer.
func TestOpenChildTranscriptViewerLiveChild(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "working", sessionID: "sess-child-1"},
	}

	drainCmd(m.openChildTranscriptViewer("c1"))

	if m.childTranscriptViewer == nil {
		t.Fatal("expected overlay to open for a live (non-terminal) child")
	}
	if !m.childTranscriptViewer.live {
		t.Error("expected overlay to be in live mode")
	}
	if m.childTranscriptViewer.callID != "c1" {
		t.Errorf("callID = %q, want %q", m.childTranscriptViewer.callID, "c1")
	}
	// No store load command for live children.
	if len(rt.sent) != 0 {
		t.Fatalf("expected no command sent for live child, got %d", len(rt.sent))
	}
}

// TestOpenChildTranscriptViewerNoSessionID covers a child that errored
// before the child process ever reported a session ID - terminal, but
// nothing persisted to drill into.
func TestOpenChildTranscriptViewerNoSessionID(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "failed", sessionID: ""},
	}

	drainCmd(m.openChildTranscriptViewer("c1"))

	if m.childTranscriptViewer != nil {
		t.Fatal("expected no overlay to open for a child with no recorded session ID")
	}
	if len(rt.sent) != 0 {
		t.Fatalf("expected no command sent, got %d", len(rt.sent))
	}
	if m.notification == "" {
		t.Error("expected an inline notification explaining there's nothing to open")
	}
}

// TestHandleChildTranscriptViewerKeyEscCloses guards the close affordance,
// mirroring handleDiffViewerKey's esc/q behavior.
func TestHandleChildTranscriptViewerKeyEscCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 100, 40
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "completed", sessionID: "sess-child-1"},
	}
	drainCmd(m.openChildTranscriptViewer("c1"))
	if m.childTranscriptViewer == nil {
		t.Fatal("setup: expected overlay to be open")
	}

	m.handleChildTranscriptViewerKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.childTranscriptViewer != nil {
		t.Fatal("expected esc to close the overlay")
	}
}
