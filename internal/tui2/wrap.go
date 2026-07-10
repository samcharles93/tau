package tui2

import "strings"

// wrapWords greedily word-wraps text to maxWidth visible columns,
// preserving explicit newlines as hard breaks and internal whitespace
// as-is (multiple spaces are not collapsed). A single word longer than
// maxWidth is left intact (callers truncate elsewhere if needed).
//
// Mirrors pkg/taui's wrapWords (paragraph.go) but uses visibleWidth
// (statusbar.go) which delegates to lipgloss.Width — the same
// uniseg-based measurement the viewport uses, so wrapped lines fit.
func wrapWords(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		words, spaces := splitPreserving(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		lineWidth := 0
		for i, w := range words {
			ww := visibleWidth(w)
			switch {
			case line == "":
				line, lineWidth = w, ww
			case lineWidth+spaces[i]+ww <= maxWidth:
				line += strings.Repeat(" ", spaces[i]) + w
				lineWidth += spaces[i] + ww
			default:
				out = append(out, line)
				line, lineWidth = w, ww
			}
		}
		out = append(out, line)
	}
	return out
}

// splitPreserving splits text into words around whitespace, returning the
// words and the number of spaces that preceded each word (spaces[0] is
// always 0). Multiple consecutive spaces are preserved.
func splitPreserving(s string) (words []string, spaces []int) {
	start := 0
	for start < len(s) {
		// Skip leading spaces.
		sp := 0
		for start < len(s) && s[start] == ' ' {
			sp++
			start++
		}
		if start >= len(s) {
			break
		}
		// Scan the word.
		end := start
		for end < len(s) && s[end] != ' ' {
			end++
		}
		words = append(words, s[start:end])
		spaces = append(spaces, sp)
		start = end
	}
	return
}
