package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Paragraph is a multi-line, word-wrapped text block — the component for
// streaming assistant output. Append tokens as they arrive; Render re-wraps to
// the current width, honoring any embedded newlines. Safe for concurrent use
// (an application goroutine appends while the render goroutine reads).
type Paragraph struct {
	mu   sync.Mutex
	text string
	fn   func(string) string
}

// NewParagraph creates a paragraph with initial text.
func NewParagraph(text string) *Paragraph { return &Paragraph{text: text} }

// SetText replaces the content.
func (p *Paragraph) SetText(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text = s
}

// Append adds to the content (e.g. one streamed token).
func (p *Paragraph) Append(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text += s
}

// Text returns the current content.
func (p *Paragraph) Text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.text
}

// SetStyle sets a colour callback applied to each wrapped line.
func (p *Paragraph) SetStyle(fn func(string) string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fn = fn
}

// Invalidate is a no-op.
func (p *Paragraph) Invalidate() {}

// Render wraps the text to width and returns the resulting lines.
func (p *Paragraph) Render(width int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.text == "" {
		return nil
	}
	lines := wrapWords(p.text, width)
	if p.fn != nil && termkit.ColorEnabled() {
		for i := range lines {
			lines[i] = p.fn(lines[i])
		}
	}
	return lines
}

// WrapText word-wraps text to width visible columns, honoring embedded
// newlines as hard breaks. Useful for committing streamed text into history.
func WrapText(text string, width int) []string { return wrapWords(text, width) }

// wrapWords greedily word-wraps text to maxWidth visible columns, preserving
// explicit newlines as hard breaks and internal whitespace as-is (multiple
// spaces are not collapsed). A single word longer than maxWidth is left intact
// (callers truncate elsewhere if needed).
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
			ww := VisibleWidth(w)
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

// splitPreserving splits text into words around whitespace, returning the words
// and the number of spaces that preceded each word (spaces[0] is always 0).
// Multiple consecutive spaces are preserved.
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
