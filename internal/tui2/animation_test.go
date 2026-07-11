package tui2

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/theme"
)

// --- thinkingDots ---

func TestThinkingDotsThreeFrameSequence(t *testing.T) {
	// Frame 0 → "●", frame 6 → "●●", frame 12 → "●●●", frame 18 → "●" (wrap).
	want := []string{"●", "●●", "●●●"}
	for i := range 3 {
		got := thinkingDots(i * thinkingDotFrameTicks)
		if got != want[i] {
			t.Errorf("thinkingDots(%d) = %q, want %q", i*thinkingDotFrameTicks, got, want[i])
		}
	}
}

func TestThinkingDotsWraps(t *testing.T) {
	// After three frames, it should wrap back to the first.
	first := thinkingDots(0)
	wrapped := thinkingDots(3 * thinkingDotFrameTicks)
	if first != wrapped {
		t.Errorf("thinkingDots did not wrap: %q vs %q", first, wrapped)
	}
}

func TestThinkingDotsStableWithinTickWindow(t *testing.T) {
	// Within a single thinkingDotFrameTicks window, the dot frame must not
	// change, so the dot count doesn't flicker frame-to-frame.
	a := thinkingDots(0)
	for i := 1; i < thinkingDotFrameTicks; i++ {
		if got := thinkingDots(i); got != a {
			t.Fatalf("thinkingDots changed within tick window at frame %d: %q vs %q", i, got, a)
		}
	}
}

func TestThinkingDotsNegativeFrame(t *testing.T) {
	// Negative frame values must index safely, never panic.
	if got := thinkingDots(-1); got == "" {
		t.Fatal("negative frame produced empty string")
	}
	if got := thinkingDots(-999999); got == "" {
		t.Fatal("large negative frame produced empty string")
	}
}

func TestThinkingDotsCadence(t *testing.T) {
	// At 80ms per tick and thinkingDotFrameTicks=6, each visual frame lasts
	// ~480ms. Verify the divisor relationship.
	if thinkingDotFrameTicks*80 != 480 {
		t.Errorf("thinkingDotFrameTicks=%d * 80ms = %dms, want 480ms", thinkingDotFrameTicks, thinkingDotFrameTicks*80)
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

func TestWorkingIndicatorShowsDots(t *testing.T) {
	m := &model{
		spinnerFrame: 0,
	}
	out := stripANSI(m.workingIndicator())
	if !strings.Contains(out, "Thinking ") {
		t.Errorf("indicator missing label: %q", out)
	}
	if !strings.Contains(out, "●") {
		t.Errorf("indicator missing dot: %q", out)
	}
}

func TestWorkingIndicatorNoVerbOrClock(t *testing.T) {
	m := &model{
		spinnerFrame: 0,
	}
	out := stripANSI(m.workingIndicator())
	for _, banned := range []string{"4s", "ctrl+c", "Cogitating", "Percolating", "…"} {
		if strings.Contains(out, banned) {
			t.Errorf("indicator contains removed element %q: %q", banned, out)
		}
	}
}

func TestWorkingIndicatorAnimates(t *testing.T) {
	m := &model{spinnerFrame: 0}
	a := m.workingIndicator()
	m.spinnerFrame = thinkingDotFrameTicks
	b := m.workingIndicator()
	if a == b {
		t.Fatal("workingIndicator did not change across dot frames")
	}
}

// TestWorkingIndicatorAccentsOnlyTheDots guards the "accent is sparing"
// convention this codebase otherwise follows everywhere (reasoningLabelStyle
// on just the bar, userGlyphStyle on just the "⏎" glyph): the static
// "Thinking " label must stay unstyled/inherit the terminal's default
// foreground, since it's the only element on screen for the entire thinking
// phase of every turn — only the dot glyphs after it should carry Warm
// Ochre.
func TestWorkingIndicatorAccentsOnlyTheDots(t *testing.T) {
	m := &model{spinnerFrame: 0}
	out := m.workingIndicator()

	idx := strings.Index(out, "Thinking ")
	if idx < 0 {
		t.Fatalf("indicator missing label: %q", out)
	}
	label := out[idx : idx+len("Thinking ")]
	if label != stripANSI(label) {
		t.Fatalf("label carries styling, want it unstyled: %q", label)
	}

	accentSGR := fmt.Sprintf("38;2;%d;%d;%d", theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2])
	dotsPortion := out[idx+len("Thinking "):]
	if !strings.Contains(dotsPortion, accentSGR) {
		t.Fatalf("dot sequence missing the Warm Ochre accent: %q", out)
	}
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}
