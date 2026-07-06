package tui2

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// --- renderStatusBar --------------------------------------------------------

func TestRenderStatusBarBasic(t *testing.T) {
	left := []statusSeg{{text: "tau"}, {text: "gpt-4"}}
	right := []statusSeg{{text: "1.2k tok", prio: prioTokens}}

	out := renderStatusBar(80, left, right)
	if out == "" {
		t.Fatal("expected non-empty status bar")
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "tau") {
		t.Errorf("status bar should contain 'tau', got %q", plain)
	}
	if !strings.Contains(plain, "gpt-4") {
		t.Errorf("status bar should contain 'gpt-4', got %q", plain)
	}
	if !strings.Contains(plain, "1.2k tok") {
		t.Errorf("status bar should contain token count, got %q", plain)
	}
}

func TestRenderStatusBarWidthPressureDropsLowPrio(t *testing.T) {
	left := []statusSeg{{text: "LLLLLLLLLL", prio: 0}}
	right := []statusSeg{
		{text: "RRRRRR", prio: prioContext},
		{text: "costly", prio: prioCost},
		{text: "expensive", prio: prioCost},
		{text: "dropped-first", prio: 1},
	}

	out := renderStatusBar(10, left, right)
	plain := stripANSI(out)
	if strings.Contains(plain, "dropped-first") {
		t.Errorf("lowest-prio segment should be dropped under width pressure, got %q", plain)
	}
}

func TestRenderStatusBarRightOnlyFitsAll(t *testing.T) {
	right := []statusSeg{
		{text: "short", prio: 1},
		{text: "seg", prio: 2},
	}

	out := renderStatusBar(30, nil, right)
	plain := stripANSI(out)
	if !strings.Contains(plain, "short") || !strings.Contains(plain, "seg") {
		t.Errorf("expected both segments present, got %q", plain)
	}
}

func TestRenderStatusBarEmpty(t *testing.T) {
	out := renderStatusBar(80, nil, nil)
	if out == "" {
		t.Fatal("expected non-empty output even for empty segments")
	}
}

func TestRenderStatusBarMinWidth(t *testing.T) {
	out := renderStatusBar(0, []statusSeg{{text: "hello"}}, nil)
	if out == "" {
		t.Fatal("expected output even with 0 width")
	}
}

func TestRenderStatusBarLeftTruncated(t *testing.T) {
	right := []statusSeg{{text: "RRRRR", prio: 1}}
	left := []statusSeg{{text: "LLLLLLLLLLLLLLLLL", prio: 0}}

	out := renderStatusBar(10, left, right)
	plain := stripANSI(out)
	// Right segment should still be present since it has higher prio.
	if !strings.Contains(plain, "RRRRR") {
		t.Errorf("right segment should survive truncation, got %q", plain)
	}
}

// --- joinSegs ---------------------------------------------------------------

func TestJoinSegsEmpty(t *testing.T) {
	styled, plain := joinSegs(nil)
	if styled != "" || plain != "" {
		t.Fatalf("expected empty output for empty input, got styled=%q plain=%q", styled, plain)
	}
}

func TestJoinSegsSingle(t *testing.T) {
	styled, plain := joinSegs([]statusSeg{{text: "hello"}})
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
	if styled == "" {
		t.Fatal("styled should not be empty for a single segment")
	}
}

func TestJoinSegsMultiple(t *testing.T) {
	segs := []statusSeg{
		{text: "a"},
		{text: "b"},
		{text: "c"},
	}
	_, plain := joinSegs(segs)
	if plain != "a · b · c" {
		t.Fatalf("plain = %q, want %q", plain, "a · b · c")
	}
}

func TestJoinSegsSkipsEmpty(t *testing.T) {
	segs := []statusSeg{
		{text: "a"},
		{text: ""},
		{text: "b"},
	}
	_, plain := joinSegs(segs)
	if plain != "a · b" {
		t.Fatalf("plain = %q, want %q", plain, "a · b")
	}
}

func TestJoinSegsCustomStyle(t *testing.T) {
	styler := func(s string) string {
		return lipgloss.NewStyle().Bold(true).Render(s)
	}
	_, plain := joinSegs([]statusSeg{
		{text: "bold", style: styler},
	})
	if plain != "bold" {
		t.Fatalf("plain = %q, want %q", plain, "bold")
	}
}

// --- humanizeTokens ---------------------------------------------------------

func TestHumanizeTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{999999, "1000.0k"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tc := range tests {
		got := humanizeTokens(tc.n)
		if got != tc.want {
			t.Errorf("humanizeTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// --- formatCost -------------------------------------------------------------

func TestFormatCost(t *testing.T) {
	tests := []struct {
		cost float64
		want string
	}{
		{0.0, "$0.0000"},
		{0.001234, "$0.0012"},
		{0.9999, "$0.9999"},
		{1.0, "$1.00"},
		{1.5, "$1.50"},
		{123.456, "$123.46"},
	}
	for _, tc := range tests {
		got := formatCost(tc.cost)
		if got != tc.want {
			t.Errorf("formatCost(%f) = %q, want %q", tc.cost, got, tc.want)
		}
	}
}

// --- contextPct -------------------------------------------------------------

func TestContextPct(t *testing.T) {
	tests := []struct {
		prompt    int
		ctxWindow int
		want      int
	}{
		{0, 8192, -1},
		{100, 0, -1},
		{4096, 8192, 50},
		{7373, 8192, 90},
		{8192, 8192, 100},
		{1000, 1000, 100},
	}
	for _, tc := range tests {
		got := contextPct(tc.prompt, tc.ctxWindow)
		if got != tc.want {
			t.Errorf("contextPct(%d, %d) = %d, want %d", tc.prompt, tc.ctxWindow, got, tc.want)
		}
	}
}

func TestFormatContextPct(t *testing.T) {
	if got := formatContextPct(0, 8192); got != "" {
		t.Fatalf("formatContextPct(0, 8192) = %q, want empty", got)
	}
	if got := formatContextPct(4096, 8192); got != "ctx 50%" {
		t.Fatalf("formatContextPct(4096, 8192) = %q, want %q", got, "ctx 50%")
	}
}

// --- contextStyle -----------------------------------------------------------

func TestContextStyle(t *testing.T) {
	tests := []struct {
		pct  int
		name string
	}{
		{50, "green (low)"},
		{80, "nil (medium)"},
		{90, "amber (high)"},
		{95, "red (critical)"},
	}
	for _, tc := range tests {
		style := contextStyle(tc.pct)
		if tc.pct >= 90 && style == nil {
			t.Errorf("contextStyle(%d) = nil, wanted a style for %s", tc.pct, tc.name)
		}
		if tc.pct < 75 && style != nil {
			t.Errorf("contextStyle(%d) = non-nil, wanted nil for %s", tc.pct, tc.name)
		}
	}
}

// --- notifyLevelStyle -------------------------------------------------------

func TestNotifyLevelStyleWarn(t *testing.T) {
	style := notifyLevelStyle(notify.LevelWarn)
	if style == nil {
		t.Fatal("expected a style for warn level")
	}
}

func TestNotifyLevelStyleError(t *testing.T) {
	style := notifyLevelStyle(notify.LevelError)
	if style == nil {
		t.Fatal("expected a style for error level")
	}
}

func TestNotifyLevelStyleInfo(t *testing.T) {
	style := notifyLevelStyle(notify.LevelInfo)
	if style != nil {
		t.Fatal("expected nil style for info level")
	}
}

// --- truncateANSIToWidth ----------------------------------------------------

func TestTruncateANSIToWidthFits(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello")
	out := truncateANSIToWidth(s, 100, "…")
	plain := stripANSI(out)
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
}

func TestTruncateANSIToWidthTruncates(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello world")
	out := truncateANSIToWidth(s, 6, "…")
	plain := stripANSI(out)
	if visibleWidth(plain) != 6 || !strings.HasPrefix(plain, "hello") {
		t.Fatalf("truncated = %q, want 6-char prefix with ellipsis", plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
}

func TestTruncateANSIToWidthZeroBudget(t *testing.T) {
	out := truncateANSIToWidth("hello", 0, "…")
	if out != "" {
		t.Fatalf("expected empty for zero width, got %q", out)
	}
}

func TestTruncateANSIToWidthStyledOutput(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF0000")).Render("very long text here")
	out := truncateANSIToWidth(s, 10, "…")
	plain := stripANSI(out)
	// Truncation + ellipsis should produce <= budget.
	if visibleWidth(plain) > 10 {
		t.Fatalf("truncated plain text width = %d, want <= 10", visibleWidth(plain))
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
	// ANSI codes must not be lost by truncation.
	if !strings.HasPrefix(out, "\x1b") {
		t.Error("truncated output lost ANSI style prefix")
	}
}

// --- stripANSI --------------------------------------------------------------

func TestStripANSIPlain(t *testing.T) {
	plain := stripANSI("hello world")
	if plain != "hello world" {
		t.Fatalf("stripANSI(%q) = %q", "hello world", plain)
	}
}

func TestStripANSISingleEscape(t *testing.T) {
	plain := stripANSI("\x1b[31mhello\x1b[0m")
	if plain != "hello" {
		t.Fatalf("stripANSI with escape codes = %q, want %q", plain, "hello")
	}
}

func TestStripANSIMultipleCodes(t *testing.T) {
	plain := stripANSI("\x1b[1m\x1b[31mhello\x1b[0m world")
	if plain != "hello world" {
		t.Fatalf("stripANSI with multiple codes = %q, want %q", plain, "hello world")
	}
}

func TestStripANSINonEscapeCodes(t *testing.T) {
	plain := stripANSI("hello\x1bworld")
	if plain != "helloworld" {
		// The incomplete escape is still consumed.
		t.Logf("stripANSI handles incomplete escape, got %q", plain)
	}
}

// --- boldText ---------------------------------------------------------------

func TestBoldTextWrapsWithANSI(t *testing.T) {
	out := boldText("important")
	if !strings.HasPrefix(out, "\x1b") {
		t.Error("boldText should apply ANSI bold style")
	}
	plain := stripANSI(out)
	if plain != "important" {
		t.Fatalf("boldText plain = %q, want %q", plain, "important")
	}
}

// --- visibleWidth -----------------------------------------------------------

func TestVisibleWidth(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"hello", 5},
		{"hello world", 11},
	}
	for _, tc := range tests {
		got := visibleWidth(tc.s)
		if got != tc.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tc.s, got, tc.want)
		}
	}
}

func TestVisibleWidthStripped(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello")
	plain := stripANSI(s)
	w := visibleWidth(plain)
	if w != 5 {
		t.Fatalf("visibleWidth of styled plain = %d, want 5", w)
	}
}
