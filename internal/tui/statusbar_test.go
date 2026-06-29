package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// sgrSeq matches complete ANSI SGR escape sequences (the only kind the status
// bar emits). Stripping these and then checking for a stray ESC byte detects a
// severed/partial escape, which is the signature of ANSI-unaware truncation.
var sgrSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// assertNoSeveredANSI fails if out contains an ESC byte that is not part of a
// complete SGR sequence (i.e. a truncation cut through an escape).
func assertNoSeveredANSI(t *testing.T, out string) {
	t.Helper()
	if rest := sgrSeq.ReplaceAllString(out, ""); strings.ContainsRune(rest, '\x1b') {
		t.Fatalf("severed/partial ANSI escape leaked: %q", out)
	}
}

// plainWidth measures a right-group subset the way renderStatusBar does, so tests
// can derive exact width thresholds instead of hard-coding cell counts.
func plainWidth(segs []statusSeg) int {
	_, plain := joinSegs(segs)
	return uniseg.StringWidth(plain)
}

func TestRenderStatusBar_RightJustified(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}, {text: "opus"}}
	right := []statusSeg{{text: "web: :7777", prio: prioWeb}}

	const width = 40
	out := renderStatusBar(width, left, right)

	if w := uniseg.StringWidth(out); w != width {
		t.Fatalf("expected exact width %d, got %d (%q)", width, w, out)
	}
	if !strings.HasPrefix(out, "τ tau · opus") {
		t.Fatalf("left group not pinned left: %q", out)
	}
	if !strings.HasSuffix(out, "web: :7777") {
		t.Fatalf("right group not flush-right: %q", out)
	}
	// The gap between the two groups must be >= 1 column of spaces.
	mid := strings.TrimSuffix(strings.TrimPrefix(out, "τ tau · opus"), "web: :7777")
	if mid == "" || strings.TrimSpace(mid) != "" {
		t.Fatalf("expected a whitespace-only gap between groups, got %q", out)
	}
}

func TestRenderStatusBar_DropOrder(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}}
	leftW := plainWidth(left)

	tokens := statusSeg{text: "↑12.3k ↓4.5k", prio: prioTokens}
	cost := statusSeg{text: "$0.0182", prio: prioCost}
	ctx := statusSeg{text: "ctx 41%", prio: prioContext}
	web := statusSeg{text: "web: :7777", prio: prioWeb}
	right := []statusSeg{tokens, cost, ctx, web} // visual order

	cases := []struct {
		name    string
		keep    []statusSeg // right subset that should exactly fit
		present []string
		absent  []string
	}{
		{
			name:    "drops web first",
			keep:    []statusSeg{tokens, cost, ctx},
			present: []string{"↑12.3k ↓4.5k", "$0.0182", "ctx 41%"},
			absent:  []string{"web:"},
		},
		{
			name:    "then drops cost",
			keep:    []statusSeg{tokens, ctx},
			present: []string{"↑12.3k ↓4.5k", "ctx 41%"},
			absent:  []string{"web:", "$0.0182"},
		},
		{
			name:    "then drops tokens, context survives",
			keep:    []statusSeg{ctx},
			present: []string{"ctx 41%"},
			absent:  []string{"web:", "$0.0182", "↑12.3k"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			width := leftW + 1 + plainWidth(tc.keep)
			out := renderStatusBar(width, left, right)

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

func TestRenderStatusBar_TransientNeverDropped(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}}
	right := []statusSeg{
		{text: "[STEERING...]", prio: prioTransient},
		{text: "$0.0182", prio: prioCost},
		{text: "web: :7777", prio: prioWeb},
	}

	const width = 20
	out := renderStatusBar(width, left, right)

	if w := uniseg.StringWidth(out); w > width {
		t.Fatalf("output width %d exceeds budget %d (%q)", w, width, out)
	}
	if !strings.Contains(out, "[STEERING...]") {
		t.Fatalf("transient segment was dropped: %q", out)
	}
}

func TestRenderStatusBar_LeftTruncatesWhenTransientWins(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}, {text: "a-very-long-model-name-that-overflows"}}
	right := []statusSeg{{text: "[STEERING...]", prio: prioTransient}}

	const width = 20
	out := renderStatusBar(width, left, right)

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

// TestRenderStatusBar_TruncationIsANSIAware exercises the two truncation paths
// (no-right left overflow, and transient-wins left truncation) under colour, and
// asserts the styled output is never corrupted by an ANSI-unaware cut. The rest
// of the suite disables colour, which masks this class of bug.
func TestRenderStatusBar_TruncationIsANSIAware(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)

	cases := []struct {
		name  string
		left  []statusSeg
		right []statusSeg
		width int
	}{
		{
			name:  "no right group, left overflows",
			left:  []statusSeg{{text: "τ tau"}, {text: "a-very-long-model-name-that-overflows"}},
			width: 20,
		},
		{
			name:  "transient wins, left truncated",
			left:  []statusSeg{{text: "τ tau"}, {text: "a-very-long-model-name-that-overflows"}},
			right: []statusSeg{{text: "[STEERING...]", prio: prioTransient}},
			width: 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderStatusBar(tc.width, tc.left, tc.right)

			if w := taui.VisibleWidth(out); w > tc.width {
				t.Fatalf("visible width %d exceeds budget %d (%q)", w, tc.width, out)
			}
			assertNoSeveredANSI(t, out)

			// Visible identity must survive (the buggy cut dropped it entirely),
			// and the truncated left group must carry an ellipsis.
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

func TestRenderStatusBar_IdleNoRight(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}, {text: "opus"}, {text: "anthropic"}}
	out := renderStatusBar(78, left, nil)

	if out != "τ tau · opus · anthropic" {
		t.Fatalf("idle line should be the minimalist join with no trailing pad, got %q", out)
	}
}

func TestRenderStatusBar_NeverExceedsBudget(t *testing.T) {
	termkit.DisableColor()

	left := []statusSeg{{text: "τ tau"}, {text: "opus-4.8"}, {text: "anthropic"}, {text: "med"}}
	right := []statusSeg{
		{text: "[STEERING...]", prio: prioTransient},
		{text: "↑12.3k ↓4.5k", prio: prioTokens},
		{text: "$0.0182", prio: prioCost},
		{text: "ctx 41%", prio: prioContext},
		{text: "web: :7777", prio: prioWeb},
	}
	for width := 1; width <= 120; width++ {
		out := renderStatusBar(width, left, right)
		if w := uniseg.StringWidth(out); w > width {
			t.Fatalf("width %d: output %d exceeds budget (%q)", width, w, out)
		}
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
		{41, 100, "ctx 41%"},
		{50, 200, "ctx 25%"},
		{200000, 200000, "ctx 100%"},
	}
	for _, tc := range cases {
		if got := formatContextPct(tc.prompt, tc.window); got != tc.want {
			t.Errorf("formatContextPct(%d,%d) = %q, want %q", tc.prompt, tc.window, got, tc.want)
		}
	}
}
