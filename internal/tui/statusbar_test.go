package tui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// The core layout/width-pressure algorithm (renderStatusBar/joinSegs) and
// the shared formatters (humanizeTokens/formatCost/contextPct/
// formatContextPct) now live in pkg/taui/statusline.go and are covered
// exhaustively there (narrow widths, empty groups, Unicode, ANSI/OSC
// hyperlinks, undroppable segments, formatting boundaries - see
// pkg/taui/statusline_test.go). What remains here is this frontend's own
// styling glue: that joinSegs/renderStatusBar are wired to termkit-based
// grey by default, and that contextStyle maps severity to termkit colors.

func TestJoinSegs_UsesTermkitGreyDefault(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)

	styled, plain := joinSegs([]statusSeg{{Text: "a"}, {Text: "b"}})
	if plain != "a · b" {
		t.Fatalf("plain = %q, want %q", plain, "a · b")
	}
	// The separator (and any unstyled segment) must carry the grey escape
	// this frontend's statusGrey applies - proving the taui.JoinStatusLineSegs
	// wrapper is actually passing statusGrey through, not silently dropping
	// styling like a bare default(s string) string { return s } would.
	if !strings.Contains(styled, "\x1b[") {
		t.Fatalf("expected ANSI styling from the termkit grey default, got %q", styled)
	}
}

func TestRenderStatusBar_PinsLeftAndJustifiesRight(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{Text: "τ tau", Prio: prioTransient}, {Text: "opus"}}
	right := []statusSeg{{Text: "Open Browser", Prio: prioWeb}}

	const width = 40
	out := renderStatusBar(width, left, right)

	if !strings.HasPrefix(out, "τ tau · opus") {
		t.Fatalf("left group not pinned left: %q", out)
	}
	if !strings.HasSuffix(out, "Open Browser") {
		t.Fatalf("right group not flush-right: %q", out)
	}
}

func TestContextStyle(t *testing.T) {
	if contextStyle(50) != nil {
		t.Error("expected nil (default grey) style below 75%")
	}
	if contextStyle(80) == nil {
		t.Error("expected an amber style at 75-89%")
	}
	if contextStyle(95) == nil {
		t.Error("expected a red style at 90%+")
	}
}

func TestHumanizeTokens(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{942, "942"},
		{999, "999"},
		{1000, "1.0k"},
		{15200, "15.2k"},
		{1_000_000, "1.0M"},
		{1_300_000, "1.3M"},
	}
	for _, tc := range cases {
		if got := humanizeTokens(tc.in); got != tc.want {
			t.Errorf("humanizeTokens(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0.0000"},
		{0.0182, "$0.0182"},
		{0.5, "$0.5000"},
		{1, "$1.00"},
		{1.23, "$1.23"},
		{12.5, "$12.50"},
	}
	for _, tc := range cases {
		if got := formatCost(tc.in); got != tc.want {
			t.Errorf("formatCost(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatContextPct(t *testing.T) {
	cases := []struct {
		prompt int
		window int
		want   string
	}{
		{0, 100, ""},  // no prompt tokens → not computable
		{50, 0, ""},   // no window → not computable
		{-1, 100, ""}, // negative guard
		{41, 100, "41%"},
		{50, 200, "25%"},
		{200000, 200000, "100%"},
	}
	for _, tc := range cases {
		if got := formatContextPct(tc.prompt, tc.window); got != tc.want {
			t.Errorf("formatContextPct(%d,%d) = %q, want %q", tc.prompt, tc.window, got, tc.want)
		}
	}
}
