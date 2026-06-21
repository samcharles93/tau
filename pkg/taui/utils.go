package taui

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// VisibleWidth returns the visible width of a string in terminal columns,
// after stripping ANSI escape sequences. Width is computed with grapheme-aware
// rules (rivo/uniseg), so emoji, CJK, combining marks, and ZWJ sequences are
// measured correctly.
func VisibleWidth(s string) int {
	if len(s) == 0 {
		return 0
	}
	// Fast path: pure ASCII printable (1 column each, no escapes).
	if isPrintableASCII(s) {
		return len(s)
	}
	return uniseg.StringWidth(stripANSI(s))
}

// isPrintableASCII returns true if every byte in s is a printable ASCII char (0x20-0x7e).
func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// runeWidth returns the terminal column width of a single rune. For lone runes
// this matches uniseg; multi-rune grapheme clusters (e.g. ZWJ emoji) should be
// measured with VisibleWidth on the whole string rather than summed per rune.
func runeWidth(r rune) int {
	if r == utf8.RuneError {
		return 0
	}
	return uniseg.StringWidth(string(r))
}

// stripANSI removes ANSI/OSC escape sequences from a string.
func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) {
			if s[i+1] == '[' {
				// CSI: ESC [ ... m/G/K/H/J
				j := i + 2
				for j < len(s) && !isCSITerminator(s[j]) {
					j++
				}
				if j < len(s) {
					i = j + 1
					continue
				}
			}
			if s[i+1] == ']' {
				// OSC: ESC ] ... BEL or ESC ] ... ST (ESC \)
				j := i + 2
				for j < len(s) {
					if s[j] == '\x07' {
						i = j + 1
						break
					}
					if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
						i = j + 2
						break
					}
					j++
				}
				if j >= len(s) {
					i = j
				}
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isCSITerminator(b byte) bool {
	switch b {
	case 'm', 'G', 'K', 'H', 'J', 'A', 'B', 'C', 'D', 'E', 'F', 'S', 'T', 'f', 'h', 'l', 's', 'u':
		return true
	}
	return false
}

// truncateANSIToWidth caps the visible width of a styled line to width columns
// while preserving its ANSI escape sequences (which have zero visible width).
// Used by the renderer to stop frame lines from wrapping, which would break the
// one-logical-line-per-row assumption the inline cursor model relies on.
func truncateANSIToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(s) <= width {
		return s
	}
	var b strings.Builder
	vis := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			j := i + 1
			switch {
			case j < len(s) && s[j] == '[': // CSI ... final byte
				j++
				for j < len(s) && !isCSITerminator(s[j]) {
					j++
				}
				if j < len(s) {
					j++
				}
			case j < len(s) && s[j] == ']': // OSC ... BEL or ST
				for j < len(s) {
					if s[j] == '\x07' {
						j++
						break
					}
					if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
			default:
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runeWidth(r)
		if vis+w > width {
			break
		}
		b.WriteString(s[i : i+size])
		vis += w
		i += size
	}
	return b.String()
}

// TruncateToWidth truncates text to fit within maxWidth visible columns.
// If text exceeds maxWidth, an ellipsis is appended.
func TruncateToWidth(text string, maxWidth int, ellipsis string) string {
	if maxWidth <= 0 {
		return ""
	}
	if text == "" {
		return ""
	}

	visWidth := VisibleWidth(text)
	if visWidth <= maxWidth {
		return text
	}

	ellipsisWidth := VisibleWidth(ellipsis)
	targetWidth := maxWidth - ellipsisWidth
	if targetWidth <= 0 {
		// Just fit the ellipsis itself.
		var result strings.Builder
		resultWidth := 0
		for _, r := range ellipsis {
			w := runeWidth(r)
			if resultWidth+w > maxWidth {
				break
			}
			result.WriteString(string(r))
			resultWidth += w
		}
		return result.String()
	}

	// Truncate word to target width.
	var result strings.Builder
	resultWidth := 0
	for _, r := range text {
		w := runeWidth(r)
		if resultWidth+w > targetWidth {
			break
		}
		result.WriteString(string(r))
		resultWidth += w
	}
	return result.String() + ellipsis
}
