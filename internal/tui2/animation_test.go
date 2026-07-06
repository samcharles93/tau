package tui2

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- thinkingVerb ---

func TestThinkingVerbStableWithinRotateWindow(t *testing.T) {
	// Within a single verbRotate window the verb must not change, so the word
	// doesn't flicker frame-to-frame while the clock ticks sub-second.
	seed := int64(42)
	a := thinkingVerb(seed, 100*time.Millisecond)
	b := thinkingVerb(seed, verbRotate-time.Millisecond)
	if a != b {
		t.Fatalf("verb changed within one rotate window: %q vs %q", a, b)
	}
}

func TestThinkingVerbAdvancesAcrossWindows(t *testing.T) {
	seed := int64(0)
	first := thinkingVerb(seed, 0)
	next := thinkingVerb(seed, verbRotate)
	if first == next {
		t.Fatalf("verb did not advance after one rotate window (both %q)", first)
	}
}

func TestThinkingVerbSeedVariesOpeningWord(t *testing.T) {
	// Two different seeds should be able to open on different words; check that
	// at least one offset in a small sweep differs (guards against the seed
	// being ignored entirely).
	base := thinkingVerb(0, 0)
	differs := false
	for s := int64(1); s < int64(len(thinkingVerbs)); s++ {
		if thinkingVerb(s, 0) != base {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatal("seed never changed the opening verb")
	}
}

func TestThinkingVerbTotalOnEdgeInputs(t *testing.T) {
	// Negative seed and zero elapsed must index safely, never panic.
	if got := thinkingVerb(-1, 0); got == "" {
		t.Fatal("negative seed produced empty verb")
	}
	if got := thinkingVerb(-999999, 10*time.Minute); got == "" {
		t.Fatal("large elapsed produced empty verb")
	}
}

// --- shimmer ---

func TestShimmerPreservesPlainText(t *testing.T) {
	termkit.ForceColor() // ensure SGR is emitted so stripping is exercised
	in := "⠹ Percolating…"
	out := shimmer(in, 3)
	if got := stripANSI(out); got != in {
		t.Fatalf("shimmer altered visible text: got %q, want %q", got, in)
	}
}

func TestShimmerAnimatesBetweenFrames(t *testing.T) {
	termkit.ForceColor()
	in := "Converging…"
	// Different phases should generally colour the runes differently, so the
	// sweep visibly moves; compare raw (styled) output across a phase step.
	if shimmer(in, 0) == shimmer(in, 4) {
		t.Fatal("shimmer output identical across phases — no visible motion")
	}
}

func TestShimmerEmptyString(t *testing.T) {
	if out := shimmer("", 7); out != "" {
		t.Fatalf("shimmer(\"\") = %q, want empty", out)
	}
}

// --- lerpColor ---

func TestLerpColorEndpoints(t *testing.T) {
	a := termkit.Color{0, 0, 0}
	b := termkit.Color{255, 255, 255}
	if got := lerpColor(a, b, 0); got != a {
		t.Fatalf("t=0 gave %v, want %v", got, a)
	}
	if got := lerpColor(a, b, 1); got != b {
		t.Fatalf("t=1 gave %v, want %v", got, b)
	}
	if got := lerpColor(a, b, 0.5); got != (termkit.Color{128, 128, 128}) {
		t.Fatalf("t=0.5 gave %v, want {128,128,128}", got)
	}
}

func TestLerpColorClampsOutOfRange(t *testing.T) {
	a := termkit.Color{10, 20, 30}
	b := termkit.Color{40, 50, 60}
	if got := lerpColor(a, b, -5); got != a {
		t.Fatalf("t<0 not clamped: %v", got)
	}
	if got := lerpColor(a, b, 5); got != b {
		t.Fatalf("t>1 not clamped: %v", got)
	}
}

// --- formatElapsed ---

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{4 * time.Second, "4s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1:00"},
		{67 * time.Second, "1:07"},
		{125 * time.Second, "2:05"},
		{-3 * time.Second, "0s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// --- toolGlyph ---

func TestToolGlyph(t *testing.T) {
	if g := toolGlyph("done", 0); g != "✓" {
		t.Errorf("done glyph = %q, want ✓", g)
	}
	if g := toolGlyph("error", 0); g != "✗" {
		t.Errorf("error glyph = %q, want ✗", g)
	}
	// Running/pending use the shared spinner frames and must animate.
	run0 := toolGlyph("running", 0)
	run1 := toolGlyph("running", 1)
	if run0 == run1 {
		t.Errorf("running glyph did not advance between frames (%q)", run0)
	}
	if !contains(spinnerFrames, run0) {
		t.Errorf("running glyph %q not a spinner frame", run0)
	}
	if toolGlyph("pending", 0) != toolGlyph("running", 0) {
		t.Error("pending and running should share the spinner glyph")
	}
}

// --- workingIndicator ---

func TestWorkingIndicatorContainsVerbAndClock(t *testing.T) {
	m := &model{
		turnStartedAt: time.Now().Add(-4 * time.Second),
		turnSeed:      1,
		spinnerFrame:  2,
	}
	out := stripANSI(m.workingIndicator())
	if !strings.Contains(out, "4s") {
		t.Errorf("indicator missing elapsed clock: %q", out)
	}
	if !strings.Contains(out, "ctrl+c to interrupt") {
		t.Errorf("indicator missing interrupt hint: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("indicator missing verb ellipsis: %q", out)
	}
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}
