package tui2

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/notify"
)

func TestHandleChatEventSnapshot(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	state := tauchat.ChatSessionState{
		SessionID:    "sess-1",
		Model:        tauchat.ChatModelRef{ID: "gpt-4"},
		ProviderName: "openai",
		Messages: []tauchat.ChatMessage{
			{Role: tauchat.ChatRoleUser, Content: "hello"},
			{Role: tauchat.ChatRoleAssistant, Content: "hi there"},
		},
	}

	m.handleChatEvent(tauchat.ChatSessionSnapshotEvent{State: state})

	if m.modelName != "gpt-4" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-4")
	}
	if m.provider != "openai" {
		t.Fatalf("provider = %q, want %q", m.provider, "openai")
	}
	if len(m.renderedLines) < 2 {
		t.Fatalf("expected at least 2 rendered lines from history, got %d", len(m.renderedLines))
	}
	if m.lastAssistantText != "hi there" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "hi there")
	}
}

func TestHandleChatEventResponseStartedClearsState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "old"
	m.reasoning = "old"
	m.tools = []toolState{{id: "t1"}}
	m.inResponse = false

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})

	if m.streaming != "" {
		t.Fatal("streaming should be cleared on start")
	}
	if m.reasoning != "" {
		t.Fatal("reasoning should be cleared on start")
	}
	if len(m.tools) != 0 {
		t.Fatal("tools should be cleared on start")
	}
	if !m.inResponse {
		t.Fatal("inResponse should be true on start")
	}
}

func TestHandleChatEventDeltaAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello "})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "world"})

	if m.streaming != "hello world" {
		t.Fatalf("streaming = %q, want %q", m.streaming, "hello world")
	}
}

func TestHandleChatEventResponseCompleted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "final answer"
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}
	m.inResponse = true
	m.focused = false // looked away — expect a desktop-notify Cmd

	cmd := m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if cmd == nil {
		t.Fatal("expected a Cmd from response completed")
	}

	if m.inResponse {
		t.Fatal("inResponse should be false after completion")
	}
	if len(m.renderedLines) == 0 {
		t.Fatal("expected rendered lines after completion")
	}
	if m.lastAssistantText != "final answer" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "final answer")
	}
}

// TestApplySnapshotPreservesCommittedGroupExpandState guards the other half
// of the same fix: applySnapshot fully rebuilds m.renderedLines from scratch
// on every snapshot (which fires after every submitted prompt), so without
// carrying expand state forward via oldGroups/committedGroupKey, a group the
// user had unfolded would silently refold the moment they sent their next
// message.
func TestApplySnapshotPreservesCommittedGroupExpandState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{
			Role: tauchat.ChatRoleAssistant,
			ToolCalls: []tauchat.ChatToolCall{
				{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-2", Function: tauchat.ChatFunctionCall{Name: "read"}},
			},
		},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-2", Content: "b"},
	}

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group after first snapshot, got %d", len(m.committedGroups))
	}
	if !m.toggleCommittedToolAtLine(m.committedGroups[0].lineIdx) {
		t.Fatal("expected the header click to be handled")
	}
	if !m.committedGroups[0].expanded {
		t.Fatal("expected the group to be unfolded before the next snapshot")
	}

	// A second snapshot (e.g. after the user sends another message) rebuilds
	// everything from scratch — the group must come back already unfolded.
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group after second snapshot, got %d", len(m.committedGroups))
	}
	if !m.committedGroups[0].expanded {
		t.Fatal("expected the group's unfolded state to survive an applySnapshot rebuild")
	}
}

// TestMessageRangesRecordedOnApplySnapshot verifies applySnapshot records a
// messageLineRange for every message with a real ID, keyed to the exact
// renderedLines span that message's own append produced.
func TestMessageRangesRecordedOnApplySnapshot(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"},
		{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "hi there"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})

	if len(m.messageRanges) != 2 {
		t.Fatalf("expected 2 message ranges, got %d: %+v", len(m.messageRanges), m.messageRanges)
	}
	u1, a1 := m.messageRanges[0], m.messageRanges[1]
	if u1.id != "u1" || u1.startLine != 0 {
		t.Fatalf("u1 range = %+v, want id=u1 startLine=0", u1)
	}
	if a1.id != "a1" || a1.startLine != u1.endLine {
		t.Fatalf("a1 range = %+v, want id=a1 startLine=%d (u1.endLine)", a1, u1.endLine)
	}
	if a1.endLine != len(m.renderedLines) {
		t.Fatalf("a1.endLine = %d, want %d (len(renderedLines))", a1.endLine, len(m.renderedLines))
	}
}

// TestMessageRangesRebuiltOnApplySnapshotRerun verifies a second snapshot
// fully replaces messageRanges rather than appending to stale entries from
// the first rebuild — mirrors renderedLines' own m.renderedLines[:0] reset.
func TestMessageRangesRebuiltOnApplySnapshotRerun(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	first := []tauchat.ChatMessage{{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"}}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: first}})
	if len(m.messageRanges) != 1 {
		t.Fatalf("expected 1 message range after first snapshot, got %d", len(m.messageRanges))
	}

	second := []tauchat.ChatMessage{
		{ID: "u1", Role: tauchat.ChatRoleUser, Content: "hello"},
		{ID: "a1", Role: tauchat.ChatRoleAssistant, Content: "hi there"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: second}})
	if len(m.messageRanges) != 2 {
		t.Fatalf("expected messageRanges rebuilt to exactly 2 entries after second snapshot, got %d: %+v", len(m.messageRanges), m.messageRanges)
	}
}

func TestHandleChatEventResponseCancelled(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = "partial"
	m.reasoning = "thinking"
	m.tools = []toolState{{
		id:        "t1",
		name:      "read",
		status:    "running",
		startedAt: time.Now().Add(-time.Second),
	}}
	m.inResponse = true
	m.steering = true

	m.handleChatEvent(tauchat.ChatResponseCancelledEvent{})

	if m.streaming != "" {
		t.Fatal("streaming should be cleared on cancel")
	}
	if m.reasoning != "" {
		t.Fatal("reasoning should be cleared on cancel")
	}
	if len(m.tools) != 0 {
		t.Fatal("tools should be cleared on cancel")
	}
	if m.inResponse {
		t.Fatal("inResponse should be false on cancel")
	}
	if m.steering {
		t.Fatal("steering should be false on cancel")
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	for _, want := range []string{"partial", "read", "chat request cancelled"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected cancelled turn scrollback to contain %q, got %q", want, joined)
		}
	}
}

func TestHandleChatEventRuntimeError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true
	m.steering = true
	m.streaming = "partial"
	m.reasoning = "thinking"
	m.tools = []toolState{
		{id: "t1", name: "shell", status: "done", result: "ok"},
		{id: "t2", name: "read", status: "pending", startedAt: time.Now().Add(-time.Second)},
	}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "API error"})

	if m.inResponse {
		t.Fatal("inResponse should be false after runtime error")
	}
	if m.steering {
		t.Fatal("steering should be false after runtime error")
	}
	if m.streaming != "" || m.reasoning != "" || m.tools != nil {
		t.Fatal("expected the in-flight turn's streaming/reasoning/tools state cleared")
	}
	if m.notification != "API error" || m.notificationLevel != notify.LevelError {
		t.Fatalf("expected an error-level notification banner, got %q level=%v", m.notification, m.notificationLevel)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	for _, want := range []string{"partial", "2 tool calls", "API error"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected runtime error scrollback to contain %q, got %q", want, joined)
		}
	}
	if len(m.committedGroups) != 1 {
		t.Fatalf("expected interrupted tool batch to be committed, got %d groups", len(m.committedGroups))
	}
	if got := m.committedGroups[0].tools[1].status; got != "error" {
		t.Fatalf("interrupted pending tool status = %q, want error", got)
	}
	if got := m.committedGroups[0].tools[1].result; !strings.Contains(got, "API error") {
		t.Fatalf("interrupted pending tool result = %q, want API error", got)
	}
}

func TestHandleChatEventNotification(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatNotificationEvent{Message: "hello"})

	if m.notification != "hello" {
		t.Fatalf("expected the notification banner to show %q, got %q", "hello", m.notification)
	}
}

func TestHandleChatEventSessionsListed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionsListedEvent{
		Sessions: []tauchat.SessionSummary{{
			ID: "sess-1", ModelID: "gpt-4", MessageCount: 10,
		}},
	})

	if len(m.sessionSummaries) != 1 {
		t.Fatalf("sessionSummaries = %d, want 1", len(m.sessionSummaries))
	}
	if len(m.renderedLines) == 0 {
		t.Fatal("expected the session list to be rendered as a system message")
	}
}

func TestHandleChatEventSessionLoaded(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionLoadedEvent{
		State: tauchat.ChatSessionState{SessionID: "sess-loaded"},
	})

	if m.notification == "" {
		t.Fatal("expected notification about loaded session")
	}
	if !strings.Contains(m.notification, "sess-loaded") {
		t.Fatalf("notification = %q, should mention session ID", m.notification)
	}
}

func TestHandleChatEventSessionLoadedReplaysMessagesAndSeedsHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "old-session"
	m.history = []string{"stale entry"}

	m.handleChatEvent(tauchat.SessionLoadedEvent{
		State: tauchat.ChatSessionState{
			SessionID: "sess-loaded",
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "first prompt"},
				{Role: tauchat.ChatRoleAssistant, Content: "first reply"},
				{Role: tauchat.ChatRoleUser, Content: "second prompt"},
			},
		},
	})

	if m.sessionID != "sess-loaded" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-loaded")
	}
	joined := strings.Join(m.renderedLines, "\n")
	for _, want := range []string{"first prompt", "first reply", "second prompt"} {
		if !strings.Contains(stripANSI(joined), want) {
			t.Fatalf("renderedLines missing %q, got %q", want, joined)
		}
	}
	if len(m.history) != 2 || m.history[0] != "first prompt" || m.history[1] != "second prompt" {
		t.Fatalf("history = %v, want [first prompt, second prompt]", m.history)
	}
}

func TestHandleChatEventSessionDeleted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionDeletedEvent{SessionID: "sess-gone"})

	if m.notification == "" {
		t.Fatal("expected notification about deleted session")
	}
	if !strings.Contains(m.notification, "sess-gone") {
		t.Fatalf("notification = %q, should mention deleted session ID", m.notification)
	}
}

func TestHandleChatEventSessionExported(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SessionExportedEvent{Path: "/tmp/export.jsonl"})

	if m.notification == "" {
		t.Fatal("expected notification about exported session")
	}
}

func TestHandleChatEventExtensionsReloaded(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionsReloadedEvent{
		Result: tauchat.ExtensionReloadResult{ExtensionCount: 3},
	})

	if m.notification == "" {
		t.Fatal("expected notification about extension reload")
	}
	if !strings.Contains(m.notification, "3") {
		t.Fatalf("notification = %q, should mention extension count", m.notification)
	}
}

func TestHandleChatEventExtensionCommandsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionCommandsChangedEvent{
		Commands: []tauchat.ExtensionCommand{
			{Name: "mcp", Description: "MCP commands"},
		},
	})

	if len(m.extensionCommands) != 1 {
		t.Fatalf("extensionCommands = %d, want 1", len(m.extensionCommands))
	}
	if _, ok := m.extensionCommands["mcp"]; !ok {
		t.Fatal("expected 'mcp' in extensionCommands")
	}
}

func TestHandleChatEventExtensionCommandResult(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionCommandResultEvent{Output: "plugin output"})

	// Should append a system message.
	if len(m.renderedLines) == 0 {
		t.Fatal("expected rendered lines after extension command result")
	}
}

func TestHandleChatEventExtensionViewRendered(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionViewRenderedEvent{
		ViewID: "v1",
		View: tauchat.ExtensionView{
			Title:   "Dashboard",
			Widgets: []tauchat.Widget{{}}, // one widget so content shows "1 widgets"
		},
	})

	if len(m.panels) != 1 {
		t.Fatalf("expected 1 panel, got %d", len(m.panels))
	}
	p, ok := m.panels["v1"]
	if !ok || p.title != "Dashboard" {
		t.Fatalf("panel = %+v, want title %q", p, "Dashboard")
	}
}

func TestHandleChatEventExtensionViewClosed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.panels["v1"] = pluginPanel{id: "v1", title: "Dashboard"}

	m.handleChatEvent(tauchat.ExtensionViewClosedEvent{ViewID: "v1"})

	if len(m.panels) != 0 {
		t.Fatalf("expected 0 panels after close, got %d", len(m.panels))
	}
}

func TestHandleChatEventSkillsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SkillsChangedEvent{Skills: []tauchat.SkillInfo{
		{Name: "python", Description: "Python helper", Scope: "project"},
		{Name: "go", Description: "Go helper"},
	}})

	joined := strings.Join(m.renderedLines, "\n")
	for _, want := range []string{"Available Skills", "python", "Python helper", "(project)", "go", "Go helper"} {
		if !strings.Contains(stripANSI(joined), want) {
			t.Fatalf("rendered skills list missing %q, got %q", want, joined)
		}
	}
}

func TestApplySnapshotRebuildsViewport(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			SessionID:    "sess-1",
			Model:        tauchat.ChatModelRef{ID: "claude-3"},
			ProviderName: "anthropic",
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "hi"},
				{Role: tauchat.ChatRoleAssistant, Content: "hello"},
			},
		},
	})

	if m.sessionID != "sess-1" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-1")
	}
	if m.modelName != "claude-3" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "claude-3")
	}
	if m.provider != "anthropic" {
		t.Fatalf("provider = %q, want %q", m.provider, "anthropic")
	}
	// Should have rebuilt renderedLines from messages.
	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "hi") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'hi' to appear in renderedLines after snapshot")
	}
}

// TestApplySnapshotUpdatesStaleSessionID guards against a real bug: without
// updating m.sessionID here, every command sent after /clear, /session <id>,
// or /resume kept targeting the OLD session — the UI would show the new
// session's messages while silently writing into the wrong one server-side.
func TestApplySnapshotUpdatesStaleSessionID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "old-session"

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{SessionID: "new-session"},
	})

	if m.sessionID != "new-session" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "new-session")
	}
}

// TestApplySnapshotRendersAssistantMarkdown guards against a real bug: a
// ChatSessionSnapshotEvent fires routinely (not just on session load), and
// applySnapshot used to rebuild renderedLines straight from raw message
// content instead of routing assistant text through the glamour renderer.
// The visible symptom was a finalized, nicely-rendered response reverting to
// literal "**bold**"/"| table |" markdown text the next time a snapshot
// arrived (e.g. right after submitting the next prompt).
func TestApplySnapshotRendersAssistantMarkdown(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80 // newModel pre-populates the glamour cache for width 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleAssistant, Content: "**bold text**"},
			},
		},
	})

	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, "**bold text**") {
		t.Fatalf("assistant markdown was not glamour-rendered, got raw content: %q", joined)
	}
	if !strings.Contains(stripANSI(joined), "bold text") {
		t.Fatalf("expected rendered output to still contain the text, got %q", stripANSI(joined))
	}
}

// TestApplySnapshotReconstructsBashHistoryBox covers the fix for a bash-mode
// ("!") command losing its tool box on replay: runBashCommand
// (internal/agent/coordinator.go) persists the result as a plain
// ChatRoleUser message in "Ran `cmd`\n\n```\noutput\n```" form since it runs
// outside the normal tool-call loop. applySnapshot must recognize that shape
// and rebuild a real tool box from it instead of showing raw markdown text.
func TestApplySnapshotReconstructsBashHistoryBox(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "Ran `git status`\n\n```\nOn branch main\n```"},
			},
		},
	})

	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "shell") {
		t.Fatalf("expected reconstructed tool box to show tool name %q, got %q", "shell", joined)
	}
	if !strings.Contains(joined, "On branch main") {
		t.Fatalf("expected reconstructed tool box to show its output, got %q", joined)
	}
	if strings.Contains(joined, "Ran `git status`") {
		t.Fatalf("expected raw bash-history markdown to be replaced by a tool box, got %q", joined)
	}
}

// TestApplySnapshotGroupsConsecutiveBashHistory ensures back-to-back bash-mode
// commands (no assistant/user text between them) replay as one compact group,
// matching how a live burst of tool calls collapses via renderCommittedTools.
func TestApplySnapshotGroupsConsecutiveBashHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "Ran `git status`\n\n```\nclean\n```"},
				{Role: tauchat.ChatRoleUser, Content: "Ran `git diff`\n\n```\n\n```"},
			},
		},
	})

	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "2 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '2 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
	}
}

func TestApplySnapshotEmptySessionIDKeepsCurrent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionID = "keep-me"

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{}})

	if m.sessionID != "keep-me" {
		t.Fatalf("sessionID = %q, want unchanged %q", m.sessionID, "keep-me")
	}
}

// TestNotificationWrapsInsteadOfTruncating guards against a real bug: chat
// runtime errors/notifications were previously squeezed into a single-line
// status-bar segment that hard-truncated with "…" once the message didn't
// fit — the notification banner (rendered above the input area) must instead
// wrap across as many lines as it needs.
// TestNotificationWrapsWithinReservedLines verifies a notification that
// fits within notifyReservedLines wraps in full (no mid-word ellipsis
// truncation — the original bug this banner replaced, where a long
// chat-runtime error got squeezed into a single-line status-bar segment
// and hard-truncated there).
func TestNotificationWrapsWithinReservedLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	msg := "short notice that fits in two lines"
	m.notification = msg
	m.notificationLevel = notify.LevelError

	geom := m.computeLayout()
	plain := stripANSI(geom.chromeStr)

	if strings.Contains(plain, "…") {
		t.Fatalf("notification should wrap, not truncate with an ellipsis:\n%s", plain)
	}
	for word := range strings.FieldsSeq(msg) {
		if !strings.Contains(plain, word) {
			t.Fatalf("expected word %q to survive in the rendered chrome, got:\n%s", word, plain)
		}
	}
}

// TestNotificationAreaHasFixedHeight guards against a real UX bug: the
// notification banner only occupied space while m.notification was
// non-empty, so the viewport visibly grew and shrank by that height every
// time a notification appeared or cleared ("pushing text up and dropping
// it back down"). The reserved area must now be exactly notifyReservedLines
// tall regardless of whether there's currently a message — verified here
// by checking the viewport gets the same height either way.
func TestNotificationAreaHasFixedHeight(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for range 40 {
		m.appendMessage("user", "line")
	}

	m.View()
	emptyHeight := m.viewport.Height()

	m.notification = "a notice"
	m.View()
	withNoticeHeight := m.viewport.Height()

	if emptyHeight != withNoticeHeight {
		t.Fatalf("viewport height changed when a notification appeared: %d -> %d, want unchanged", emptyHeight, withNoticeHeight)
	}
}

// TestNotificationLongerThanReservedLinesIsClipped documents the accepted
// trade-off of a fixed-height reserved area: a message that doesn't fit
// within notifyReservedLines is clipped rather than growing the area (and
// therefore resizing the whole layout) to fit it.
func TestNotificationLongerThanReservedLinesIsClipped(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 24})
	m.notification = "this message is deliberately far too long to fit within just two narrow lines of text"

	geom := m.computeLayout()
	notifyLines := strings.Split(stripANSI(geom.chromeStr), "\n")[:notifyReservedLines]
	if len(notifyLines) != notifyReservedLines {
		t.Fatalf("expected exactly %d reserved notification lines, got %d", notifyReservedLines, len(notifyLines))
	}
}

// TestNotificationRendersAboveInput verifies positioning: the notification
// banner must appear before the separator/input in the rendered chrome, not
// buried in the status bar below the input.
func TestNotificationRendersAboveInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.notification = "distinctive-marker-text"

	geom := m.computeLayout()
	plain := stripANSI(geom.chromeStr)

	notifyIdx := strings.Index(plain, "distinctive-marker-text")
	sepIdx := strings.Index(plain, strings.Repeat("─", 10))
	if notifyIdx < 0 {
		t.Fatal("expected the notification text to appear in the chrome")
	}
	if sepIdx < 0 {
		t.Fatal("expected to find the separator line in the chrome")
	}
	if notifyIdx > sepIdx {
		t.Fatalf("expected notification (at %d) to render above the separator/input (at %d)", notifyIdx, sepIdx)
	}
}

func TestSetNotificationReturnsTimerCmd(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	cmd := m.setNotification("hello")
	if cmd == nil {
		t.Fatal("expected a Cmd from setNotification")
	}
	if m.notification != "hello" {
		t.Fatalf("notification = %q, want %q", m.notification, "hello")
	}
	if m.notificationGen == 0 {
		t.Fatal("notificationGen should be > 0")
	}
}

func TestNotificationDefaultDurationsAreTransient(t *testing.T) {
	if defaultNotificationClearDelay <= 0 {
		t.Fatalf("defaultNotificationClearDelay = %v, want positive", defaultNotificationClearDelay)
	}
	if defaultNotifyInfoDuration <= 0 {
		t.Fatalf("defaultNotifyInfoDuration = %v, want positive", defaultNotifyInfoDuration)
	}
	if defaultNotifyWarnDuration <= 0 {
		t.Fatalf("defaultNotifyWarnDuration = %v, want positive", defaultNotifyWarnDuration)
	}
}

func TestClearNotificationGenerationMatch(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.notification = "test"
	m.notificationGen = 5

	// Matching generation clears.
	m.Update(clearNotificationMsg{gen: 5})
	if m.notification != "" {
		t.Fatal("notification should be cleared with matching gen")
	}
}

func TestClearNotificationGenerationMismatch(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.notification = "test"
	m.notificationGen = 5

	// Mismatched generation does NOT clear.
	m.Update(clearNotificationMsg{gen: 3})
	if m.notification != "test" {
		t.Fatal("notification should NOT be cleared with mismatched gen")
	}
}

func TestHandleChatEventCommandsChanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// Should be a no-op — not panic, not crash.
	cmd := m.handleChatEvent(tauchat.CommandsChangedEvent{})
	if cmd != nil {
		t.Fatal("expected nil Cmd from CommandsChangedEvent")
	}
}

func TestHandleChatEventSkillsChangedWithEmptyList(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.SkillsChangedEvent{Skills: nil})

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "no skills available") {
		t.Fatalf("expected 'no skills available' message, got %q", joined)
	}
}

func TestAgentStateDefaultsToReady(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	if m.agentState != agentReady {
		t.Fatalf("agentState = %v, want agentReady (zero value)", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ready") {
		t.Fatalf("status bar = %q, want the Ready label", plain)
	}
}

func TestAgentStateResponseStartedTransitionsToThinking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})

	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Thinking") {
		t.Fatalf("status bar = %q, want 'Thinking'", plain)
	}
	if !strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, want the interrupt hint", plain)
	}
}

func TestAgentStateResponseDeltaTransitionsToStreaming(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})

	if m.agentState != agentStreaming {
		t.Fatalf("agentState = %v, want agentStreaming", m.agentState)
	}
	if m.streamStartedAt.IsZero() {
		t.Fatal("expected streamStartedAt to be set on transition into Streaming")
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "generating") {
		t.Fatalf("status bar = %q, want 'generating'", plain)
	}
}

// TestAgentStateResponseDeltaStreamStartedAtStableAcrossDeltas checks
// streamStartedAt is set once per turn (on the transition into Streaming),
// not reset on every subsequent delta — otherwise a live tok/s estimate
// would never accumulate enough elapsed time to ever become available.
func TestAgentStateResponseDeltaStreamStartedAtStableAcrossDeltas(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello "})
	first := m.streamStartedAt
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "world"})
	if !m.streamStartedAt.Equal(first) {
		t.Fatalf("streamStartedAt changed across deltas: %v -> %v", first, m.streamStartedAt)
	}
}

func TestAgentStateResponseCompletedReturnsToReady(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.agentState != agentReady {
		t.Fatalf("agentState = %v, want agentReady after completion", m.agentState)
	}
}

func TestAgentStateResponseCancelledTransitionsToCancelled(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseCancelledEvent{})

	if m.agentState != agentCancelled {
		t.Fatalf("agentState = %v, want agentCancelled", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Cancelled") {
		t.Fatalf("status bar = %q, want 'Cancelled'", plain)
	}
	if strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, should not show the interrupt hint once cancelled", plain)
	}
}

func TestAgentStateRuntimeErrorTransitionsToError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "connection reset by peer"})

	if m.agentState != agentError {
		t.Fatalf("agentState = %v, want agentError", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Error") {
		t.Fatalf("status bar = %q, want 'Error'", plain)
	}
	// The message itself belongs to the notification banner/scrollback, not
	// the status bar — see TestAgentStateRuntimeErrorStatusBarIsJustTheState.
	if strings.Contains(plain, "connection reset by peer") {
		t.Fatalf("status bar = %q, should not restate the error message", plain)
	}
}

// TestAgentStateRuntimeErrorStatusBarIsJustTheState checks the status bar
// shows only the "Error" state label, not the message itself — the full
// message already has a home in the notification banner (persists until
// dismissed) and scrollback, both on screen at the same time as this bar,
// so restating it here would just be a third copy of the same text.
func TestAgentStateRuntimeErrorStatusBarIsJustTheState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	longMsg := strings.Repeat("x", 200) + "\nsecond line should never appear"
	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: longMsg})

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Error") {
		t.Fatalf("expected status bar to show the Error state, got %q", plain)
	}
	if strings.Contains(plain, "second line") || strings.Contains(plain, strings.Repeat("x", 60)) {
		t.Fatalf("status bar should not restate the error message, got %q", plain)
	}
}

// TestChildTranscriptLoadedEventPopulatesOverlay guards the happy path: a
// matching load result fills the open overlay's viewport and clears loading.
func TestChildTranscriptLoadedEventPopulatesOverlay(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 100, 40
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "completed", sessionID: "sess-child-1"},
	}
	drainCmd(m.openChildTranscriptViewer("c1"))
	if m.childTranscriptViewer == nil {
		t.Fatal("setup: expected overlay to be open")
	}

	m.handleChatEvent(tauchat.ChildTranscriptLoadedEvent{
		SessionID: "sess-child-1",
		Messages: []tauchat.ChatMessage{
			{Role: tauchat.ChatRoleUser, Content: "hello from the child session"},
		},
	})

	if m.childTranscriptViewer.loading {
		t.Error("expected loading=false after a matching ChildTranscriptLoadedEvent")
	}
	content := m.childTranscriptViewer.viewport.View()
	if !strings.Contains(content, "hello from the child session") {
		t.Fatalf("expected viewport content to contain the loaded message, got:\n%s", content)
	}
}

// TestChildTranscriptLoadedEventStaleResponseIgnored guards against a race:
// the user closed the overlay (or reopened it on a different child) before
// an in-flight load returned — the stale result must not clobber current
// state.
func TestChildTranscriptLoadedEventStaleResponseIgnored(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	// No overlay open at all.
	m.handleChatEvent(tauchat.ChildTranscriptLoadedEvent{SessionID: "sess-child-1", Messages: nil})
	if m.childTranscriptViewer != nil {
		t.Fatal("expected no overlay to appear from a load event when none was open")
	}

	// Overlay open, but for a different session (user reopened on another child).
	m.width, m.height = 100, 40
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "completed", sessionID: "sess-child-CURRENT"},
	}
	drainCmd(m.openChildTranscriptViewer("c1"))
	m.handleChatEvent(tauchat.ChildTranscriptLoadedEvent{
		SessionID: "sess-child-STALE",
		Messages:  []tauchat.ChatMessage{{Role: tauchat.ChatRoleUser, Content: "stale content"}},
	})

	if !m.childTranscriptViewer.loading {
		t.Error("expected loading to remain true — the stale event must not have been applied")
	}
	if strings.Contains(m.childTranscriptViewer.viewport.View(), "stale content") {
		t.Fatal("stale load result must not populate the overlay for a different session")
	}
}

// TestChatRuntimeErrorEventClosesStuckOverlay guards against a permanent
// "Loading…" overlay when the child transcript load fails, and confirms the
// failure does not get misrouted into the chat-turn-interruption path.
func TestChatRuntimeErrorEventClosesStuckOverlay(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 100, 40
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "research#k3v9qp", status: "completed", sessionID: "sess-child-1"},
	}
	drainCmd(m.openChildTranscriptViewer("c1"))
	m.tools = []toolState{{id: "unrelated-live-tool", status: "running"}}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{
		SessionID: "sess-child-1",
		Message:   "boom: session not found",
	})

	if m.childTranscriptViewer != nil {
		t.Fatal("expected the stuck overlay to close on a matching load failure")
	}
	if len(m.tools) != 1 || m.tools[0].status != "running" {
		t.Fatal("a child-transcript load failure must not interrupt an unrelated live turn's tools")
	}
	if m.notification == "" {
		t.Error("expected a notification explaining the load failure")
	}
}
