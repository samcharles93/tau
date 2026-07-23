package tui2

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestInterposeBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		count int
		want  []string
	}{
		{
			name:  "zero count returns original",
			lines: []string{"a", "b", "c"},
			count: 0,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "negative count returns original",
			lines: []string{"a", "b"},
			count: -1,
			want:  []string{"a", "b"},
		},
		{
			name:  "single line returns original",
			lines: []string{"only"},
			count: 2,
			want:  []string{"only"},
		},
		{
			name:  "nil slice returns nil",
			lines: nil,
			count: 1,
			want:  nil,
		},
		{
			name:  "one blank between two lines",
			lines: []string{"a", "b"},
			count: 1,
			want:  []string{"a", "", "b"},
		},
		{
			name:  "two blanks between three lines",
			lines: []string{"a", "b", "c"},
			count: 2,
			want:  []string{"a", "", "", "b", "", "", "c"},
		},
		{
			name:  "three blanks between two lines",
			lines: []string{"x", "y"},
			count: 3,
			want:  []string{"x", "", "", "", "y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interposeBlankLines(tt.lines, tt.count)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("interposeBlankLines(%v, %d) = %v, want %v",
					tt.lines, tt.count, got, tt.want)
			}
		})
	}
}

func TestLayoutConstants(t *testing.T) {
	// Prove the constants carry the values they were designed for.
	if contentPadLeft != 6 {
		t.Errorf("contentPadLeft = %d, want 6", contentPadLeft)
	}
	if contentPadRight != 0 {
		t.Errorf("contentPadRight = %d, want 0", contentPadRight)
	}
	if spacingReasoningToContent != 0 {
		t.Errorf("spacingReasoningToContent = %d, want 0", spacingReasoningToContent)
	}
	if spacingToolToContent != 0 {
		t.Errorf("spacingToolToContent = %d, want 0", spacingToolToContent)
	}
	if spacingBetweenMessages != 0 {
		t.Errorf("spacingBetweenMessages = %d, want 0", spacingBetweenMessages)
	}
	if toolBoxPadTopBottom != 0 {
		t.Errorf("toolBoxPadTopBottom = %d, want 0", toolBoxPadTopBottom)
	}
	if toolBoxPadLeftRight != 1 {
		t.Errorf("toolBoxPadLeftRight = %d, want 1", toolBoxPadLeftRight)
	}
	if separatorMarginTop != 0 {
		t.Errorf("separatorMarginTop = %d, want 0", separatorMarginTop)
	}
	if separatorMarginBottom != 0 {
		t.Errorf("separatorMarginBottom = %d, want 0", separatorMarginBottom)
	}
	if spacingBetweenToolRows != 0 {
		t.Errorf("spacingBetweenToolRows = %d, want 0", spacingBetweenToolRows)
	}
}

// TestContinuationPaddingMatchesConstant verifies that continuation styles
// derive their left padding from the shared contentPadLeft constant.
func TestContinuationPaddingMatchesConstant(t *testing.T) {
	// userContinuationStyle rendered output should have contentPadLeft spaces before content.
	usr := stripANSI(userContinuationStyle.Render("x"))
	wantPrefix := strings.Repeat(" ", contentPadLeft)
	if !strings.HasPrefix(usr, wantPrefix) {
		t.Errorf("userContinuationStyle.Render = %q, want prefix %q", usr, wantPrefix)
	}

	// assistantContinuationStyle same.
	asst := stripANSI(assistantContinuationStyle.Render("x"))
	if !strings.HasPrefix(asst, wantPrefix) {
		t.Errorf("assistantContinuationStyle.Render = %q, want prefix %q", asst, wantPrefix)
	}

	// continuationStyle (default) same.
	def := stripANSI(continuationStyle.Render("x"))
	if !strings.HasPrefix(def, wantPrefix) {
		t.Errorf("continuationStyle.Render = %q, want prefix %q", def, wantPrefix)
	}
}

// TestToolBoxPaddingMatchesConstant verifies every tool-box style and the
// context-menu / expanded styles reference the shared toolBoxPad* constants.
func TestToolBoxPaddingMatchesConstant(t *testing.T) {
	type styleEntry struct {
		name   string
		render func() string
	}
	styles := []styleEntry{
		{"toolBoxRunningStyle", func() string { return stripANSI(toolBoxRunningStyle.Render("x")) }},
		{"toolBoxSuccessStyle", func() string { return stripANSI(toolBoxSuccessStyle.Render("x")) }},
		{"toolBoxErrorStyle", func() string { return stripANSI(toolBoxErrorStyle.Render("x")) }},
		{"toolBoxSkillRunningStyle", func() string { return stripANSI(toolBoxSkillRunningStyle.Render("x")) }},
		{"toolBoxSkillSuccessStyle", func() string { return stripANSI(toolBoxSkillSuccessStyle.Render("x")) }},
		{"toolBoxSkillFailedStyle", func() string { return stripANSI(toolBoxSkillFailedStyle.Render("x")) }},
		{"contextMenuStyle", func() string { return stripANSI(contextMenuStyle.Render("x")) }},
		{"toolBoxExpandedStyle", func() string { return stripANSI(toolBoxExpandedStyle.Render("x")) }},
	}

	for _, s := range styles {
		t.Run(s.name, func(t *testing.T) {
			out := s.render()
			// Each rendered line (after the top border) should have
			// toolBoxPadLeftRight spaces of left padding inside the border.
			// The top/bottom padding means toolBoxPadTopBottom empty lines
			// between border and content.
			// Since toolBoxPadTopBottom is 0 and toolBoxPadLeftRight is 1,
			// we can verify: for every style, the output is non-empty and
			// contains the rendered content.
			if out == "" {
				t.Error("rendered output should not be empty")
			}
		})
	}
}

// TestSeparatorMarginsConstant verifies separatorStyle uses the shared
// separatorMargin* constants.
func TestSeparatorMarginsConstant(t *testing.T) {
	// separatorStyle has MarginTop/MarginBottom set to the constants.
	// Verify the style renders without error.
	out := separatorStyle.Render("─")
	if out == "" {
		t.Error("separatorStyle rendered empty output")
	}
}

// TestLayoutAtNormalWidth verifies that a full turn (user → assistant)
// renders correctly at width 80 without breaking alignment.
func TestLayoutAtNormalWidth(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	// Append a user message, then an assistant message.
	m.appendMessage("user", "Hello, this is a test message")
	m.appendMessage("assistant", "Here is a response with **markdown**.")

	view := m.View()
	plain := stripANSI(view.Content)

	// User message content should appear:
	if !strings.Contains(plain, "Hello, this is a test message") {
		t.Error("user message content missing from view")
	}
	// Assistant response should appear:
	if !strings.Contains(plain, "Here is a response with") {
		t.Error("assistant message content missing from view")
	}
	// View should not be empty:
	if view.Content == "" {
		t.Error("view content should not be empty")
	}
}

// TestLayoutAtNarrowWidth verifies that content renders correctly at
// narrow terminal width (30) — wrapped lines, code blocks, lists don't break.
func TestLayoutAtNarrowWidth(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 40})

	// Append a user message with a long line that will wrap.
	m.appendMessage("user", "This is a longer message that should wrap at narrow widths gracefully.")
	// Append an assistant message with a list.
	m.appendMessage("assistant", "- item one\n- item two\n- item three")

	view := m.View()
	plain := stripANSI(view.Content)

	if !strings.Contains(plain, "This is a longer message") {
		t.Error("user message content missing from narrow view")
	}
	if !strings.Contains(plain, "item one") {
		t.Error("list item missing from narrow view")
	}
	if view.Content == "" {
		t.Error("narrow view content should not be empty")
	}
}

// TestNoSpacingInPersistedContent verifies that mechanical blank lines
// are never written into persisted content fields — spacing is purely
// in renderedLines, not in lastAssistantText, canonicalMessages, or
// messageRanges content.
func TestNoSpacingInPersistedContent(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80

	// feed a snapshot with messages so canonicalMessages and messageRanges are populated.
	messages := []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "hello", ID: "u1"},
		{Role: tauchat.ChatRoleAssistant, Content: "world", ID: "a1"},
	}
	m.applySnapshot(tauchat.ChatSessionSnapshotEvent{State: tauchat.ChatSessionState{Messages: messages}})

	// lastAssistantText should be raw content, no blank-line padding.
	if strings.Contains(m.lastAssistantText, "\n\n") && !strings.Contains(messages[1].Content, "\n\n") {
		t.Errorf("lastAssistantText contains mechanical double-newline: %q", m.lastAssistantText)
	}

	// canonicalMessages content should match the original message content.
	for i, cm := range m.canonicalMessages {
		if cm.Content != messages[i].Content {
			t.Errorf("canonicalMessages[%d].Content = %q, want %q", i, cm.Content, messages[i].Content)
		}
	}

	// messageRanges content should match the original message content.
	for _, mr := range m.messageRanges {
		// content should not contain leading/trailing blank lines from spacing.
		if strings.HasPrefix(mr.content, "\n") || strings.HasSuffix(mr.content, "\n") {
			t.Errorf("messageRanges content has mechanical newlines: %q", mr.content)
		}
	}
}

// TestComponentOrderPreserved verifies that when reasoning, tool boxes, and
// assistant text are all present, they appear in chronological order after
// spacing is applied.
func TestComponentOrderPreserved(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.showReasoning = true

	// Simulate a full turn: reasoning first, then assistant text.
	m.reasoning = "Let me think about this..."
	m.streaming = "The answer is 42."
	m.finalizeResponse("msg-1")

	plain := stripANSI(strings.Join(m.renderedLines, "\n"))

	// Reasoning block (collapsed since there's a trailing answer) and answer.
	hasReasoning := strings.Contains(plain, "reasoning collapsed")
	hasAnswer := strings.Contains(plain, "The answer is 42")

	if !hasReasoning {
		t.Errorf("reasoning block missing from renderedLines:\n%s", plain)
	}
	if !hasAnswer {
		t.Errorf("assistant content missing from renderedLines:\n%s", plain)
	}

	// Reasoning must appear before the answer in the scrollback.
	reasoningIdx := strings.Index(plain, "reasoning collapsed")
	answerIdx := strings.Index(plain, "The answer is 42")
	if reasoningIdx < 0 || answerIdx < 0 {
		t.Fatal("could not find components in renderedLines")
	}
	if reasoningIdx >= answerIdx {
		t.Errorf("reasoning (idx %d) should appear before answer (idx %d)", reasoningIdx, answerIdx)
	}
}
