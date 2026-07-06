package tui2

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// This file holds the "living" presentation touches that keep tui2 from
// feeling robotic while a turn is in flight: a shimmer sweep across the
// working indicator, a rotating personality verb, a live elapsed clock, and
// the per-state glyphs for tool rows. All of it is driven off the existing
// 80ms spinner tick (see model.spinnerFrame) — no goroutines, no extra
// timers — so it composes through Bubbletea's normal Update/View flow.

// thinkingVerbs are the rotating status words shown while the model warms up,
// before any text, reasoning, or tool call is visible. The mix is deliberate:
// cerebral verbs promise the work is real, culinary/whimsical ones refuse to
// take the wait too seriously, and a handful of tau-native maths verbs
// (Circling, Converging, Unwinding, Winding, Radianing) tie the personality
// back to τ, the circle constant. Kept alphabetical-ish within families for
// easy scanning when editing; order is not significant since selection cycles.
var thinkingVerbs = []string{
	// Cerebral — owning up to the thinking.
	"Cogitating", "Contemplating", "Deliberating", "Discerning",
	"Inferring", "Musing", "Pondering", "Reasoning", "Ruminating",
	// Culinary — patience reframed as cooking.
	"Brewing", "Marinating", "Percolating", "Simmering", "Steeping", "Whisking",
	// Whimsical — confident enough to be silly.
	"Beavering", "Doodling", "Noodling", "Puttering", "Tinkering", "Wibbling",
	// Scientific — computation as a physical process.
	"Crystallising", "Distilling", "Nucleating", "Precipitating", "Synthesising",
	// tau-native — the circle constant at play.
	"Circling", "Converging", "Radianing", "Unwinding", "Winding",
}

// verbRotate is how long each verb stays on screen before the next is shown.
const verbRotate = 3 * time.Second

// thinkingVerb picks a verb deterministically from (seed, elapsed): the seed
// varies the starting verb per turn so two consecutive turns don't open on the
// same word, and elapsed advances through the list every verbRotate so a long
// wait cycles rather than freezing. Pure and total — safe to unit-test and
// never panics for an empty elapsed or negative seed.
func thinkingVerb(seed int64, elapsed time.Duration) string {
	n := int64(len(thinkingVerbs))
	if n == 0 {
		return ""
	}
	steps := seed + int64(elapsed/verbRotate)
	return thinkingVerbs[((steps%n)+n)%n]
}

// shimmerBand is the half-width, in cells, of the travelling highlight. A
// wider band spreads the light over more characters (softer); narrower makes a
// tighter, faster-reading pulse.
const shimmerBand = 5.0

// shimmer renders text with a band of light sweeping across it: each rune is
// coloured by lerping from theme.ShimmerBase to theme.ShimmerHighlight based on
// its distance from a moving peak, so successive frames (advance phase by 1 per
// tick) read as a pulse of brightness travelling through the string. Width is
// unchanged — only colour SGR is added — so callers can measure the plain text
// for layout. Returns text untouched when colour is disabled (via FgColor).
func shimmer(text string, phase int) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return text
	}
	// The peak travels from off the left edge to off the right edge and wraps,
	// so the light fully enters and fully exits rather than snapping. period is
	// always >= 2*shimmerBand here (n >= 1), so no zero-guard is needed.
	period := n + int(shimmerBand)*2
	pos := float64(((phase%period)+period)%period) - shimmerBand

	var b strings.Builder
	b.Grow(len(text) * 4)
	for i, r := range runes {
		d := math.Abs(float64(i) - pos)
		w := 0.0
		if d < shimmerBand {
			w = 1 - d/shimmerBand // triangular falloff, peak = 1 at the centre
		}
		b.WriteString(termkit.FgColor(string(r), lerpColor(theme.ShimmerBase, theme.ShimmerHighlight, w)))
	}
	return b.String()
}

// lerpColor linearly interpolates between a and b at t, clamped to [0,1].
func lerpColor(a, b termkit.Color, t float64) termkit.Color {
	t = math.Max(0, math.Min(1, t))
	return termkit.Color{lerpU8(a[0], b[0], t), lerpU8(a[1], b[1], t), lerpU8(a[2], b[2], t)}
}

func lerpU8(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

// formatElapsed renders a running duration compactly: whole seconds under a
// minute ("4s"), then "m:ss" ("1:07") beyond. Sub-second waits show "0s" so
// the clock is present from the first frame rather than blinking in late.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	if secs < 60 {
		return strconv.Itoa(secs) + "s"
	}
	return strconv.Itoa(secs/60) + ":" + pad2(secs%60)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// workingIndicator renders the single-line "the model is warming up" state: a
// braille spinner and a rotating personality verb with a shimmer sweeping
// across them, followed by a dim live elapsed clock and an interrupt hint.
// Everything advances off m.spinnerFrame (shimmer phase + spinner) and
// m.turnStartedAt (clock), both updated on the 80ms tick, so it stays a pure
// function of model state — no timers of its own.
func (m *model) workingIndicator() string {
	elapsed := time.Since(m.turnStartedAt)
	verb := thinkingVerb(m.turnSeed, elapsed)
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
	head := shimmer(frame+" "+verb+"…", m.spinnerFrame)
	meta := workingMetaStyle.Render("  " + formatElapsed(elapsed) + " · ctrl+c to interrupt")
	return head + meta
}

// toolGlyph returns the leading glyph for a tool row in a given lifecycle
// state: a live braille spinner frame while pending/running (so the row
// visibly works), a check when done, a cross on error. frame is the shared
// spinner tick so every running row animates in lockstep.
func toolGlyph(status string, frame int) string {
	switch status {
	case "done":
		return "✓"
	case "error":
		return "✗"
	default: // pending, running
		return spinnerFrames[frame%len(spinnerFrames)]
	}
}
