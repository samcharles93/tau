// Package termkit provides zero-dependency ANSI terminal rendering primitives.
// It is designed for CLI/TUI apps that need polished inline output — spinners,
// progress bars, hyperlinks, and three-state tool-call lifecycles — without
// coupling to any particular TUI framework.
//
// The package respects NO_COLOR (https://no-color.org) and automatically
// degrades to plain text when stdout is not a character device.
package termkit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// ANSI escape sequences — CSI ("\033[") and OSC ("\033]")
// ─────────────────────────────────────────────────────────────────────────────

const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
)

// Color is an RGB triple for truecolor SGR sequences.
type Color [3]uint8

// Common colours — keep the palette small; callers can construct their own.
var (
	ColorAmber    = Color{255, 158, 0}
	ColorGreen    = Color{0, 200, 83}
	ColorRed      = Color{255, 23, 68}
	ColorYellow   = Color{255, 234, 0}
	ColorObsidian = Color{18, 19, 26}
	ColorWhite    = Color{255, 255, 255}
	ColorGrey     = Color{128, 134, 150}
)

// Fg returns the CSI sequence for a truecolor foreground.
func (c Color) Fg() string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", c[0], c[1], c[2])
}

// Bg returns the CSI sequence for a truecolor background.
func (c Color) Bg() string {
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", c[0], c[1], c[2])
}

// ─────────────────────────────────────────────────────────────────────────────
// TTY / colour detection
// ─────────────────────────────────────────────────────────────────────────────

var (
	colorMu      sync.Mutex
	colorChecked bool
	colorOn      bool
)

// ColorEnabled reports whether ANSI sequences should be emitted.
// It returns false when output is not a character device or NO_COLOR is set.
func ColorEnabled() bool {
	colorMu.Lock()
	defer colorMu.Unlock()
	if colorChecked {
		return colorOn
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		colorOn = false
	} else {
		info, err := os.Stdout.Stat()
		colorOn = err == nil && info.Mode()&os.ModeCharDevice != 0
	}
	colorChecked = true
	return colorOn
}

// DisableColor forces colour off for subsequent ColorEnabled calls.
func DisableColor() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorOn = false
	colorChecked = true
}

// ForceColor forces colour on — useful in tests that run under a pipe.
func ForceColor() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorOn = true
	colorChecked = true
}

// ─────────────────────────────────────────────────────────────────────────────
// Public helpers
// ─────────────────────────────────────────────────────────────────────────────

// Style wraps s in the given ANSI codes and appends Reset.
func Style(s string, codes ...string) string {
	if !ColorEnabled() {
		return s
	}
	return strings.Join(codes, "") + s + Reset
}

// Wrap wraps s in the given ANSI codes WITHOUT a trailing Reset.
// Use this inside backgrounds (e.g. Box) where a mid-line Reset would
// clear the parent's background colour.
func Wrap(s string, codes ...string) string {
	if !ColorEnabled() {
		return s
	}
	return strings.Join(codes, "") + s
}

// Emit writes a raw control sequence to w only when colour is enabled.
// If w is nil, os.Stdout is used.
func Emit(w io.Writer, seq string) {
	if !ColorEnabled() {
		return
	}
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprint(w, seq)
}

// FgColor wraps text in a truecolor foreground SGR sequence.
func FgColor(s string, c Color) string {
	return Style(s, c.Fg())
}

// BgColor returns a background colour SGR sequence with text.
func BgColor(s string, bg Color) string {
	return Style(s, bg.Bg())
}

// FgBgColor wraps text in truecolor foreground + background SGR sequences.
func FgBgColor(s string, fg, bg Color) string {
	return Style(s, fg.Fg(), bg.Bg())
}

// FgOnly wraps text in a foreground colour WITHOUT a trailing Reset.
// Use inside components that sit within a Box background.
func FgOnly(s string, c Color) string { return Wrap(s, c.Fg()) }

// BgOnly wraps text in a background colour WITHOUT a trailing Reset.
func BgOnly(s string, c Color) string { return Wrap(s, c.Bg()) }

// FgBgOnly wraps text in fg+bg WITHOUT a trailing Reset.
func FgBgOnly(s string, fg, bg Color) string { return Wrap(s, fg.Fg(), bg.Bg()) }

// FromRGB constructs a Color from 8-bit RGB components.
// This is the bridge for frameworks (like go-tui) that expose their colours
// as (r, g, b uint8) triples.
func FromRGB(r, g, b uint8) Color {
	return Color{r, g, b}
}

// Luminance returns the relative luminance of a colour per WCAG 2.x.
func Luminance(c Color) float64 {
	return 0.2126*srgbToLinear(c[0]) + 0.7152*srgbToLinear(c[1]) + 0.0722*srgbToLinear(c[2])
}

// ContrastRatio returns the WCAG contrast ratio between two colours.
// Order doesn't matter. 1.0 = identical, 21.0 = black vs white.
// AA requires ≥ 4.5 for normal text, ≥ 3.0 for large text.
func ContrastRatio(a, b Color) float64 {
	l1 := Luminance(a)
	l2 := Luminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// ContrastFg returns black or white for readable text on the given
// background, aiming for the higher contrast ratio.
func ContrastFg(bg Color) Color {
	black := Color{18, 19, 26}
	white := Color{255, 255, 255}
	if ContrastRatio(bg, white) > ContrastRatio(bg, black) {
		return white
	}
	return black
}

func srgbToLinear(c uint8) float64 {
	f := float64(c) / 255.0
	if f <= 0.04045 {
		return f / 12.92
	}
	v := (f + 0.055) / 1.055
	return v * v
}
