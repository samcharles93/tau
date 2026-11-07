package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func newPeekModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel(&fakeRuntime{}, nil)
	m.setReasoningMode(reasoningModePeek)
	m.width = 80
	return m
}

// longReasoning is several explicit lines so wrapping can't merge them.
const longReasoning = "step one\n\nstep two\n\nstep three\n\nstep four\n\nstep five"

func TestPeekShowsOnlyLiveTail(t *testing.T) {
	m := newPeekModel(t)
	m.agentState = agentThinking
	m.reasoning = longReasoning

	live := m.liveReasoningLines()
	if len(live) != reasoningPeekLines {
		t.Fatalf("peek rendered %d lines, want %d: %q", len(live), reasoningPeekLines, stripANSI(strings.Join(live, "\n")))
	}
	joined := stripANSI(strings.Join(m.viewportLinesForView(true), "\n"))
	if !strings.Contains(joined, "step five") {
		t.Fatalf("peek should show the newest reasoning, got %q", joined)
	}
	if strings.Contains(joined, "step one") {
		t.Fatalf("peek should hide older reasoning, got %q", joined)
	}
}

func TestPeekShortReasoningShownWhole(t *testing.T) {
	m := newPeekModel(t)
	m.reasoning = "just one thought"
	if got := stripANSI(strings.Join(m.liveReasoningLines(), "\n")); !strings.Contains(got, "just one thought") {
		t.Fatalf("short reasoning should render whole, got %q", got)
	}
}

func TestFullModeShowsWholeLiveTrace(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.reasoning = longReasoning
	joined := stripANSI(strings.Join(m.liveReasoningLines(), "\n"))
	if !strings.Contains(joined, "step one") || !strings.Contains(joined, "step five") {
		t.Fatalf("full mode should render the whole trace, got %q", joined)
	}
}

func TestCtrlRExpandsLivePeekForTurn(t *testing.T) {
	m := newPeekModel(t)
	m.agentState = agentThinking
	m.reasoning = longReasoning

	m.dispatchKey(key('r', tea.ModCtrl))
	if !m.peekExpanded {
		t.Fatal("ctrl+r during live reasoning should expand the peek")
	}
	if got := stripANSI(strings.Join(m.liveReasoningLines(), "\n")); !strings.Contains(got, "step one") {
		t.Fatalf("expanded peek should show the whole trace, got %q", got)
	}

	m.dispatchKey(key('r', tea.ModCtrl))
	if m.peekExpanded {
		t.Fatal("second ctrl+r should collapse the peek again")
	}

	m.peekExpanded = true
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	if m.peekExpanded {
		t.Fatal("a new turn should reset the expanded peek")
	}
}

func TestPeekCollapsesReasoningBeforeTools(t *testing.T) {
	m := newPeekModel(t)
	m.reasoning = "plan: read the file"

	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "tool-1", ToolName: "read"})

	if len(m.committedReasoning) != 1 {
		t.Fatalf("committed reasoning blocks = %d, want 1", len(m.committedReasoning))
	}
	if !m.committedReasoning[0].collapsed {
		t.Fatal("peek mode should fold reasoning once tool calls follow it")
	}
}

func TestFullModeKeepsReasoningBeforeToolsOpen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.reasoning = "plan: read the file"

	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{CallID: "tool-1", ToolName: "read"})

	if m.committedReasoning[0].collapsed {
		t.Fatal("full mode should keep pre-tool reasoning expanded, as before")
	}
}

func TestPeekKeepsReasoningOnlyTurnOpen(t *testing.T) {
	m := newPeekModel(t)
	m.reasoning = "the whole response is this reasoning"
	m.agentState = agentThinking
	m.finalizeResponse("msg-1")

	if m.committedReasoning[0].collapsed {
		t.Fatal("a reasoning-only turn should stay expanded, or the response disappears")
	}
}

func TestReasoningCommandModes(t *testing.T) {
	m := newPeekModel(t)

	steps := []struct {
		args string
		want string
	}{
		{"", reasoningModeFull},
		{"", reasoningModeOff},
		{"", reasoningModePeek},
		{"full", reasoningModeFull},
		{"off", reasoningModeOff},
		{"on", reasoningModeFull},
		{"PEEK", reasoningModePeek},
	}
	for _, s := range steps {
		m.cmdReasoning(s.args)
		if got := m.reasoningMode(); got != s.want {
			t.Fatalf("/reasoning %q -> %q, want %q", s.args, got, s.want)
		}
	}

	m.cmdReasoning("loud")
	if got := m.reasoningMode(); got != reasoningModePeek {
		t.Fatalf("unknown argument should leave the mode alone, got %q", got)
	}
}

func TestSetReasoningModeDefaultsToPeek(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.setReasoningMode("")
	if got := m.reasoningMode(); got != reasoningModePeek {
		t.Fatalf("empty mode = %q, want peek", got)
	}
}
