package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// TestStreamCursorAppearsWhileStreaming checks the cursor shows up on the
// live view while text is actively streaming, and only there.
func TestStreamCursorAppearsWhileStreaming(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "the response so far"

	view := m.viewportLinesForView(false)
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, streamCursor) {
		t.Fatalf("expected the cursor while streaming, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAbsentBeforeStreamingStarts checks the working indicator
// state (in response, nothing streamed yet) shows no cursor - there's no
// content for it to sit after.
func TestStreamCursorAbsentBeforeStreamingStarts(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = ""

	view := m.viewportLinesForView(false)
	joined := strings.Join(view, "\n")
	if strings.Contains(joined, streamCursor) {
		t.Fatalf("expected no cursor before any text has streamed, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAbsentOnCompletedMessage checks the cursor never survives
// into committed scrollback - it's presentation-only, drawn fresh from
// m.streaming each frame, never written into renderedLines.
func TestStreamCursorAbsentOnCompletedMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "the finished response"
	m.finalizeResponse("msg-1")

	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, streamCursor) {
		t.Fatalf("expected no cursor in committed scrollback, got %q", joined)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatalf("expected no cursor in the view once the turn is finalized, got %q", strings.Join(view, "\n"))
	}
}

// TestStreamCursorIsPresentationOnly checks the cursor never leaks into any
// of the places the raw streamed text feeds: the persisted/copyable text
// (m.lastAssistantText, set in finalizeResponse), and m.streaming itself,
// which is what would flow into token counts and the model's context on the
// next turn if it were ever mutated in place.
func TestStreamCursorIsPresentationOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "plain text response"

	// While actively streaming, the raw buffer itself must stay untouched -
	// rendering must never mutate the source string the cursor is drawn from.
	_ = m.viewportLinesForView(false)
	if strings.Contains(m.streaming, streamCursor) {
		t.Fatalf("m.streaming was mutated to include the cursor: %q", m.streaming)
	}

	content := m.finalizeResponse("msg-1")
	if strings.Contains(content, streamCursor) {
		t.Fatalf("finalizeResponse's returned content includes the cursor: %q", content)
	}
	if strings.Contains(m.lastAssistantText, streamCursor) {
		t.Fatalf("m.lastAssistantText (source for /copy) includes the cursor: %q", m.lastAssistantText)
	}
}

// TestStreamCursorClearsOnCancelAndError checks the cursor disappears on
// both interrupted-turn paths (flushInterruptedTurn -> flushStreamingText).
func TestStreamCursorClearsOnCancelAndError(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.inResponse = true
	m.streaming = "an in-flight response"

	m.flushInterruptedTurn("chat request cancelled")

	if m.streaming != "" {
		t.Fatalf("expected m.streaming cleared after an interrupted turn, got %q", m.streaming)
	}
	view := m.viewportLinesForView(false)
	if strings.Contains(strings.Join(view, "\n"), streamCursor) {
		t.Fatal("expected no cursor after the turn was interrupted")
	}
}

// TestRenderStreamingLinesCursorPlacement checks the cursor lands correctly
// after plain text, a wrapped multi-line response, and a trailing newline
// (its own blank row) - and that reserving room for it never pushes any
// line past the wrap width, which is what would cause terminal-level
// reflow/jitter on every delta.
func TestRenderStreamingLinesCursorPlacement(t *testing.T) {
	t.Run("plain text", func(t *testing.T) {
		lines := renderStreamingLines("hello", 40)
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d: %q", len(lines), lines)
		}
		if !strings.HasSuffix(stripANSI(lines[0]), streamCursor) {
			t.Fatalf("expected cursor at end of line, got %q", stripANSI(lines[0]))
		}
	})

	t.Run("wrapped lines", func(t *testing.T) {
		width := 20
		lines := renderStreamingLines("this is a long response that should wrap across several lines", width)
		if len(lines) < 2 {
			t.Fatalf("expected wrapping to produce multiple lines, got %d", len(lines))
		}
		for i, line := range lines {
			plain := stripANSI(line)
			if i < len(lines)-1 && strings.Contains(plain, streamCursor) {
				t.Fatalf("cursor should only appear on the last line, found it on line %d: %q", i, plain)
			}
			if visibleWidth(plain) > width {
				t.Fatalf("line %d width = %d, want <= %d (cursor pushed line past wrap width): %q", i, visibleWidth(plain), width, plain)
			}
		}
		if !strings.HasSuffix(stripANSI(lines[len(lines)-1]), streamCursor) {
			t.Fatalf("expected cursor at end of last line, got %q", stripANSI(lines[len(lines)-1]))
		}
	})

	t.Run("trailing newline", func(t *testing.T) {
		lines := renderStreamingLines("finished a line\n", 40)
		last := stripANSI(lines[len(lines)-1])
		if last != streamCursor {
			t.Fatalf("expected the cursor alone on its own row after a trailing newline, got %q", last)
		}
	})
}

func TestSeedHistoryFromMessagesIgnoresEmptyContentAndNonUserRoles(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.seedHistoryFromMessages([]tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "  "},
		{Role: tauchat.ChatRoleAssistant, Content: "assistant text"},
		{Role: tauchat.ChatRoleUser, Content: "real prompt"},
	})

	if len(m.history) != 1 || m.history[0] != "real prompt" {
		t.Fatalf("history = %v, want [real prompt]", m.history)
	}
}

func TestSeedHistoryFromMessagesKeepsExistingWhenNoUserMessages(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"keep me"}

	m.seedHistoryFromMessages([]tauchat.ChatMessage{
		{Role: tauchat.ChatRoleAssistant, Content: "assistant only"},
	})

	if len(m.history) != 1 || m.history[0] != "keep me" {
		t.Fatalf("history = %v, want unchanged [keep me]", m.history)
	}
}

func TestSubmitInputEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "  "

	cmd := m.submitInput()
	if cmd != nil {
		t.Fatal("expected nil Cmd for empty/whitespace input")
	}
}

// TestSubmitInputDuringResponseQueuesByDefault guards the intended design:
// a plain Enter while a response is in flight queues the message behind the
// running turn (startOrQueueTurn's existing inResponse branch) rather than
// steering - steering is reserved for the dedicated Ctrl+S hotkey
// (handleSteer). See TestHandleSteerWithText for the Ctrl+S path.
func TestSubmitInputDuringResponseQueuesByDefault(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = true
	m.input = "hello"

	drainCmd(m.submitInput())

	if m.notification != "queued - will send after current response" {
		t.Fatalf("notification = %q, want the queued notification", m.notification)
	}
	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if len(rt.sent) != 0 {
		t.Fatalf("sent = %d commands, want 0 (queued locally, not sent yet)", len(rt.sent))
	}
	if len(m.turnQueue) != 1 || m.turnQueue[0] != "hello" {
		t.Fatalf("turnQueue = %v, want [\"hello\"]", m.turnQueue)
	}
}

// TestSubmitInputDuringResponseSlashCommandRunsImmediately guards that a
// slash command isn't queued as plain text - it still executes right away
// even mid-turn, since it's not itself an LLM prompt.
func TestSubmitInputDuringResponseSlashCommandRunsImmediately(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = true
	m.input = "/session list"

	drainCmd(m.submitInput())

	if len(m.turnQueue) != 0 {
		t.Fatalf("turnQueue = %v, want empty - slash commands must not be queued as prompts", m.turnQueue)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("sent = %d commands, want 1 (the slash command ran immediately)", len(rt.sent))
	}
}

func TestSubmitInputDuringBash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.bashRunning = true
	m.input = "hello"

	m.submitInput()

	if m.notification == "" {
		t.Fatal("expected notification about waiting for bash command")
	}
}

func TestSubmitInputSlashCommand(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/clear"

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd for /clear")
	}
	if len(m.history) != 1 || m.history[0] != "/clear" {
		t.Fatalf("history = %v, want [/clear]", m.history)
	}
}

func TestSubmitInputBashCommand(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "!ls -la"

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd for bash command")
	}
	if !m.bashRunning {
		t.Fatal("bashRunning should be true after !command")
	}
	if len(m.history) != 1 || m.history[0] != "!ls -la" {
		t.Fatalf("history = %v, want [!ls -la]", m.history)
	}
}

func TestSubmitInputDoubleBangBashCommand(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "!!ls -la"

	drainCmd(m.submitInput())

	sent, ok := rt.sent[0].(tauchat.RunBashCommand)
	if !ok {
		t.Fatalf("expected RunBashCommand, got %T", rt.sent[0])
	}
	if sent.Command != "ls -la" || !sent.Exclude {
		t.Fatalf("got Command=%q Exclude=%v, want Command=%q Exclude=true", sent.Command, sent.Exclude, "ls -la")
	}
}

func TestSubmitInputDebounce(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.lastSubmit = time.Now() // recent submit

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd even with debounce")
	}
	// The debounce check is 300ms; if we just called now, it should fire.
	// Actually the guard checks elapsed < 300ms - let's test the guard fires.
}

func TestSubmitInputRecordsHistory(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false
	m.input = "hello world"

	m.submitInput()

	if len(m.history) != 1 || m.history[0] != "hello world" {
		t.Fatalf("history = %v, want ['hello world']", m.history)
	}
	if m.historyIdx != -1 {
		t.Fatalf("historyIdx = %d, want -1 (reset)", m.historyIdx)
	}
}

func TestSubmitInputClearsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "test"
	m.inputCursor = 4

	m.submitInput()

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

func TestStartOrQueueTurnQueuesBehindRunning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = true

	m.startOrQueueTurn("queued text")

	if len(m.turnQueue) != 1 || m.turnQueue[0] != "queued text" {
		t.Fatalf("turnQueue = %v, want ['queued text']", m.turnQueue)
	}
}

func TestStartOrQueueTurnSendsWhenIdle(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.inResponse = false

	cmd := m.startOrQueueTurn("send me")
	if cmd == nil {
		t.Fatal("expected a Cmd when starting a turn")
	}
	drainCmd(cmd)

	if !m.inResponse {
		t.Fatal("inResponse should be true after starting a turn")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("expected SubmitChatPromptCommand, got %T", rt.sent[0])
	}
	// RequestID is mandatory server-side (ChatSessionState.BeginTurn rejects
	// an empty one with "request id is required") - this exact regression
	// shipped once because no test asserted it was actually populated.
	if sent.RequestID == "" {
		t.Fatal("expected a non-empty RequestID")
	}
}

func TestStartOrQueueTurnClearsPriorTerminalState(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state agentState
	}{
		{name: "cancelled", state: agentCancelled},
		{name: "error", state: agentError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.agentState = tt.state

			m.startOrQueueTurn("try again")

			if m.agentState != agentThinking {
				t.Fatalf("agentState = %v, want agentThinking after a new submission", m.agentState)
			}
		})
	}
}

func TestStartOrQueueTurnAppendsUserMessage(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.inResponse = false

	drainCmd(m.startOrQueueTurn("show this"))

	if len(m.renderedLines) == 0 {
		t.Fatal("expected the user message to be appended to renderedLines")
	}
}

func TestDrainTurnQueueEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.turnQueue = nil

	cmd := m.drainTurnQueue()
	if cmd != nil {
		t.Fatal("expected nil Cmd for empty queue")
	}
}

func TestDrainTurnQueuePopsAndSends(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.turnQueue = []string{"first", "second"}

	drainCmd(m.drainTurnQueue())

	if len(m.turnQueue) != 1 || m.turnQueue[0] != "second" {
		t.Fatalf("turnQueue after drain = %v, want ['second']", m.turnQueue)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
}

func TestClearInputResetsBothFieldAndCursor(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "something"
	m.inputCursor = 5

	m.clearInput()

	if m.input != "" {
		t.Fatalf("input = %q, want empty", m.input)
	}
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.inputCursor)
	}
}

func TestRecallHistoryEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = nil
	m.historyIdx = -1

	cmd := m.recallHistory(-1)
	if cmd != nil {
		t.Fatal("expected nil Cmd when history is empty")
	}
}

func TestRecallHistoryNavigates(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1

	m.recallHistory(-1) // up from start
	if m.input != "second" {
		t.Fatalf("up: input = %q, want %q", m.input, "second")
	}

	m.recallHistory(-1) // up again
	if m.input != "first" {
		t.Fatalf("up again: input = %q, want %q", m.input, "first")
	}

	m.recallHistory(1) // down
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}
}

func TestRecallHistoryClamps(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"only"}
	m.historyIdx = -1

	// Up twice should clamp to the one entry.
	m.recallHistory(-1)
	m.recallHistory(-1)
	if m.input != "only" {
		t.Fatalf("clamped up: input = %q, want %q", m.input, "only")
	}
}

func TestRecallHistoryDownNoNavigationActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1

	m.recallHistory(1) // down without navigating yet
	if m.input != "first" {
		t.Fatalf("down from -1: input = %q, want %q", m.input, "first")
	}
}

// TestRecallHistoryDraftRestore verifies that an in-progress (unsent) draft is
// stashed on first history recall and restored when navigating past the most
// recent history entry (CAT-18).
func TestRecallHistoryDraftRestore(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.history = []string{"first", "second"}
	m.historyIdx = -1
	m.input = "my draft"
	m.inputCursor = 8

	// Up: should stash draft and show most recent history.
	m.recallHistory(-1)
	if m.input != "second" {
		t.Fatalf("up: input = %q, want %q", m.input, "second")
	}
	if m.draftInput != "my draft" {
		t.Fatalf("draft not stashed: draftInput = %q, want %q", m.draftInput, "my draft")
	}

	// Up again: oldest entry.
	m.recallHistory(-1)
	if m.input != "first" {
		t.Fatalf("up again: input = %q, want %q", m.input, "first")
	}

	// Down: back to most recent.
	m.recallHistory(1)
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}

	// Down past most recent: should restore draft.
	m.recallHistory(1)
	if m.input != "my draft" {
		t.Fatalf("down past most recent: input = %q, want %q", m.input, "my draft")
	}
	if m.historyIdx != len(m.history) {
		t.Fatalf("historyIdx = %d, want %d (draft slot)", m.historyIdx, len(m.history))
	}

	// Down again: clamped at draft slot.
	m.recallHistory(1)
	if m.input != "my draft" {
		t.Fatalf("down again: input = %q, want %q", m.input, "my draft")
	}

	// Up from draft: back to most recent history.
	m.recallHistory(-1)
	if m.input != "second" {
		t.Fatalf("up from draft: input = %q, want %q", m.input, "second")
	}
}

func TestMoveCursorRight(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 1

	m.moveCursorRight()
	if m.inputCursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.inputCursor)
	}

	m.moveCursorRight()
	if m.inputCursor != 2 {
		t.Fatalf("cursor should not move past end: got %d", m.inputCursor)
	}
}

func TestAtLastLineEndSingleLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 2

	if !m.atLastLineEnd() {
		t.Fatal("expected at last line end")
	}

	m.inputCursor = 1
	if m.atLastLineEnd() {
		t.Fatal("not at last line end")
	}
}

func TestAtLastLineEndMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = len([]rune(m.input))

	if !m.atLastLineEnd() {
		t.Fatal("cursor at end of last line")
	}
}

func TestMoveCursorVertUpFromNonFirstLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = len([]rune("line one\n")) // start of line two, col 0

	m.moveCursorVert(-1)
	// Should move to same column on line one (col 0).
	if m.inputCursor != 0 {
		t.Fatalf("cursor = %d, want 0 (start of line one)", m.inputCursor)
	}
}

func TestMoveCursorVertDownFromNonLastLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi\nthere"
	m.inputCursor = 1 // col 1 of line 0 ("hi")

	m.moveCursorVert(1)
	// Should move to col 1 on line 1 ("there"), which is index 4.
	if m.inputCursor != 4 {
		t.Fatalf("cursor = %d, want 4 (col 1 on line 1)", m.inputCursor)
	}
}

func TestMoveCursorVertStaysInBounds(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "single"
	m.inputCursor = 0

	m.moveCursorVert(-1) // already on first line
	if m.inputCursor != 0 {
		t.Fatal("cursor should not move above first line")
	}

	m.moveCursorVert(1) // already on last line
	if m.inputCursor != 0 {
		t.Fatal("cursor should not move below last line")
	}
}

func TestMoveCursorVertPreservesColumnWhenTargetLineShorter(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "abc\nde"
	m.inputCursor = 3 // end of line 0 (col 3)

	m.moveCursorVert(1)
	// Line 1 is "de" (2 chars) - should clamp to col 2, i.e. index 4 (start
	// of line 1) + 2 = 6.
	if m.inputCursor != 6 {
		t.Fatalf("cursor = %d, want 6 (end of line 1, col 2)", m.inputCursor)
	}
}

func TestKillToLineStartAtStart(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.inputCursor = 0

	m.killToLineStart()
	if m.input != "hello" {
		t.Fatalf("kill to start at cursor 0 should not change input, got %q", m.input)
	}
}

func TestKillToLineEndAtEnd(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello"
	m.inputCursor = 5

	m.killToLineEnd()
	if m.input != "hello" {
		t.Fatalf("kill to end at end should not change input, got %q", m.input)
	}
}

func TestSplitInputLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line0\nline1\nline2"

	lines := m.splitInputLines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if string(lines[0]) != "line0" {
		t.Fatalf("line 0 = %q, want %q", string(lines[0]), "line0")
	}
}

func TestSplitInputLinesNoNewlines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "single"

	lines := m.splitInputLines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestCursorLineCol(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "abc\nde"
	m.inputCursor = 4

	lines := m.splitInputLines()
	line, col := m.cursorLineCol(lines)
	if line != 1 || col != 0 {
		t.Fatalf("cursor at 4: line=%d col=%d, want line=1 col=0", line, col)
	}
}

func TestUpdatePasteMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(tea.PasteMsg{Content: "pasted text"})
	if cmd != nil {
		t.Fatal("expected nil Cmd from PasteMsg")
	}
	if m.input != "pasted text" {
		t.Fatalf("input = %q, want %q", m.input, "pasted text")
	}
}

func TestRenderInputAreaEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	area := m.renderInputArea()
	if area == "" {
		t.Fatal("expected non-empty input area even for empty input")
	}
	plain := stripANSI(area)
	if !strings.Contains(plain, "╭ chat") {
		t.Fatalf("input area = %q, want chat mode title", plain)
	}
	if !strings.Contains(plain, ">") {
		t.Fatalf("input area = %q, want '>' prompt", plain)
	}
}

func TestRenderInputAreaMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "line one\nline two"
	m.inputCursor = 8 // end of line 0

	area := m.renderInputArea()
	plain := stripANSI(area)
	lines := strings.Split(plain, "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines including padded input box, got %d", len(lines))
	}
	if !strings.Contains(lines[2], "line one") {
		t.Fatalf("line 2 = %q, want 'line one'", lines[2])
	}
	if !strings.Contains(lines[3], "line two") {
		t.Fatalf("line 3 = %q, want 'line two'", lines[3])
	}
}

func TestRenderInputAreaWrapsLongLineInsideBox(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 32
	m.input = "this is a long message that should wrap inside the input box"
	m.inputCursor = len([]rune(m.input))

	area := m.renderInputArea()
	plain := stripANSI(area)
	lines := strings.Split(plain, "\n")
	if len(lines) <= 5 {
		t.Fatalf("expected wrapped input area to grow vertically, got %d lines:\n%s", len(lines), plain)
	}
	for _, line := range lines {
		if visibleWidth(line) > m.width {
			t.Fatalf("input line width = %d, want <= %d: %q\n%s", visibleWidth(line), m.width, line, plain)
		}
	}
	if strings.Contains(plain, "…") {
		t.Fatalf("input should wrap instead of truncate with ellipsis:\n%s", plain)
	}
}

func TestRenderInputAreaWrapsCursorOntoContinuationLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 18
	m.input = "abcdefghijklmnop"
	m.inputCursor = 15

	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, "  op") {
		t.Fatalf("expected wrapped continuation line with cursor cell, got:\n%s", plain)
	}
	for line := range strings.SplitSeq(plain, "\n") {
		if visibleWidth(line) > m.width {
			t.Fatalf("input line width = %d, want <= %d: %q\n%s", visibleWidth(line), m.width, line, plain)
		}
	}
}

// TestRenderInputAreaCapsHeightAndScrollsOverflow guards against the input
// box growing past inputBoxHeightFrac of the terminal and pushing the
// viewport off the top - a long multi-line paste must scroll inside a
// capped box instead of rendering every line.
func TestRenderInputAreaCapsHeightAndScrollsOverflow(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 80, 20 // 60% cap -> max box height 12 -> max body 8 rows

	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		if i > 1 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "line %02d", i)
	}
	m.input = sb.String()
	m.inputCursor = 0 // cursor on the first line

	area := m.renderInputArea()
	lines := strings.Split(area, "\n")
	maxBoxHeight := int(float64(m.height) * inputBoxHeightFrac)
	if len(lines) > maxBoxHeight {
		t.Fatalf("input box height = %d lines, want <= %d (60%% of terminal height %d)", len(lines), maxBoxHeight, m.height)
	}

	plain := stripANSI(area)
	if !strings.Contains(plain, "line 01") {
		t.Fatalf("expected the cursor's line (line 01) to stay visible, got:\n%s", plain)
	}
	if strings.Contains(plain, "line 30") {
		t.Fatalf("expected lines far past the cursor to scroll out of view, got:\n%s", plain)
	}
	if !strings.Contains(plain, "more below") {
		t.Fatalf("expected a scroll indicator when content overflows, got:\n%s", plain)
	}
}

// TestRenderInputAreaFitsWithoutClipping checks the common case is
// unaffected: input short enough to fit within the height cap renders every
// line with no scroll indicator.
func TestRenderInputAreaFitsWithoutClipping(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width, m.height = 80, 40
	m.input = "line one\nline two\nline three"
	m.inputCursor = 0

	plain := stripANSI(m.renderInputArea())
	if !strings.Contains(plain, "line one") || !strings.Contains(plain, "line two") || !strings.Contains(plain, "line three") {
		t.Fatalf("expected all lines to render unclipped, got:\n%s", plain)
	}
	if strings.Contains(plain, "more above") || strings.Contains(plain, "more below") {
		t.Fatalf("expected no scroll indicator when content fits, got:\n%s", plain)
	}
}

func TestRenderInputChunkCursorOverCharacter(t *testing.T) {
	ln := []rune("hello")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 2, false, 0, 0))
	if out != "hello" {
		t.Fatalf("cursor over character = %q, want %q", out, "hello")
	}
}

func TestRenderInputChunkCursorAtEnd(t *testing.T) {
	ln := []rune("hi")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 2, false, 0, 0))
	if out != "hi " {
		t.Fatalf("cursor at end = %q, want 'hi '", out)
	}
}

func TestRenderInputChunkCursorAtStart(t *testing.T) {
	ln := []rune("abc")
	out := stripANSI(renderInputChunk(ln, 0, len(ln), true, 0, false, 0, 0))
	if out != "abc" {
		t.Fatalf("cursor at start = %q, want 'abc'", out)
	}
}

func TestInputPositionAtMapsClickToRunePosition(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.input = "hello world"

	textStartCol := 1 + promptPrefixWidth() // 1 for the left border
	const bodyRow = 2                       // top border + hint row precede body rows

	if got := m.inputPositionAt(bodyRow, textStartCol); got != 0 {
		t.Fatalf("click at text start = %d, want 0", got)
	}
	if got := m.inputPositionAt(bodyRow, textStartCol+5); got != 5 {
		t.Fatalf("click 5 cols in = %d, want 5", got)
	}
	if got := m.inputPositionAt(bodyRow, textStartCol+100); got != len([]rune(m.input)) {
		t.Fatalf("click past the end = %d, want %d", got, len([]rune(m.input)))
	}
}

func TestInsertAtCursorMultiByte(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""
	m.inputCursor = 0

	m.insertAtCursor("😀")
	if m.input != "😀" {
		t.Fatalf("input = %q, want emoji", m.input)
	}
	if m.inputCursor != 1 {
		t.Fatalf("cursor = %d, want 1 (rune count)", m.inputCursor)
	}
}

func TestDeleteAtCursorEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	// Should not panic.
	m.deleteAtCursor()
}

func TestBackspaceAtCursorStart(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hi"
	m.inputCursor = 0

	m.backspaceAtCursor()
	// Should be a no-op.
	if m.input != "hi" {
		t.Fatalf("input = %q, want %q (unchanged)", m.input, "hi")
	}
}

func TestSubmitInputDebounceGuardFires(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "fast message"
	m.lastSubmit = time.Now()

	cmd := m.submitInput()
	if cmd == nil {
		t.Fatal("expected a Cmd even during debounce guard")
	}
}

func TestClearScreenWipesRenderedLinesNotSession(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.appendMessage("user", "hello")
	m.appendMessage("assistant", "hi there")
	m.sessionID = "sess-keep-me"

	m.clearScreen()

	if len(m.renderedLines) != 0 {
		t.Fatalf("renderedLines = %v, want empty", m.renderedLines)
	}
	if m.sessionID != "sess-keep-me" {
		t.Fatalf("sessionID = %q, want unchanged", m.sessionID)
	}
}
