package taui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// sgrSeq matches complete ANSI SGR escape sequences. Stripping these and
// then checking for a stray ESC byte detects a severed/partial escape,
// which is the signature of ANSI-unaware truncation.
var sgrSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

func assertNoSeveredANSI(t *testing.T, out string) {
	t.Helper()
	if rest := sgrSeq.ReplaceAllString(out, ""); strings.ContainsRune(rest, '\x1b') {
		t.Fatalf("severed/partial ANSI escape leaked: %q", out)
	}
}

// plainSegWidth measures a segment group's width the way RenderStatusLine
// does, so tests can derive exact width thresholds instead of hard-coding
// cell counts.
func plainSegWidth(segs []StatusLineSeg) int {
	_, plain := JoinStatusLineSegs(segs, nil)
	return uniseg.StringWidth(plain)
}

// ── JoinStatusLineSegs ──────────────────────────────────────────────────────

func TestJoinStatusLineSegs_Empty(t *testing.T) {
	styled, plain := JoinStatusLineSegs(nil, nil)
	if styled != "" || plain != "" {
		t.Fatalf("expected empty output for empty input, got styled=%q plain=%q", styled, plain)
	}
}

func TestJoinStatusLineSegs_Single(t *testing.T) {
	styled, plain := JoinStatusLineSegs([]StatusLineSeg{{Text: "hello"}}, nil)
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
	if styled != "hello" {
		t.Fatalf("styled = %q, want %q (nil defaultStyle should be a no-op)", styled, "hello")
	}
}

func TestJoinStatusLineSegs_Multiple(t *testing.T) {
	_, plain := JoinStatusLineSegs([]StatusLineSeg{{Text: "a"}, {Text: "b"}, {Text: "c"}}, nil)
	if plain != "a · b · c" {
		t.Fatalf("plain = %q, want %q", plain, "a · b · c")
	}
}

func TestJoinStatusLineSegs_SkipsEmpty(t *testing.T) {
	_, plain := JoinStatusLineSegs([]StatusLineSeg{{Text: "a"}, {Text: ""}, {Text: "b"}}, nil)
	if plain != "a · b" {
		t.Fatalf("plain = %q, want %q", plain, "a · b")
	}
}

func TestJoinStatusLineSegs_CustomStyle(t *testing.T) {
	styler := func(s string) string { return "<" + s + ">" }
	styled, plain := JoinStatusLineSegs([]StatusLineSeg{{Text: "bold", Style: styler}}, nil)
	if plain != "bold" {
		t.Fatalf("plain = %q, want %q", plain, "bold")
	}
	if styled != "<bold>" {
		t.Fatalf("styled = %q, want %q", styled, "<bold>")
	}
}

func TestJoinStatusLineSegs_DefaultStyleAppliesToUnstyledSegsAndSeparators(t *testing.T) {
	wrap := func(s string) string { return "[" + s + "]" }
	styled, _ := JoinStatusLineSegs([]StatusLineSeg{{Text: "a"}, {Text: "b"}}, wrap)
	want := "[a][ · ][b]"
	if styled != want {
		t.Fatalf("styled = %q, want %q", styled, want)
	}
}

func TestJoinStatusLineSegs_StyledOverrideBypassesStyle(t *testing.T) {
	styled, plain := JoinStatusLineSegs([]StatusLineSeg{{
		Text:           "web",
		StyledOverride: "\x1b]8;;http://example.test\x1b\\web\x1b]8;;\x1b\\",
		Style:          func(s string) string { t.Fatal("Style should not be called when StyledOverride is set"); return s },
	}}, nil)
	if plain != "web" {
		t.Fatalf("plain = %q, want %q (width math always uses Text)", plain, "web")
	}
	if !strings.Contains(styled, "\x1b]8;;http://example.test") {
		t.Fatalf("styled = %q, want the raw StyledOverride escape sequence", styled)
	}
}

// ── RenderStatusLine ────────────────────────────────────────────────────────

func TestRenderStatusLine_RightJustified(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau"}, {Text: "opus"}}
	right := []StatusLineSeg{{Text: "Open Browser", Prio: 1}}

	const width = 40
	out := RenderStatusLine(width, left, right, nil)

	if w := uniseg.StringWidth(out); w != width {
		t.Fatalf("expected exact width %d, got %d (%q)", width, w, out)
	}
	if !strings.HasPrefix(out, "τ tau · opus") {
		t.Fatalf("left group not pinned left: %q", out)
	}
	if !strings.HasSuffix(out, "Open Browser") {
		t.Fatalf("right group not flush-right: %q", out)
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(out, "τ tau · opus"), "Open Browser")
	if mid == "" || strings.TrimSpace(mid) != "" {
		t.Fatalf("expected a whitespace-only gap between groups, got %q", out)
	}
}

func TestRenderStatusLine_DropOrder(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}}
	leftW := plainSegWidth(left)

	tokens := StatusLineSeg{Text: "↑12.3k ↓4.5k", Prio: 3}
	cost := StatusLineSeg{Text: "$0.0182", Prio: 2}
	ctx := StatusLineSeg{Text: "ctx 41%", Prio: 4}
	web := StatusLineSeg{Text: "Open Browser", Prio: 1}
	right := []StatusLineSeg{tokens, cost, ctx, web}

	cases := []struct {
		name    string
		keep    []StatusLineSeg
		present []string
		absent  []string
	}{
		{"drops web first", []StatusLineSeg{tokens, cost, ctx}, []string{"↑12.3k ↓4.5k", "$0.0182", "ctx 41%"}, []string{"Open Browser"}},
		{"then drops cost", []StatusLineSeg{tokens, ctx}, []string{"↑12.3k ↓4.5k", "ctx 41%"}, []string{"Open Browser", "$0.0182"}},
		{"then drops tokens, highest prio survives", []StatusLineSeg{ctx}, []string{"ctx 41%"}, []string{"Open Browser", "$0.0182", "↑12.3k"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			width := leftW + 1 + plainSegWidth(tc.keep)
			out := RenderStatusLine(width, left, right, nil)

			if w := uniseg.StringWidth(out); w > width {
				t.Fatalf("output width %d exceeds budget %d (%q)", w, width, out)
			}
			for _, p := range tc.present {
				if !strings.Contains(out, p) {
					t.Errorf("expected %q present, got %q", p, out)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(out, a) {
					t.Errorf("expected %q dropped, got %q", a, out)
				}
			}
		})
	}
}

func TestRenderStatusLine_TransientNeverDropped(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}}
	right := []StatusLineSeg{
		{Text: "[STEERING...]", Prio: StatusLinePrioTransient},
		{Text: "$0.0182", Prio: 2},
		{Text: "Open Browser", Prio: 1},
	}

	const width = 20
	out := RenderStatusLine(width, left, right, nil)

	if w := uniseg.StringWidth(out); w > width {
		t.Fatalf("output width %d exceeds budget %d (%q)", w, width, out)
	}
	if !strings.Contains(out, "[STEERING...]") {
		t.Fatalf("transient segment was dropped: %q", out)
	}
}

// TestRenderStatusLine_BothSidesDropUnderPressure verifies the width-pressure
// loop drops low-priority segments from BOTH the left and right groups (not
// just the right), which is the behavior internal/tui2 had already evolved
// independently of internal/tui before this package unified them (the
// "parity drift" this refactor resolves).
func TestRenderStatusLine_BothSidesDropUnderPressure(t *testing.T) {
	left := []StatusLineSeg{
		{Text: "τ", Prio: StatusLinePrioTransient},
		{Text: "a-very-long-model-name-that-overflows", Prio: 0},
	}
	right := []StatusLineSeg{{Text: "[STEERING...]", Prio: StatusLinePrioTransient}}

	const width = 20
	out := RenderStatusLine(width, left, right, nil)

	if w := uniseg.StringWidth(out); w > width {
		t.Fatalf("output width %d exceeds budget %d (%q)", w, width, out)
	}
	if strings.Contains(out, "a-very-long-model-name-that-overflows") {
		t.Fatalf("expected the low-priority left segment to be dropped whole, got %q", out)
	}
	if !strings.Contains(out, "τ") {
		t.Fatalf("expected the transient left segment to survive, got %q", out)
	}
	if !strings.Contains(out, "[STEERING...]") {
		t.Fatalf("transient right segment was dropped: %q", out)
	}
}

func TestRenderStatusLine_LeftTruncatesWhenTransientWins(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}, {Text: "a-very-long-model-name-that-overflows", Prio: StatusLinePrioTransient}}
	right := []StatusLineSeg{{Text: "[STEERING...]", Prio: StatusLinePrioTransient}}

	const width = 20
	out := RenderStatusLine(width, left, right, nil)

	if w := uniseg.StringWidth(out); w > width {
		t.Fatalf("output width %d exceeds budget %d (%q)", w, width, out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("expected left group to be truncated with ellipsis: %q", out)
	}
	if !strings.Contains(out, "[STEERING...]") {
		t.Fatalf("transient segment was dropped: %q", out)
	}
}

// TestRenderStatusLine_TruncationIsANSIAware exercises the two truncation
// paths (no-right left overflow, and transient-wins left truncation) under
// color, and asserts the styled output is never corrupted by an
// ANSI-unaware cut.
func TestRenderStatusLine_TruncationIsANSIAware(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)
	grey := func(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) }

	cases := []struct {
		name  string
		left  []StatusLineSeg
		right []StatusLineSeg
		width int
	}{
		{
			name:  "no right group, left overflows",
			left:  []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}, {Text: "a-very-long-model-name-that-overflows", Prio: StatusLinePrioTransient}},
			width: 20,
		},
		{
			name:  "transient wins, left truncated",
			left:  []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}, {Text: "a-very-long-model-name-that-overflows", Prio: StatusLinePrioTransient}},
			right: []StatusLineSeg{{Text: "[STEERING...]", Prio: StatusLinePrioTransient}},
			width: 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderStatusLine(tc.width, tc.left, tc.right, grey)

			if w := VisibleWidth(out); w > tc.width {
				t.Fatalf("visible width %d exceeds budget %d (%q)", w, tc.width, out)
			}
			assertNoSeveredANSI(t, out)

			vis := sgrSeq.ReplaceAllString(out, "")
			if !strings.HasPrefix(vis, "τ tau") {
				t.Fatalf("expected visible identity preserved, got %q (raw %q)", vis, out)
			}
			if !strings.Contains(vis, "…") {
				t.Fatalf("expected truncation ellipsis, got %q (raw %q)", vis, out)
			}
			if tc.right != nil && !strings.Contains(vis, "[STEERING...]") {
				t.Fatalf("transient segment was dropped: %q", vis)
			}
		})
	}
}

func TestRenderStatusLine_IdleNoRight(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau"}, {Text: "opus"}, {Text: "anthropic"}}
	out := RenderStatusLine(78, left, nil, nil)

	if out != "τ tau · opus · anthropic" {
		t.Fatalf("idle line should be the minimalist join with no trailing pad, got %q", out)
	}
}

func TestRenderStatusLine_Empty(t *testing.T) {
	out := RenderStatusLine(80, nil, nil, nil)
	if out == "" {
		t.Fatal("expected non-empty output (space padding) even for empty segments")
	}
}

func TestRenderStatusLine_MinWidth(t *testing.T) {
	out := RenderStatusLine(0, []StatusLineSeg{{Text: "hello"}}, nil, nil)
	if out == "" {
		t.Fatal("expected output even with 0 width")
	}
}

func TestRenderStatusLine_NeverExceedsBudget(t *testing.T) {
	left := []StatusLineSeg{{Text: "τ tau", Prio: StatusLinePrioTransient}, {Text: "opus-4.8"}, {Text: "anthropic"}, {Text: "med"}}
	right := []StatusLineSeg{
		{Text: "[STEERING...]", Prio: StatusLinePrioTransient},
		{Text: "↑12.3k ↓4.5k", Prio: 3},
		{Text: "$0.0182", Prio: 2},
		{Text: "ctx 41%", Prio: 4},
		{Text: "Open Browser", Prio: 1},
	}
	for width := 1; width <= 120; width++ {
		out := RenderStatusLine(width, left, right, nil)
		if w := uniseg.StringWidth(out); w > width {
			t.Fatalf("width %d: output %d exceeds budget (%q)", width, w, out)
		}
	}
}

// ── Formatters ──────────────────────────────────────────────────────────────

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
		if got := HumanizeTokens(tc.in); got != tc.want {
			t.Errorf("HumanizeTokens(%d) = %q, want %q", tc.in, got, tc.want)
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
		if got := FormatCost(tc.in); got != tc.want {
			t.Errorf("FormatCost(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestContextPct(t *testing.T) {
	cases := []struct {
		prompt, window, want int
	}{
		{0, 8192, -1},
		{100, 0, -1},
		{4096, 8192, 50},
		{7373, 8192, 90},
		{8192, 8192, 100},
		{1000, 1000, 100},
	}
	for _, tc := range cases {
		if got := ContextPct(tc.prompt, tc.window); got != tc.want {
			t.Errorf("ContextPct(%d, %d) = %d, want %d", tc.prompt, tc.window, got, tc.want)
		}
	}
}

func TestFormatContextPct(t *testing.T) {
	cases := []struct {
		prompt int
		window int
		want   string
	}{
		{0, 100, ""},
		{50, 0, ""},
		{-1, 100, ""},
		{41, 100, "41%"},
		{50, 200, "25%"},
		{200000, 200000, "100%"},
	}
	for _, tc := range cases {
		if got := FormatContextPct(tc.prompt, tc.window); got != tc.want {
			t.Errorf("FormatContextPct(%d,%d) = %q, want %q", tc.prompt, tc.window, got, tc.want)
		}
	}
}

func TestContextSeverityFor(t *testing.T) {
	cases := []struct {
		pct  int
		want ContextSeverity
	}{
		{0, ContextNormal},
		{50, ContextNormal},
		{74, ContextNormal},
		{75, ContextWarn},
		{89, ContextWarn},
		{90, ContextCritical},
		{100, ContextCritical},
	}
	for _, tc := range cases {
		if got := ContextSeverityFor(tc.pct); got != tc.want {
			t.Errorf("ContextSeverityFor(%d) = %v, want %v", tc.pct, got, tc.want)
		}
	}
}
