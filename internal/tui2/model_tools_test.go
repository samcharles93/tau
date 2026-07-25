package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestBashModeIgnoresUnrelatedToolCompletion(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("ls"))

	// An LLM tool call finishing must not be mistaken for the bash command.
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "unrelated-tool-call"})

	if !m.bashRunning {
		t.Error("an unrelated tool completion must not clear bashRunning")
	}
	if m.bashCallID == "" {
		t.Error("an unrelated tool completion must not clear bashCallID")
	}
}

func TestHandleChatEventToolCallDelta(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "read", ArgumentsSummary: "{\"file\":"})
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "", ArgumentsSummary: "\"foo.txt\"}"})

	if len(m.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.tools))
	}
	if m.tools[0].name != "read" {
		t.Fatalf("tool name = %q, want %q", m.tools[0].name, "read")
	}
	if m.tools[0].args != `{"file":"foo.txt"}` {
		t.Fatalf("tool args = %q, want %q", m.tools[0].args, `{"file":"foo.txt"}`)
	}
}

func TestHandleChatEventToolExecutionStarted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "pending"}}

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1"})

	if m.tools[0].status != "running" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "running")
	}
}

func TestShellToolOpensOutputsUponSubmission(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:           "shell-1",
		ToolName:         "shell",
		ArgumentsSummary: `{"command":"git status"}`,
	})

	if m.expandedID != "shell-1" {
		t.Fatalf("expandedID = %q, want shell-1", m.expandedID)
	}
}

func TestHandleChatEventToolExecutionStartedAdoptsFinalCallID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "tool_call_0",
		ToolName:         "tau-plugin-hello__hello_greet",
		ArgumentsSummary: `{"name":"Sam"}`,
	})

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:   "call_real",
		ToolName: "tau-plugin-hello__hello_greet",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected lifecycle event to adopt existing synthetic row, got %d tools: %+v", len(m.tools), m.tools)
	}
	if m.tools[0].id != "call_real" {
		t.Fatalf("tool id = %q, want final call id", m.tools[0].id)
	}
	if m.tools[0].status != "running" {
		t.Fatalf("tool status = %q, want running", m.tools[0].status)
	}
}

func TestToolCallsDontDuplicateEvents(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "tool_call_0",
		Index:            0,
		ToolName:         "docs",
		ArgumentsSummary: `{"query":"tool`,
	})

	// Providers commonly stream the real call ID in a later delta without
	// repeating the function name. Both deltas describe index 0 and must
	// remain one row through the execution lifecycle.
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "call-real",
		Index:            0,
		ArgumentsSummary: ` output"}`,
	})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:   "call-real",
		ToolName: "docs",
	})
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{
		CallID:   "call-real",
		ToolName: "docs",
	})

	if len(m.tools) != 1 {
		t.Fatalf("tool count = %d, want 1: %+v", len(m.tools), m.tools)
	}
	if m.tools[0].id != "call-real" {
		t.Fatalf("tool id = %q, want call-real", m.tools[0].id)
	}
	if m.tools[0].status != "done" {
		t.Fatalf("tool status = %q, want done", m.tools[0].status)
	}
}

func TestHandleChatEventToolExecutionStartedCreatesMissingTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{
		CallID:           "bash-1",
		ToolName:         "shell",
		ArgumentsSummary: "git status",
	})
	m.handleChatEvent(tauchat.ChatToolOutputEvent{CallID: "bash-1", Chunk: "On branch main"})
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{
		CallID:        "bash-1",
		ResultSummary: "On branch main",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.tools))
	}
	tool := m.tools[0]
	if tool.name != "shell" {
		t.Fatalf("tool name = %q, want shell", tool.name)
	}
	if tool.args != "git status" {
		t.Fatalf("tool args = %q, want git status", tool.args)
	}
	if tool.status != "done" {
		t.Fatalf("tool status = %q, want done", tool.status)
	}
	if tool.result != "On branch main" {
		t.Fatalf("tool result = %q, want On branch main", tool.result)
	}
}

func TestHandleChatEventToolExecutionCompletedDone(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "running"}}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1", ResultSummary: "success"})

	if m.tools[0].status != "done" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "done")
	}
	if m.tools[0].result != "success" {
		t.Fatalf("tool result = %q, want %q", m.tools[0].result, "success")
	}
}

func TestHandleChatEventToolExecutionCompletedAdoptsFinalCallID(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:           "tool_call_0",
		ToolName:         "tau-plugin-hello__hello_greet",
		ArgumentsSummary: `{"name":"Sam"}`,
	})

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{
		CallID:        "call_real",
		ToolName:      "tau-plugin-hello__hello_greet",
		ResultSummary: "Hello, Sam!",
	})

	if len(m.tools) != 1 {
		t.Fatalf("expected completed event to adopt existing synthetic row, got %d tools: %+v", len(m.tools), m.tools)
	}
	if m.tools[0].id != "call_real" {
		t.Fatalf("tool id = %q, want final call id", m.tools[0].id)
	}
	if m.tools[0].status != "done" {
		t.Fatalf("tool status = %q, want done", m.tools[0].status)
	}
	if m.tools[0].result != "Hello, Sam!" {
		t.Fatalf("tool result = %q, want final summary", m.tools[0].result)
	}
}

func TestHandleChatEventToolExecutionCompletedError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "running"}}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1", IsError: true, ResultSummary: "failed"})

	if m.tools[0].status != "error" {
		t.Fatalf("tool status = %q, want %q", m.tools[0].status, "error")
	}
}

func TestHandleChatEventToolExecutionCompletedBash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.bashCallID = "bash-abc"

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "bash-abc", ResultSummary: "done"})

	if m.bashRunning {
		t.Fatal("bashRunning should be false when matching bash call ID completes")
	}
	if m.bashCallID != "" {
		t.Fatalf("bashCallID should be cleared, got %q", m.bashCallID)
	}
}

func TestHandleChatEventToolOutputAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", result: "partial"}}

	m.handleChatEvent(tauchat.ChatToolOutputEvent{CallID: "t1", Chunk: " and more"})

	if m.tools[0].result != "partial and more" {
		t.Fatalf("tool result = %q, want %q", m.tools[0].result, "partial and more")
	}
}

func TestHandleChatEventResponseCompletedToolOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = []toolState{{id: "t1", name: "search", status: "done"}}
	m.agentState = agentThinking

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	// A tool-only turn has no real assistant prose, so lastAssistantText
	// (used by /copy) stays empty rather than holding a synthetic
	// placeholder - but the tool call itself must still land in scrollback.
	if m.lastAssistantText != "" {
		t.Fatalf("lastAssistantText = %q, want empty for a tool-only turn", m.lastAssistantText)
	}
	if len(m.tools) != 0 {
		t.Fatalf("expected the tool-only batch to be committed, m.tools still has %d entries", len(m.tools))
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "search") {
		t.Fatalf("expected the tool call to be committed to scrollback, got %q", joined)
	}
}

func TestPendingToolCallsFailWhenInferenceEnds(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.agentState = agentThinking
	m.tools = []toolState{{id: "t1", name: "docs", status: "pending"}}

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if len(m.committedGroups) != 1 || len(m.committedGroups[0].tools) != 1 {
		t.Fatalf("committed tool groups = %+v, want one tool", m.committedGroups)
	}
	tool := m.committedGroups[0].tools[0]
	if tool.status != "error" {
		t.Fatalf("pending tool status after inference ended = %q, want error", tool.status)
	}
	if strings.TrimSpace(tool.result) == "" {
		t.Fatal("pending tool should explain why it failed when inference ended")
	}
}

// TestToolCallDoesNotReorderAheadOfPrecedingText guards against a real bug:
// commentary text the model streamed BEFORE calling a tool was rendering
// AFTER the tool's box once it committed - because renderedLines (baked
// history) always rendered ahead of the live m.streaming buffer, a
// just-committed tool box would visually "jump" above text that
// chronologically came first. upsertToolCall now flushes pending streaming
// text into scrollback the moment a new tool call starts.
func TestToolCallDoesNotReorderAheadOfPrecedingText(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "I'll investigate the workspace."})
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "t1", ToolName: "read"})

	if m.streaming != "" {
		t.Fatalf("streaming = %q, want flushed to empty once a tool call starts", m.streaming)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "investigate the workspace") {
		t.Fatalf("expected preceding text in scrollback, got %q", joined)
	}
	if len(m.tools) != 1 || m.tools[0].id != "t1" {
		t.Fatalf("expected the tool call to still be live (not yet committed), got %+v", m.tools)
	}
}

// TestManyToolCallsCommitAsOneGroup guards against a real bug: a turn with
// many sequential/parallel tool calls committed one box per tool into
// permanent scrollback, so a turn with 100+ tool calls could bury the
// assistant text right before or after it. Multiple tool calls uninterrupted
// by text must collapse into one compact summary once committed.
func TestManyToolCallsCommitAsOneGroup(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	for i := range 8 {
		id := fmt.Sprintf("t%d", i)
		m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: id, ToolName: "read"})
		m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: id})
	}
	// Text resumes after the batch - this is the boundary that commits it.
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "Done reading."})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if len(m.tools) != 0 {
		t.Fatalf("expected the batch to be committed and cleared, m.tools has %d entries", len(m.tools))
	}
	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "8 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '8 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
	}
}

func TestToolGroupMetricsAreSameWhenToggled(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done"},
		{id: "t1", name: "docs", status: "pending"},
		{id: "t2", name: "shell", status: "running"},
		{id: "t3", name: "read", status: "error"},
	}, nil)

	assertMetrics := func(state, got string) {
		t.Helper()
		for _, want := range []string{"4 tool calls", "1 pending", "1 running", "1 error"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s tool group = %q, want %q", state, got, want)
			}
		}
	}

	assertMetrics("collapsed", stripANSI(strings.Join(m.renderedLines, "\n")))
	if !m.toggleCommittedToolAtLine(m.committedGroups[0].lineIdx) {
		t.Fatal("expected tool group toggle to be handled")
	}
	assertMetrics("expanded", stripANSI(strings.Join(m.renderedLines, "\n")))
}

// TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow covers the fix that let
// a committed "N tool calls" group keep its accordion interaction after it
// scrolls into history: it used to freeze forever as one flat summary line
// the moment a group had more than one call, with no way back to the
// per-tool detail. toggleCommittedToolAtLine now mirrors the live group's
// two levels - click the header to unfold into per-tool rows, click a row
// to see its full output, click again to fold back down.
func TestCommittedToolGroupUnfoldsRefoldsAndExpandsRow(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "a.go"},
		{id: "t1", name: "search", status: "done", result: "b.go"},
	}, nil)

	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group, got %d", len(m.committedGroups))
	}
	g := m.committedGroups[0]
	if g.expanded {
		t.Fatal("expected a freshly committed group to start folded")
	}

	// Click the header line (relative row 0) to unfold.
	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a click on the group's header line to be handled")
	}
	if !g.expanded {
		t.Fatal("expected the group to unfold on header click")
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "read") || !strings.Contains(joined, "search") {
		t.Fatalf("expected unfolded group to show each tool's compact row, got %q", joined)
	}
	if strings.Contains(joined, "Press Enter to collapse") {
		t.Fatalf("expected rows to stay compact (no full-detail box) until individually expanded, got %q", joined)
	}

	// Click the second row (relative row 3: border(0) + header(1) + t0's
	// row(2) + t1's row(3) - see renderToolGroupBox's row accounting) to
	// expand just that tool's full output.
	if !m.toggleCommittedToolAtLine(g.lineIdx + 3) {
		t.Fatal("expected a click on a tool row to be handled")
	}
	if g.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1", g.expandedID)
	}
	joined = stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "b.go") {
		t.Fatalf("expected t1's full output visible once expanded, got %q", joined)
	}

	// Click the header TEXT line again (relative row 1, not row 0's
	// border) to fold the whole group back down - a real regression: the
	// fold trigger only matched row 0 (the border character), so clicking
	// the actual visible "N tool calls" text a user would click did
	// nothing at all.
	if !m.toggleCommittedToolAtLine(g.lineIdx + 1) {
		t.Fatal("expected a click on the header text line to be handled")
	}
	if g.expanded {
		t.Fatal("expected the group to fold back down on a header text click")
	}
	joined = stripANSI(strings.Join(m.renderedLines, "\n"))
	if strings.Contains(joined, "read") || strings.Contains(joined, "b.go") {
		t.Fatalf("expected folded group to show only the one-line summary, got %q", joined)
	}
}

func TestCommittedSingleToolOpensAndCloses(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.commitToolGroup([]toolState{
		{id: "t0", name: "read", status: "done", result: "single output"},
	}, nil)

	if len(m.committedGroups) != 1 {
		t.Fatalf("expected 1 committed group, got %d", len(m.committedGroups))
	}
	g := m.committedGroups[0]
	if g.expanded {
		t.Fatal("expected a freshly committed single tool to start collapsed")
	}
	if strings.Contains(stripANSI(strings.Join(m.renderedLines, "\n")), "Press Enter to collapse") {
		t.Fatal("expected collapsed single tool to hide its expanded controls")
	}

	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a click on the single tool box to be handled")
	}
	if !g.expanded {
		t.Fatal("expected single tool to expand on click")
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "single output") {
		t.Fatalf("expected expanded single tool to show output, got %q", joined)
	}

	if !m.toggleCommittedToolAtLine(g.lineIdx) {
		t.Fatal("expected a second click on the single tool box to be handled")
	}
	if g.expanded {
		t.Fatal("expected single tool to collapse on second click")
	}
}

// TestStreamCursorClearsOnToolTransition checks the cursor disappears the
// instant a tool call starts mid-stream - upsertToolCall flushes streaming
// text to scrollback and clears m.streaming, which is what the cursor's
// visibility is keyed on.
func TestStreamCursorClearsOnToolTransition(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.agentState = agentThinking
	m.streaming = "text before the tool call"

	m.upsertToolCall("call-1", "read", `{"path":"a.go"}`, "")

	if m.streaming != "" {
		t.Fatalf("expected m.streaming cleared on tool transition, got %q", m.streaming)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatal("expected no cursor once a tool call has started")
	}
}

func TestHandleChatEventResponseCompletedNoContentNoTools(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = nil
	m.agentState = agentThinking

	cmd := m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	if cmd == nil {
		t.Fatal("expected a Cmd even with empty response")
	}
	if m.inResponse() {
		t.Fatal("inResponse should be false")
	}
}

// TestHandleChatEventRuntimeErrorSkipsRedundantEchoForLoneTool is a
// regression test: a single in-flight tool call failing (e.g. a provider
// streaming a tool-call delta with no function name) previously showed the
// exact same error text three times - the committed lone tool box's compact
// summary, a duplicate "✗ <message>" scrollback line, and the notification
// banner. The scrollback echo is now skipped whenever a lone tool box
// already displays the reason inline (see ChatRuntimeErrorEvent's
// hadLoneTool); the banner (m.notification) and the tool box itself are
// unaffected - this only removes the redundant middle copy.
func TestHandleChatEventRuntimeErrorSkipsRedundantEchoForLoneTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.agentState = agentThinking
	m.tools = []toolState{
		{id: "fc1", name: "shell", status: "pending", startedAt: time.Now()},
	}

	m.handleChatEvent(tauchat.ChatRuntimeErrorEvent{Message: "stream tool call 0 has no function name"})

	if m.notification != "stream tool call 0 has no function name" {
		t.Fatalf("expected the notification banner to still show the error, got %q", m.notification)
	}
	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if got := strings.Count(joined, "stream tool call 0 has no function name"); got != 1 {
		t.Errorf("error text appears %d times in scrollback, want exactly 1 (the tool box) - got:\n%s", got, joined)
	}
	if strings.Contains(joined, "✗ stream tool call") {
		t.Error("expected no redundant '✗ <message>' scrollback echo when the lone tool box already shows the reason")
	}
}

// TestApplySnapshotPreservesToolCalls guards against a real bug: a
// ChatRoleTool message fell into applySnapshot's default/continue branch,
// so every past tool call silently vanished from the viewport the next time
// a snapshot rebuilt it (e.g. right after submitting the next prompt) -
// the same routine-snapshot hazard as the markdown-reverting bug above.
func TestApplySnapshotPreservesToolCalls(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleUser, Content: "list files"},
				{
					Role: tauchat.ChatRoleAssistant,
					ToolCalls: []tauchat.ChatToolCall{
						{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "ls", Arguments: `{"path":"."}`}},
					},
				},
				{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a.go\nb.go"},
			},
		},
	})

	joined := stripANSI(strings.Join(m.renderedLines, "\n"))
	if !strings.Contains(joined, "ls") {
		t.Fatalf("expected replayed tool box to show tool name %q, got %q", "ls", joined)
	}
	if !strings.Contains(joined, "a.go") {
		t.Fatalf("expected replayed tool box to show its result, got %q", joined)
	}
}

// TestApplySnapshotGroupsConsecutiveToolCalls mirrors
// TestManyToolCallsCommitAsOneGroup but for the session-history replay path
// (applySnapshot) - a saved session with a burst of tool calls must replay
// as one compact group too, not one box per call.
func TestApplySnapshotGroupsConsecutiveToolCalls(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	messages := []tauchat.ChatMessage{
		{
			Role: tauchat.ChatRoleAssistant,
			ToolCalls: []tauchat.ChatToolCall{
				{ID: "call-1", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-2", Function: tauchat.ChatFunctionCall{Name: "read"}},
				{ID: "call-3", Function: tauchat.ChatFunctionCall{Name: "read"}},
			},
		},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-1", Content: "a"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-2", Content: "b"},
		{Role: tauchat.ChatRoleTool, ToolCallID: "call-3", Content: "c"},
	}

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{Messages: messages},
	})

	groupLines := 0
	for _, line := range m.renderedLines {
		if strings.Contains(stripANSI(line), "3 tool calls") {
			groupLines++
		}
	}
	if groupLines != 1 {
		t.Fatalf("expected exactly one compact '3 tool calls' summary line, found %d in %v", groupLines, m.renderedLines)
	}
}

func TestFinalizeResponseEmptyWithNoTools(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.tools = nil
	m.reasoning = ""
	m.agentState = agentThinking

	// finalizeResponse only resets streaming/reasoning content - the
	// agentState transition back to agentReady (and so inResponse()) is the
	// caller's responsibility (see handleChatEvent's ChatResponseCompletedEvent
	// case), same as ChatResponseCancelledEvent/ChatRuntimeErrorEvent.
	content := m.finalizeResponse("")

	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
}

func TestRenderToolWithResult(t *testing.T) {
	tool := toolState{name: "read", result: "file contents", status: "done"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "read") {
		t.Fatalf("tool output = %q, want 'read'", out)
	}
	if !strings.Contains(out, "file contents") {
		t.Fatalf("tool output = %q, want 'file contents'", out)
	}
}

func TestRenderToolLongResultTruncated(t *testing.T) {
	longResult := strings.Repeat("x", 100)
	tool := toolState{name: "search", result: longResult, status: "done"}
	out := stripANSI(renderTool(tool, 0))
	if len(out) > 80 {
		t.Fatalf("tool output too long: %d chars", len(out))
	}
	if !strings.Contains(out, "…") {
		t.Logf("long result should be truncated with ellipsis, got %q", out)
	}
}

func TestRenderToolWithoutResult(t *testing.T) {
	tool := toolState{name: "think", status: "running"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "think") {
		t.Fatalf("tool output = %q, want 'think'", out)
	}
}

// TestExpandedToolBoxPopulatesMarkdownCacheAtInnerWidth guards against a
// cache-key mismatch: an expanded tool box renders its markdown result at
// innerWidth = width-8, but mdCache is normally only populated at the full
// terminal width (constructor preload + WindowSizeMsg). A direct lookup at
// innerWidth used to miss every time - silently falling back to raw,
// unrendered markdown - because nothing ever populated the cache at that
// key. renderToolBox must ensure a renderer exists at innerWidth itself.
func TestExpandedToolBoxPopulatesMarkdownCacheAtInnerWidth(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	tool := toolState{id: "t1", name: "read", status: "done", result: "# Heading\n\nSome body text."}
	m.renderToolBox(tool, true, 0, m.width)

	const innerWidth = 72 // width(80) - 8, per renderToolBox's own math
	if r, ok := m.mdCache[innerWidth]; !ok || r == nil {
		t.Fatalf("expected renderToolBox to ensure a glamour renderer in mdCache at innerWidth=%d, got ok=%v", innerWidth, ok)
	}
}

func TestExpandedToolBoxCachesStableBodyAndKeepsLiveTitle(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tool := toolState{
		id:        "t1",
		name:      "read",
		status:    "running",
		result:    "# Heading\n\nSome body text.",
		startedAt: time.Now(),
	}

	first := m.renderToolBox(tool, true, 0, m.width)
	cached, ok := m.expandedToolCache[tool.id]
	if !ok || cached.body == "" {
		t.Fatal("expected expanded tool body to be cached")
	}

	tool.spinnerIdx++
	second := m.renderToolBox(tool, true, 0, m.width)
	if first == second {
		t.Fatal("cached expanded tool box did not refresh its live spinner title")
	}
	if got := m.expandedToolCache[tool.id].body; got != cached.body {
		t.Fatal("spinner-only update rebuilt the cached expanded tool body")
	}
}

func TestExpandedToolBoxCachedCompositionMatchesDirectRender(t *testing.T) {
	title := toolRunningStyle.Render("⠋ read running")
	bodyContent := "\n" + lipgloss.NewStyle().Bold(true).Render("first line") + "\nsecond line"
	// renderToolBox applies this exact Width/Padding combination before
	// cacheAndRenderExpandedTool receives the style. The explicit width is
	// load-bearing: without it, independently rendered header/body blocks
	// size themselves to different content widths.
	style := toolBoxExpandedStyle.Width(80).Padding(0, 1)

	direct := style.Render(title + "\n" + bodyContent)
	cached := renderExpandedToolHeader(style, title, style.Render(bodyContent))
	if cached != direct {
		t.Fatalf("cached composition changed ANSI output:\n direct: %q\n cached: %q", direct, cached)
	}
	if stripANSI(cached) != stripANSI(direct) {
		t.Fatalf("cached composition changed visible layout:\n direct: %q\n cached: %q", stripANSI(direct), stripANSI(cached))
	}
}

func TestExpandedToolBoxCacheInvalidatesWithRenderInputs(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tool := toolState{id: "t1", name: "read", status: "done", result: "# First"}

	m.renderToolBox(tool, true, 0, m.width)
	tool.result = "# Changed"
	changed := stripANSI(m.renderToolBox(tool, true, 0, m.width))
	if !strings.Contains(changed, "Changed") {
		t.Fatalf("expanded output did not update after result change: %q", changed)
	}
	if got := m.expandedToolCache[tool.id].source; got != tool.result {
		t.Fatalf("cached source = %q, want %q", got, tool.result)
	}

	m.renderToolBox(tool, true, 0, 100)
	if got := m.expandedToolCache[tool.id].width; got != 100 {
		t.Fatalf("cached width = %d, want 100 after resize", got)
	}

	m.tools = []toolState{tool}
	m.focusedTool = 0
	m.renderToolBox(tool, true, 0, 100)
	if !m.expandedToolCache[tool.id].focused {
		t.Fatal("focused style did not invalidate expanded tool cache")
	}
}

func TestExpandedToolBoxCacheIsBounded(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for i := range expandedToolCacheLimit + 1 {
		tool := toolState{
			id:     fmt.Sprintf("tool-%d", i),
			name:   "read",
			status: "done",
			result: "# Heading",
		}
		m.renderToolBox(tool, true, 0, m.width)
	}

	if got := len(m.expandedToolCache); got != expandedToolCacheLimit {
		t.Fatalf("expanded tool cache size = %d, want %d", got, expandedToolCacheLimit)
	}
	if _, ok := m.expandedToolCache["tool-0"]; ok {
		t.Fatal("oldest expanded tool cache entry was not evicted")
	}
}

func TestSpaceTogglesToolExpansion(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", status: "done"}}
	m.focusedTool = 0

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.expandedID != "t1" {
		t.Fatalf("expandedID = %q, want t1", m.expandedID)
	}

	m.dispatchKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.expandedID != "" {
		t.Fatalf("expandedID should collapse on second space, got %q", m.expandedID)
	}
}

// TestAdoptToolCallIDReconcilesIDMismatchBetweenStreamAndLifecycle guards
// against duplicate rows for a single call: a gateway can stream one ID
// live in tool-call deltas and then assign a different ID to the same call
// in the final assembled response used to drive started/completed
// lifecycle events. Before this fix, adoptToolCallID only reconciled rows
// still under their original synthetic "tool_call_N" ID; a row already
// upgraded by adoptStreamedToolCallID to a real streamed ID could never be
// re-matched, so the started event created a second, duplicate row while
// the first sat stuck in "pending" until the turn ended.
func TestAdoptToolCallIDReconcilesIDMismatchBetweenStreamAndLifecycle(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// Delta arrives under the synthetic index-based ID first.
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "tool_call_0", Index: 0, ToolName: "read"})
	if len(m.tools) != 1 || m.tools[0].id != "tool_call_0" {
		t.Fatalf("after first delta: tools = %+v, want one row id=tool_call_0", m.tools)
	}

	// A later delta for the same index reveals a real streamed ID -
	// adoptStreamedToolCallID upgrades the row in place.
	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "stream-id-1", Index: 0, ToolName: "read"})
	if len(m.tools) != 1 || m.tools[0].id != "stream-id-1" {
		t.Fatalf("after streamed-ID delta: tools = %+v, want one row id=stream-id-1", m.tools)
	}

	// The lifecycle started event arrives under yet another, unrelated ID -
	// the row must be adopted, not duplicated.
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "lifecycle-id-1", ToolName: "read"})
	if len(m.tools) != 1 {
		t.Fatalf("after started event: len(tools) = %d, want 1 (got duplicate rows: %+v)", len(m.tools), m.tools)
	}
	if m.tools[0].id != "lifecycle-id-1" {
		t.Fatalf("tools[0].id = %q, want lifecycle-id-1", m.tools[0].id)
	}
	if m.tools[0].status != "running" {
		t.Fatalf("tools[0].status = %q, want running", m.tools[0].status)
	}

	// Completion under the same lifecycle ID must settle the single row,
	// not leave the adopted row stuck and spawn yet another.
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "lifecycle-id-1", ToolName: "read"})
	if len(m.tools) != 1 {
		t.Fatalf("after completed event: len(tools) = %d, want 1 (got: %+v)", len(m.tools), m.tools)
	}
	if m.tools[0].status != "done" {
		t.Fatalf("tools[0].status = %q, want done", m.tools[0].status)
	}
}

func TestUpsertToolCallExisting(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", name: "read", args: "old"}}

	m.upsertToolCall("t1", "read", "new", "")

	if m.tools[0].args != "oldnew" {
		t.Fatalf("args = %q, want %q (appended)", m.tools[0].args, "oldnew")
	}
}

func TestSetToolStatusNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.setToolStatus("nonexistent", "running")
}

func TestSetToolResultNotFound(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.setToolResult("nonexistent", "result")
}

func TestSetToolResultAppends(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", result: "part"}}

	m.setToolResult("t1", " two")

	if m.tools[0].result != "part two" {
		t.Fatalf("result = %q, want %q (appended)", m.tools[0].result, "part two")
	}
}

func TestAgentStateToolExecutionStartedTransitionsToRunningTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})

	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Running read") {
		t.Fatalf("status bar = %q, want 'Running read'", plain)
	}
	if !strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, want the interrupt hint", plain)
	}
}

func TestAgentStateRunningToolWithoutRunningEntryHasSafeFallback(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.agentState = agentRunningTool

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Running") {
		t.Fatalf("status bar = %q, want the defensive Running fallback", plain)
	}
}

func TestToolExecutionStartResetsElapsedClock(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	oldStart := time.Now().Add(-time.Minute)
	m.tools = []toolState{{id: "t1", name: "read", status: "pending", startedAt: oldStart}}
	beforeStart := time.Now()

	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})

	if m.tools[0].startedAt.Before(beforeStart) {
		t.Fatalf("startedAt = %v, want execution-start time after %v", m.tools[0].startedAt, beforeStart)
	}
	startedAt := m.tools[0].startedAt
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	if !m.tools[0].startedAt.Equal(startedAt) {
		t.Fatalf("duplicate execution-start reset startedAt: %v -> %v", startedAt, m.tools[0].startedAt)
	}
}

// TestAgentStateToolExecutionCompletedKeepsRunningToolWhileSiblingActive
// guards against the status bar flickering back to "Thinking" and forward
// to "Running <tool>" again between two concurrently running tool calls -
// it should stay on agentRunningTool until every call in the batch settles.
func TestAgentStateToolExecutionCompletedKeepsRunningToolWhileSiblingActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t2", ToolName: "grep"})

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1"})
	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool while t2 is still running", m.agentState)
	}

	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t2"})
	if m.agentState != agentProcessing {
		t.Fatalf("agentState = %v, want agentProcessing once every tool has settled", m.agentState)
	}
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Processing") || strings.Contains(plain, "Thinking") {
		t.Fatalf("status bar = %q, want Processing without Thinking", plain)
	}
}

func TestToolStateDefaultValues(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.upsertToolCall("new-id", "bash", "echo hi", "")

	if m.tools[0].status != "pending" {
		t.Fatalf("new tool status = %q, want %q", m.tools[0].status, "pending")
	}
}

func TestAppendMessageToolPreservesRenderedBlockLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("tool", "╭────╮\n│ ok │\n╰────╯")

	if len(m.renderedLines) != 3 {
		t.Fatalf("expected 3 rendered lines, got %d", len(m.renderedLines))
	}
	if got := stripANSI(m.renderedLines[1]); got != "│ ok │" {
		t.Fatalf("middle line = %q, want unindented tool border line", got)
	}
	if strings.Contains(m.renderedLines[1], "38;2;120;170;255") ||
		strings.Contains(m.renderedLines[1], "38;2;128;134;150") {
		t.Fatalf("tool block line = %q, should not be restyled as chat continuation", m.renderedLines[1])
	}
}

func TestToolStyleForStatus(t *testing.T) {
	tests := []struct {
		name, toolName, status string
	}{
		{"read running", "read", "running"},
		{"read pending", "read", "pending"},
		{"read done", "read", "done"},
		{"read error", "read", "error"},
		{"skill running", "skill", "running"},
		{"skill done", "skill", "done"},
		{"skill error", "skill", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := toolStyleForStatus(tt.toolName, tt.status)
			// Render a small string through the style - smoke test that it
			// doesn't panic and emits output.
			out := s.Render("x")
			if out == "" {
				t.Error("rendered output is empty")
			}
		})
	}
}

func TestSkillLabelFromArgs(t *testing.T) {
	tests := []struct {
		args string
		want string
	}{
		{`{"name":"my-skill"}`, "skill: my-skill"},
		{`{"name":"code-review","other":true}`, "skill: code-review"},
		{"", "skill"},
		{"not json", "skill"},
		{`{"no-name":"here"}`, "skill"},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			if got := skillLabelFromArgs(tt.args); got != tt.want {
				t.Errorf("skillLabelFromArgs(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestRenderChildAgentLine verifies the compact child agent summary line.
func TestRenderChildAgentLine(t *testing.T) {
	tests := []struct {
		name         string
		c            childAgentResult
		wantContains []string
	}{
		{
			name:         "completed",
			c:            childAgentResult{instanceID: "research#k3v9qp", status: "completed", turns: 7, tokens: 12300, durationMs: 8000},
			wantContains: []string{"research#k3v9qp", "completed", "7", "8s"},
		},
		{
			name:         "failed",
			c:            childAgentResult{instanceID: "tau#abc123", status: "failed", turns: 3, tokens: 500, durationMs: 120000},
			wantContains: []string{"tau#abc123", "failed", "3", "2m"},
		},
		{
			name:         "timed_out",
			c:            childAgentResult{instanceID: "plan#x1y2z3", status: "timed_out", turns: 1, tokens: 100, durationMs: 300000},
			wantContains: []string{"plan#x1y2z3", "timed out", "1", "5m"},
		},
		{
			name:         "cancelled",
			c:            childAgentResult{instanceID: "init#d4e5f6", status: "cancelled", turns: 0, tokens: 0, durationMs: 500},
			wantContains: []string{"init#d4e5f6", "cancelled", "0t", "500ms"},
		},
		{
			name:         "budget_exhausted",
			c:            childAgentResult{instanceID: "research#g7h8i9", status: "budget_exhausted", turns: 5, tokens: 45000, durationMs: 45000},
			wantContains: []string{"research#g7h8i9", "budget exhausted", "5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(renderChildAgentLine(tt.c))
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("renderChildAgentLine() = %q, missing %q", got, want)
				}
			}
		})
	}
}

// TestExtractChildAgentResult verifies extraction from agent tool result details.
func TestExtractChildAgentResult(t *testing.T) {
	tests := []struct {
		name    string
		details any
		want    childAgentResult
		wantOK  bool
	}{
		{
			name: "full details",
			details: map[string]any{
				"instance_id": "research#k3v9qp",
				"status":      "completed",
				"usage":       map[string]any{"turns": float64(7), "input_tokens": float64(8000), "output_tokens": float64(4300)},
				"duration_ms": float64(8000),
				"session_id":  "sess-abc123",
			},
			want:   childAgentResult{instanceID: "research#k3v9qp", status: "completed", turns: 7, tokens: 12300, durationMs: 8000, sessionID: "sess-abc123"},
			wantOK: true,
		},
		{
			name:    "missing instance_id",
			details: map[string]any{"status": "completed"},
			wantOK:  false,
		},
		{
			name:    "not a map",
			details: "not a map",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractChildAgentResult(tt.details)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if got.instanceID != tt.want.instanceID {
					t.Errorf("instanceID = %q, want %q", got.instanceID, tt.want.instanceID)
				}
				if got.status != tt.want.status {
					t.Errorf("status = %q, want %q", got.status, tt.want.status)
				}
				if got.turns != tt.want.turns {
					t.Errorf("turns = %d, want %d", got.turns, tt.want.turns)
				}
				if got.tokens != tt.want.tokens {
					t.Errorf("tokens = %d, want %d", got.tokens, tt.want.tokens)
				}
				if got.durationMs != tt.want.durationMs {
					t.Errorf("durationMs = %d, want %d", got.durationMs, tt.want.durationMs)
				}
				if got.sessionID != tt.want.sessionID {
					t.Errorf("sessionID = %q, want %q", got.sessionID, tt.want.sessionID)
				}
			}
		})
	}
}

// TestFocusNextChildSkipsRunningChildren guards the terminal-only gating on
// Tab-cycling child state blocks (CAT-65 P4.2): a still-running child has no
// persisted transcript to drill into yet, so it must never receive focus.
func TestFocusNextChildSkipsRunningChildren(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.childAgentOrder = []string{"c1", "c2", "c3"}
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "a#1", status: "completed", sessionID: "s1"},
		"c2": {instanceID: "a#2", status: "working"},
		"c3": {instanceID: "a#3", status: "failed", sessionID: "s3"},
	}
	m.focusedChild = -1

	m.focusNextChild(1)
	if m.focusedChild != 0 {
		t.Fatalf("focusedChild = %d, want 0 (c1)", m.focusedChild)
	}

	// c2 is running - must be skipped, landing on c3.
	m.focusNextChild(1)
	if m.focusedChild != 2 {
		t.Fatalf("focusedChild = %d, want 2 (c3, skipping running c2)", m.focusedChild)
	}

	// Wraps back around to c1.
	m.focusNextChild(1)
	if m.focusedChild != 0 {
		t.Fatalf("focusedChild = %d, want 0 (wrapped back to c1)", m.focusedChild)
	}
}

// TestFocusNextChildNoneEligibleClearsFocus guards the all-running case: no
// terminal child exists, so focusedChild must reset to -1 rather than stick
// on a stale index.
func TestFocusNextChildNoneEligibleClearsFocus(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.childAgentOrder = []string{"c1"}
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "a#1", status: "working"},
	}
	m.focusedChild = -1

	m.focusNextChild(1)
	if m.focusedChild != -1 {
		t.Fatalf("focusedChild = %d, want -1 (no terminal children)", m.focusedChild)
	}
}

// TestFocusNextToolAndFocusNextChildAreMutuallyExclusive guards the
// two-stage Tab design: landing a real focus in one ring must clear the
// other, so Enter never acts on a stale focus from the wrong ring.
func TestFocusNextToolAndFocusNextChildAreMutuallyExclusive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.tools = []toolState{{id: "t1", status: "done"}}
	m.childAgentOrder = []string{"c1"}
	m.childAgents = map[string]childAgentResult{
		"c1": {instanceID: "a#1", status: "completed", sessionID: "s1"},
	}
	m.focusedTool = -1
	m.focusedChild = -1

	m.focusNextChild(1)
	if m.focusedChild != 0 {
		t.Fatalf("focusedChild = %d, want 0", m.focusedChild)
	}

	m.focusNextTool(1)
	if m.focusedTool != 0 {
		t.Fatalf("focusedTool = %d, want 0", m.focusedTool)
	}
	if m.focusedChild != -1 {
		t.Fatalf("focusedChild = %d, want -1 (cleared by focusNextTool landing a real focus)", m.focusedChild)
	}
}

// TestToolArgLabel covers the argument-preview label generalised from
// skillLabelFromArgs to every builtin tool. Before this, renderTool and
// renderToolBox showed only the bare tool name for anything but "skill" - a
// shell call gave zero visibility into what command was running or ran,
// both live and after completion, which is a real safety gap: an operator
// watching the TUI could not tell a destructive command from a benign one.
func TestToolArgLabel(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"shell", `{"command":"go test ./...","timeout":120}`, "shell: go test ./..."},
		{"grep", `{"pattern":"TODO","path":"internal"}`, "grep: TODO"},
		{"read", `{"path":"internal/foo.go"}`, "read: internal/foo.go"},
		{"read", `{"file":"internal/foo.go"}`, "read: internal/foo.go"},
		{"edit", `{"path":"internal/foo.go","edits":[]}`, "edit: internal/foo.go"},
		{"write", `{"path":"internal/foo.go","content":"x"}`, "write: internal/foo.go"},
		{"find", `{"pattern":"*.go"}`, "find: *.go"},
		// skill behavior must be unchanged (delegates to skillLabelFromArgs).
		{"skill", `{"name":"code-review"}`, "skill: code-review"},
		{"skill", `{"no-name":"x"}`, "skill"},
		// Unknown/plugin tools and malformed args fall back to the bare name
		// rather than guessing at a field that may not exist.
		{"my_plugin_tool", `{"foo":"bar"}`, "my_plugin_tool"},
		{"shell", "not json", "shell"},
		{"shell", "", "shell"},
		{"agent", `{"agent":"tau","prompt":"do the thing"}`, "agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.args, func(t *testing.T) {
			if got := toolArgLabel(tt.name, tt.args); got != tt.want {
				t.Errorf("toolArgLabel(%q, %q) = %q, want %q", tt.name, tt.args, got, tt.want)
			}
		})
	}
}

// A long shell command must not blow out the one-line collapsed row; the
// full command is still visible in the expanded box's raw args.
func TestToolArgLabel_TruncatesLongArgValue(t *testing.T) {
	longCmd := strings.Repeat("x", 200)
	got := toolArgLabel("shell", `{"command":"`+longCmd+`"}`)
	if !strings.HasPrefix(got, "shell: ") {
		t.Fatalf("expected shell: prefix, got %q", got)
	}
	if len(got) > 90 {
		t.Fatalf("label too long: %d chars: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncated label to end with an ellipsis, got %q", got)
	}
}

// The running/pending row must show the arg label too, not just once done -
// that's the actual safety-critical moment, before/while a command executes.
func TestRenderTool_ShowsArgLabelWhileRunning(t *testing.T) {
	tool := toolState{name: "shell", args: `{"command":"rm -rf /tmp/scratch"}`, status: "running"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "rm -rf /tmp/scratch") {
		t.Fatalf("running shell call must show its command, got: %q", out)
	}
}

func TestRenderTool_ShowsArgLabelWhenDone(t *testing.T) {
	tool := toolState{name: "shell", args: `{"command":"go build ./..."}`, result: "(no output)", status: "done"}
	out := stripANSI(renderTool(tool, 0))
	if !strings.Contains(out, "go build ./...") {
		t.Fatalf("completed shell call must show its command, got: %q", out)
	}
	if !strings.Contains(out, "no output") {
		t.Fatalf("completed shell call must still show its result, got: %q", out)
	}
}
