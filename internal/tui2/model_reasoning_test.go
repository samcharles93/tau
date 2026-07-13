package tui2

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

func TestHandleChatEventReasoningDelta(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "thinking..."})

	if m.reasoning != "thinking..." {
		t.Fatalf("reasoning = %q, want %q", m.reasoning, "thinking...")
	}
}

// TestHandleChatEventResponseCompletedReasoningOnly guards against a
// regression where a reasoning-only turn (no trailing answer text) was
// rendered as a literal "[reasoning only]" placeholder instead of the real
// reasoning text — and more generally, where reasoning was never committed
// to scrollback at all and only ever existed in the live view, vanishing
// the instant the turn completed (see finalizeResponse).
func TestHandleChatEventResponseCompletedReasoningOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.streaming = ""
	m.reasoning = "I think..."
	m.inResponse = true

	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	found := false
	for _, line := range m.renderedLines {
		if strings.Contains(line, "I think...") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("renderedLines = %q, want a line containing the reasoning text", m.renderedLines)
	}
	if m.reasoning != "" {
		t.Fatalf("m.reasoning = %q, want cleared after finalizeResponse", m.reasoning)
	}
}

// TestReasoningStyleDistinctFromFinalAnswer guards the visual-hierarchy
// contract: reasoning must render with a style distinct from (and dimmer
// than) the final answer, the answer must remain unstyled/full-brightness
// so it stays the strongest element on screen, and the Warm Ochre accent
// must appear only on the "│ " bar prefixed to every reasoning line — never
// painted across the reasoning text itself, and never on the answer.
func TestReasoningStyleDistinctFromFinalAnswer(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 20 // narrow enough to force reasoning across multiple lines
	m.streaming = "the final answer"
	m.reasoning = "weighing several possible options carefully"
	m.inResponse = true

	m.finalizeResponse("")

	// The turn produced a trailing answer, so the block auto-collapses (see
	// commitReasoningBlock) — expand it to inspect the expanded styling this
	// test is actually about.
	if !m.toggleReasoningBlock(m.lastReasoningKey) {
		t.Fatal("expected toggleReasoningBlock to find the just-committed block")
	}

	// lipgloss renders an explicit truecolor Foreground as a decimal
	// "38;2;R;G;B" SGR sequence, not hex.
	accentSGR := fmt.Sprintf("38;2;%d;%d;%d", theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2])
	mutedSGR := fmt.Sprintf("38;2;%d;%d;%d", theme.ToneMuted[0], theme.ToneMuted[1], theme.ToneMuted[2])

	var reasoningLines []string
	var answerLine string
	for _, line := range m.renderedLines {
		switch {
		case strings.Contains(line, "│"):
			reasoningLines = append(reasoningLines, line)
		case strings.Contains(stripANSI(line), "final"):
			// The answer goes through glamour, which may style/wrap each
			// word separately, so match on stripped content rather than a
			// literal substring of the raw ANSI-laden line.
			answerLine = line
		}
	}
	if len(reasoningLines) < 2 {
		t.Fatalf("renderedLines = %q, want reasoning wrapped across multiple bar-prefixed lines", m.renderedLines)
	}
	if answerLine == "" {
		t.Fatalf("renderedLines = %q, want a line containing the final answer", m.renderedLines)
	}

	for _, reasoningLine := range reasoningLines {
		if !strings.Contains(reasoningLine, accentSGR) {
			t.Fatalf("every reasoning line should carry the Warm Ochre bar, got %q", reasoningLine)
		}
		if strings.Count(reasoningLine, accentSGR) != 1 {
			t.Fatalf("Warm Ochre accent should appear exactly once per line (on the bar), not across the body: %q", reasoningLine)
		}
		if !strings.Contains(reasoningLine, mutedSGR) {
			t.Fatalf("reasoning body should carry the explicit muted foreground (not rely on Faint alone), got %q", reasoningLine)
		}
		if !containsFaintSGR(reasoningLine) {
			t.Fatalf("reasoning body should also carry the faint (dim) SGR code as a secondary cue, got %q", reasoningLine)
		}
	}

	// The final answer must never inherit reasoning's muted/dim treatment.
	if strings.Contains(answerLine, mutedSGR) || containsFaintSGR(answerLine) {
		t.Fatalf("final answer line should not be dimmed like reasoning, got %q", answerLine)
	}
	if strings.Contains(answerLine, accentSGR) {
		t.Fatalf("final answer line should not carry the reasoning accent color, got %q", answerLine)
	}
}

// TestReasoningBlockCollapsedAndExpandedRendering checks the two render
// states directly: collapsed shows a single subtle line (no raw reasoning
// text, no keybinding hint baked in — that's /help's job), expanded shows
// the full multi-line body with the raw text intact.
func TestReasoningBlockCollapsedAndExpandedRendering(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	b := &committedReasoningBlock{key: "k", text: "the raw reasoning text", collapsed: true}
	collapsed := m.renderCommittedReasoning(b)
	if strings.Contains(collapsed, "the raw reasoning text") {
		t.Fatalf("collapsed rendering should not include the raw text, got %q", collapsed)
	}
	if strings.Contains(collapsed, "ctrl+r") || strings.Contains(strings.ToLower(collapsed), "ctrl+r") {
		t.Fatalf("collapsed rendering should not bake in the keybinding hint (belongs in /help only), got %q", collapsed)
	}
	if got := len(strings.Split(collapsed, "\n")); got != 1 {
		t.Fatalf("collapsed rendering should be a single line, got %d lines: %q", got, collapsed)
	}
	if !strings.Contains(stripANSI(collapsed), "reasoning collapsed") {
		t.Fatalf("collapsed rendering should carry a clear indicator, got %q", stripANSI(collapsed))
	}

	b.collapsed = false
	expanded := m.renderCommittedReasoning(b)
	if !strings.Contains(expanded, "the raw reasoning text") {
		t.Fatalf("expanded rendering should include the raw text, got %q", expanded)
	}
}

// TestReasoningBlockContentPreservedAcrossCollapse guards the "presentation
// state only" requirement: toggling collapse must never mutate the block's
// underlying text, and the persisted ChatMessage.ReasoningContent (re-read
// on every applySnapshot rebuild) is what's authoritative — collapsing is
// purely a rendering decision layered on top.
func TestReasoningBlockContentPreservedAcrossCollapse(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.streaming = "the final answer"
	m.reasoning = "the original reasoning text"
	m.inResponse = true
	m.finalizeResponse("msg-1")

	if len(m.committedReasoning) != 1 {
		t.Fatalf("expected one committed reasoning block, got %d", len(m.committedReasoning))
	}
	original := m.committedReasoning[0].text
	if original != "the original reasoning text" {
		t.Fatalf("unexpected initial text %q", original)
	}

	// Toggle collapsed -> expanded -> collapsed again.
	for range 2 {
		if !m.toggleReasoningBlock("msg-1") {
			t.Fatal("expected toggleReasoningBlock to find block \"msg-1\"")
		}
		if m.committedReasoning[0].text != original {
			t.Fatalf("collapse toggle mutated the underlying text: got %q, want %q", m.committedReasoning[0].text, original)
		}
	}

	// A snapshot rebuild re-reads ReasoningContent from the persisted
	// message, independent of whatever presentation state the toggles left
	// behind — collapsing never alters the message that would be sent back
	// to the model or saved to disk.
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{ID: "msg-1", Role: tauchat.ChatRoleAssistant, ReasoningContent: original, Content: "the final answer"},
			},
		},
	})
	if len(m.committedReasoning) != 1 || m.committedReasoning[0].text != original {
		t.Fatalf("expected persisted reasoning text to survive rebuild unchanged, got %+v", m.committedReasoning)
	}
}

// TestCtrlRTogglesLastReasoningBlock checks the keyboard toggle end to end:
// ctrl+r flips the most recently committed block's collapse state and
// re-renders it in place, and is a no-op with reasoning disabled or before
// anything has completed.
func TestCtrlRTogglesLastReasoningBlock(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// No completed block yet: ctrl+r is a no-op, not a panic.
	m.dispatchKey(key('r', tea.ModCtrl))
	if len(m.committedReasoning) != 0 {
		t.Fatalf("expected no committed reasoning blocks yet, got %d", len(m.committedReasoning))
	}

	m.streaming = "the final answer"
	m.reasoning = "weighing the options"
	m.inResponse = true
	m.finalizeResponse("msg-1")

	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected the block to auto-collapse (turn produced a trailing answer)")
	}

	m.dispatchKey(key('r', tea.ModCtrl))
	if m.committedReasoning[0].collapsed {
		t.Fatal("expected ctrl+r to expand the collapsed block")
	}
	if !strings.Contains(strings.Join(m.renderedLines, "\n"), "weighing the options") {
		t.Fatal("expected expanding via ctrl+r to reveal the raw reasoning text in renderedLines")
	}

	m.dispatchKey(key('r', tea.ModCtrl))
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected a second ctrl+r to collapse the block again")
	}

	// Reasoning disabled: ctrl+r must not touch the (still-persisted) block.
	m.showReasoning = false
	m.dispatchKey(key('r', tea.ModCtrl))
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected ctrl+r to be a no-op while reasoning is disabled")
	}
}

// TestStreamingReasoningRemainsVisibleWhileActive checks the split this
// feature relies on: only *completed* reasoning is collapsible — the
// in-progress turn's reasoning is rendered fresh from m.reasoning every
// frame and is never folded, even after a sibling turn's block has already
// been committed and auto-collapsed.
func TestStreamingReasoningRemainsVisibleWhileActive(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// A prior, completed turn: auto-collapses since it has a trailing answer.
	m.streaming = "prior answer"
	m.reasoning = "prior reasoning"
	m.inResponse = true
	m.finalizeResponse("msg-1")
	if !m.committedReasoning[0].collapsed {
		t.Fatal("expected the prior turn's reasoning to auto-collapse")
	}

	// A new turn starts streaming reasoning — must render in full, live,
	// regardless of the previous block's collapsed state.
	m.inResponse = true
	m.reasoning = "currently streaming reasoning"
	m.streaming = ""

	view := m.viewportLinesForView(true)
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, "currently streaming reasoning") {
		t.Fatalf("expected live streaming reasoning to render in full, got %q", stripANSI(joined))
	}
}

// TestApplySnapshotRendersPersistedReasoning guards against a real bug
// twinned with finalizeResponse's live-reasoning commit: reasoning is
// persisted per-message (ChatMessage.ReasoningContent) specifically so it
// survives a snapshot rebuild, but applySnapshot ignored that field
// entirely — a reasoning block that finalizeResponse had just committed to
// scrollback would render correctly for one turn, then vanish the moment
// the next prompt triggered a rebuild here, since this function is the sole
// source of truth for renderedLines.
//
// Since this message has a trailing answer, the block auto-collapses (see
// commitReasoningBlock) — the raw text must still survive uncollapsed in
// m.committedReasoning (collapsing is presentation-only, never touches the
// persisted/source content), and expanding it must reveal the exact text.
func TestApplySnapshotRendersPersistedReasoning(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{
		State: tauchat.ChatSessionState{
			Messages: []tauchat.ChatMessage{
				{Role: tauchat.ChatRoleAssistant, ReasoningContent: "carefully considering", Content: "the answer"},
			},
		},
	})

	if len(m.committedReasoning) != 1 {
		t.Fatalf("expected one committed reasoning block, got %d", len(m.committedReasoning))
	}
	if m.committedReasoning[0].text != "carefully considering" {
		t.Fatalf("expected raw reasoning text preserved, got %q", m.committedReasoning[0].text)
	}
	if !m.committedReasoning[0].collapsed {
		t.Fatalf("expected block to auto-collapse since the message has a trailing answer")
	}
	joined := strings.Join(m.renderedLines, "\n")
	if strings.Contains(joined, "carefully considering") {
		t.Fatalf("expected collapsed block not to show its raw text yet, got %q", joined)
	}
	if !strings.Contains(stripANSI(joined), "the answer") {
		t.Fatalf("expected assistant content to still render, got %q", stripANSI(joined))
	}

	if !m.toggleReasoningBlock(m.committedReasoning[0].key) {
		t.Fatal("expected toggleReasoningBlock to find the committed block")
	}
	joined = strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "carefully considering") {
		t.Fatalf("expected expanding the block to reveal the raw reasoning text, got %q", joined)
	}
}

func TestAgentStateReasoningDeltaStaysThinking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "considering..."})

	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking", m.agentState)
	}
}

func TestAgentStateReasoningDeltaKeepsRunningTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "checking the result"})

	if m.agentState != agentRunningTool {
		t.Fatalf("agentState = %v, want agentRunningTool while a tool is executing", m.agentState)
	}
}

func TestAgentStateReasoningAfterToolCompletionTransitionsToThinking(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatToolExecutionStartedEvent{CallID: "t1", ToolName: "read"})
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "t1"})
	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "considering the result"})

	if m.agentState != agentThinking {
		t.Fatalf("agentState = %v, want agentThinking after an actual reasoning delta", m.agentState)
	}
}
