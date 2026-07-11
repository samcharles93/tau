package tui2

import (
	"strconv"
	"time"
)

// This file holds the "living" presentation touches that keep tui2 from
// feeling robotic while a turn is in flight: a calm dot animation for the
// working indicator, a compact elapsed-time formatter for tool rows, and
// the per-state glyphs for tool rows. All of it is driven off the existing
// 80ms spinner tick (see model.spinnerFrame) — no goroutines, no extra
// timers — so it composes through Bubbletea's normal Update/View flow.

// thinkingDotFrames are the three animation frames for the working indicator:
// a single dot, two dots, three dots, then repeat.
var thinkingDotFrames = [...]string{
	"●",
	"●●",
	"●●●",
}

// thinkingDotFrameTicks is how many 80ms spinner ticks each dot frame stays
// visible before advancing. At 6 ticks per frame, each visual frame lasts
// ~480ms, giving a calm, unhurried pulse.
const thinkingDotFrameTicks = 6

// thinkingDots returns the dot frame for a given spinnerFrame index, advancing
// every thinkingDotFrameTicks ticks and wrapping through the three frames.
// Pure and total — safe for any int input, including negative values.
func thinkingDots(frame int) string {
	index := frame / thinkingDotFrameTicks
	n := len(thinkingDotFrames)
	return thinkingDotFrames[((index%n)+n)%n]
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
// static "Thinking " label followed by a calm dot animation (●, ●●, ●●●,
// repeat). Only the dots carry Tau's Warm Ochre accent — the label stays
// unstyled/inherits the terminal's default foreground, matching the rest of
// the app's "accent is sparing, on the small marker only" convention (see
// reasoningLabelStyle, userGlyphStyle) rather than tinting a phrase that's
// on screen for the whole thinking phase of every turn. Advances off
// m.spinnerFrame on the 80ms tick — no timers of its own.
func (m *model) workingIndicator() string {
	return "Thinking " + thinkingIndicatorStyle.Render(thinkingDots(m.spinnerFrame))
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
