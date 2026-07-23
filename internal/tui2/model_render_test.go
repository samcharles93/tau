package tui2

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCopyCommandReturnsRawContentAfterResponse(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello world"})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.lastAssistantText != "hello world" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "hello world")
	}

	cmd := m.cmdCopy("")
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd once a response has completed")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a BatchMsg containing the clipboard Cmd, got %#v", cmd())
	}

	// Only execute the clipboard sub-cmd - the notification sub-cmd is a
	// 4-second tea.Tick and isn't relevant here.
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.Kind() != reflect.String || v.String() != "hello world" {
		t.Fatalf("clipboard payload = %#v, want %q", clip, "hello world")
	}
	if m.notification == "" {
		t.Error("expected a confirmation notification after copying")
	}
}

func TestCopyCommandNotifiesWhenNothingToCopy(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	// setNotification's returned Cmd is a bare 4s tea.Tick - don't execute
	// it, just check the synchronous m.notification side effect.
	m.cmdCopy("")

	if m.notification != "nothing to copy" {
		t.Fatalf("notification = %q, want %q", m.notification, "nothing to copy")
	}
}

func TestRenderLineHasNoNameLabels(t *testing.T) {
	for _, role := range []string{"user", "assistant", "system"} {
		out := stripANSI(renderLine(role, "git status"))
		if strings.Contains(out, "You:") || strings.Contains(out, "tau:") {
			t.Errorf("renderLine(%q, ...) = %q still contains a literal name label", role, out)
		}
	}
}

// A bash-mode echo (appendMessage("user", "!"+cmd), see handleBashCommand)
// must render the same way as any other user line - no double-marking on
// top of the leading "!".

func TestSessionSummariesTextEmpty(t *testing.T) {
	out := sessionSummariesText(nil, "")
	if out != "Sessions: no saved sessions" {
		t.Fatalf("out = %q, want %q", out, "Sessions: no saved sessions")
	}
}

func TestSessionSummariesTextWithMetricsAndCursor(t *testing.T) {
	out := sessionSummariesText([]tauchat.SessionSummary{
		{
			ID: "sess-1", ModelID: "gpt-4", MessageCount: 3,
			InputTokens: 100, OutputTokens: 50, Cost: 0.02,
			ToolCalls: 2, ToolErrors: 1, DurationMs: 1500,
		},
	}, "cursor-2")

	for _, want := range []string{"sess-1", "3 messages", "gpt-4", "↑100", "↓50", "$0.0200", "2 tools (1 err)", "1s", "More sessions available."} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestSessionSummariesTextTreeWithAgentAttribution(t *testing.T) {
	out := sessionSummariesText([]tauchat.SessionSummary{
		{ID: "root", ModelID: "gpt-4", MessageCount: 5},
		{ID: "child-1", ModelID: "gpt-4", MessageCount: 2, ParentSessionID: "root", AgentInstanceID: "research#k3v9qp"},
		{ID: "child-2", ModelID: "gpt-4", MessageCount: 1, ParentSessionID: "root", AgentInstanceID: "plan#m2xw01"},
		{ID: "orphan", ModelID: "gpt-4", MessageCount: 1, ParentSessionID: "does-not-exist"},
	}, "")

	lines := strings.Split(out, "\n")
	var rootLine, child1Line, child2Line, orphanLine string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "root") && !strings.Contains(l, "child") && !strings.Contains(l, "orphan"):
			rootLine = l
		case strings.Contains(l, "child-1"):
			child1Line = l
		case strings.Contains(l, "child-2"):
			child2Line = l
		case strings.Contains(l, "orphan"):
			orphanLine = l
		}
	}

	if rootLine == "" || strings.HasPrefix(rootLine, "  ") {
		t.Fatalf("root line should be unindented, got %q", rootLine)
	}
	if !strings.HasPrefix(child1Line, "  └─") {
		t.Fatalf("child-1 line should be indented under its parent, got %q", child1Line)
	}
	if !strings.Contains(child1Line, "agent research#k3v9qp") {
		t.Fatalf("child-1 line missing agent attribution, got %q", child1Line)
	}
	if !strings.Contains(child2Line, "agent plan#m2xw01") {
		t.Fatalf("child-2 line missing agent attribution, got %q", child2Line)
	}
	if strings.HasPrefix(orphanLine, "  ") {
		t.Fatalf("orphan (parent not in page) should render as a root, got %q", orphanLine)
	}

	// child-1 must appear before child-2 (source order preserved) and both
	// after root (tree order, not flat order).
	rootIdx := indexOfSubstring(lines, "root")
	child1Idx := indexOfSubstring(lines, "child-1")
	child2Idx := indexOfSubstring(lines, "child-2")
	if !(rootIdx < child1Idx && child1Idx < child2Idx) {
		t.Fatalf("expected tree order root < child-1 < child-2, got indices %d,%d,%d", rootIdx, child1Idx, child2Idx)
	}
}

func TestFormatDurationCompact(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{500, "500ms"},
		{2500, "2s"},
		{90_000, "1m"},
		{3_900_000, "1h 5m"},
	}
	for _, c := range cases {
		if got := formatDurationCompact(c.ms); got != c.want {
			t.Errorf("formatDurationCompact(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// TestRenderMarkdownAtNarrowWidthStillRenders guards against a normalization
// mismatch: ensureMDRenderer clamps widths below 20 up to 20 before storing
// a renderer, but renderMarkdown used to look the renderer back up under the
// raw, unclamped width - so a narrow terminal (or m.width == 0 before the
// first WindowSizeMsg) always missed the cache and fell back to raw
// markdown.
func TestRenderMarkdownAtNarrowWidthStillRenders(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 10, Height: 24})

	out := m.renderMarkdown("# Heading", m.width)
	plain := stripANSI(out)
	if !strings.Contains(plain, "Heading") {
		t.Fatalf("expected rendered markdown to contain the heading text, got:\n%s", plain)
	}
	if strings.Contains(plain, "# Heading") {
		t.Fatalf("expected the heading to be glamour-rendered at a narrow width, not left as raw markdown:\n%s", plain)
	}
}

func TestAppendMessageMultiLine(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("user", "line one\nline two")

	if len(m.renderedLines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(m.renderedLines))
	}
}

func TestAppendMessageMultiLineUserContinuationInheritsTerminalForeground(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("user", "line one\nline two")

	if len(m.renderedLines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(m.renderedLines))
	}
	// Chat message bodies must not force a truecolor foreground: they
	// should inherit the terminal's own default foreground rather than
	// overriding the user's theme.
	if strings.Contains(m.renderedLines[1], "38;2;") {
		t.Fatalf("continuation line = %q, user message body should not force a foreground color", m.renderedLines[1])
	}
	// It should also not fall back to the generic/tool continuation's dim
	// styling - a user message reads as plain content, not muted metadata.
	if strings.Contains(m.renderedLines[1], "\x1b[2m") {
		t.Fatalf("continuation line = %q, should not use faint/dim styling", m.renderedLines[1])
	}
}

func TestAppendMessageAssistantTracksLastText(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.appendMessage("assistant", "final text")

	if m.lastAssistantText != "final text" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "final text")
	}
}
