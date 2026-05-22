package tui

import (
	"strings"

	"charm.land/glamour/v2"
)

// streamingMarkdown caches a "stable prefix" glamour render so each streaming
// flush only re-renders the trailing portion of the document.
//
// The boundary between "stable" and "trailing" is detected by
// findSafeMarkdownBoundary: a byte offset immediately after a blank line at
// which no markdown construct is open (fenced code block, list, table,
// blockquote, setext header).
//
// Invariants:
//   - stablePrefix is always a literal byte prefix of the most recently
//     rendered content. If new content is not a prefix-extension, the cache
//     is dropped.
//   - stablePrefixRender is the glamour render of stablePrefix with
//     surrounding whitespace trimmed.
//   - width is the wrap width that produced stablePrefixRender. A width
//     change drops the cache.
type streamingMarkdown struct {
	width              int
	stablePrefix       string
	stablePrefixRender string
}

// Reset drops every cached field. After Reset the next Render call is
// guaranteed to be a full render.
func (s *streamingMarkdown) Reset() {
	s.width = 0
	s.stablePrefix = ""
	s.stablePrefixRender = ""
}

// Render returns the glamour-rendered content at the given width, reusing the
// cached stable-prefix render when safe. Falls back to a full render on any
// uncertainty.
func (s *streamingMarkdown) Render(content string, width int, renderer *glamour.TermRenderer) string {
	full := func() string {
		out, err := renderer.Render(content)
		if err != nil {
			return content
		}
		return strings.TrimSuffix(out, "\n")
	}

	// Width change or content not a prefix-extension: drop cache.
	if width != s.width || !strings.HasPrefix(content, s.stablePrefix) {
		s.Reset()
		s.width = width
		out := full()
		s.tryAdvance(content, width, renderer)
		return out
	}

	boundary := findSafeMarkdownBoundary(content)
	if boundary < 0 {
		return full()
	}

	if boundary <= len(s.stablePrefix) {
		// Cached prefix already covers the boundary. Render trailing partial.
		trail := content[len(s.stablePrefix):]
		return glueRenders(s.stablePrefixRender, renderTrailing(trail, renderer))
	}

	// New safe content discovered. Render the new chunk, promote the boundary.
	newChunk := content[len(s.stablePrefix):boundary]
	newChunkRender := renderTrailing(newChunk, renderer)
	s.stablePrefixRender = glueRenders(s.stablePrefixRender, newChunkRender)
	s.stablePrefix = content[:boundary]

	trail := content[boundary:]
	if trail == "" {
		return s.stablePrefixRender
	}
	return glueRenders(s.stablePrefixRender, renderTrailing(trail, renderer))
}

// tryAdvance seeds the cache after a full render when a safe boundary exists.
func (s *streamingMarkdown) tryAdvance(content string, width int, renderer *glamour.TermRenderer) {
	boundary := findSafeMarkdownBoundary(content)
	if boundary <= 0 {
		return
	}
	prefix := content[:boundary]
	out, err := renderer.Render(prefix)
	if err != nil {
		return
	}
	s.stablePrefix = prefix
	s.stablePrefixRender = trimGlamourMargins(out)
	s.width = width
}

// renderTrailing renders a partial fragment and trims margins for concatenation.
func renderTrailing(text string, renderer *glamour.TermRenderer) string {
	if text == "" {
		return ""
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return trimGlamourMargins(out)
}

// glueRenders concatenates two glamour-rendered fragments with a single blank
// line separator.
func glueRenders(prefix, trail string) string {
	prefix = trimGlamourMargins(prefix)
	trail = trimGlamourMargins(trail)
	switch {
	case prefix == "" && trail == "":
		return ""
	case prefix == "":
		return trail
	case trail == "":
		return prefix
	default:
		return prefix + "\n\n" + trail
	}
}

// trimGlamourMargins strips leading and trailing whitespace from glamour output.
func trimGlamourMargins(s string) string {
	return strings.Trim(s, " \t\n")
}

// findSafeMarkdownBoundary returns the byte offset of the end of the latest
// safe boundary in content (immediately after a blank-line separator) where no
// markdown construct is open. Returns -1 when no safe boundary exists.
func findSafeMarkdownBoundary(content string) int {
	if len(content) == 0 {
		return -1
	}

	for p := blankLineBefore(content, len(content)); p > 0; p = blankLineBefore(content, p-1) {
		if isSafeBoundaryAt(content, p) {
			return p
		}
	}
	return -1
}

// blankLineBefore returns the byte offset of the first character after the
// latest blank-line separator that ends strictly before `until`.
// Returns -1 when no blank-line separator exists.
func blankLineBefore(content string, until int) int {
	if until <= 0 {
		return -1
	}
	end := until
	for end > 0 {
		nl := strings.LastIndexByte(content[:end], '\n')
		if nl < 0 {
			return -1
		}
		prev := strings.LastIndexByte(content[:nl], '\n')
		if prev >= 0 {
			gap := content[prev+1 : nl]
			if isBlankOrSpaces(gap) {
				return nl + 1
			}
		}
		end = nl
	}
	return -1
}

// isBlankOrSpaces reports whether s is empty or contains only spaces/tabs.
func isBlankOrSpaces(s string) bool {
	for i := range len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// isSafeBoundaryAt checks that content[:p] is safe to split.
func isSafeBoundaryAt(content string, p int) bool {
	prefix := content[:p]

	// Even number of triple-backtick fence lines (no open fence).
	if countFenceLines(prefix)%2 != 0 {
		return false
	}

	// Last non-blank line must not open a construct.
	lastLine := lastNonBlankLine(prefix)
	if lastLine != "" && lineOpensConstruct(lastLine) {
		return false
	}

	// Next line must not be a setext underline.
	if rest := content[p:]; rest != "" {
		first := firstNonBlankLine(rest)
		if isSetextUnderline(first) {
			return false
		}
	}

	return true
}

// countFenceLines counts lines that are triple-backtick fences.
func countFenceLines(s string) int {
	count := 0
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			count++
		}
	}
	return count
}

// lastNonBlankLine returns the last line that is not empty or whitespace-only.
func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// firstNonBlankLine returns the first non-blank line of s.
func firstNonBlankLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// lineOpensConstruct reports whether a line could be the start of a multi-line
// markdown construct that would be split incorrectly at a boundary.
func lineOpensConstruct(line string) bool {
	trimmed := strings.TrimSpace(line)

	// List item markers.
	if len(trimmed) > 0 {
		ch := trimmed[0]
		if ch == '-' || ch == '*' || ch == '+' {
			if len(trimmed) > 1 && (trimmed[1] == ' ' || trimmed[1] == '\t') {
				return true
			}
		}
		// Ordered list: starts with digit(s) followed by '.' or ')'.
		if ch >= '0' && ch <= '9' {
			for i := 1; i < len(trimmed); i++ {
				if trimmed[i] >= '0' && trimmed[i] <= '9' {
					continue
				}
				if (trimmed[i] == '.' || trimmed[i] == ')') && i+1 < len(trimmed) && trimmed[i+1] == ' ' {
					return true
				}
				break
			}
		}
	}

	// Table row (contains |).
	if strings.Contains(trimmed, "|") {
		return true
	}

	// Block quote.
	if strings.HasPrefix(trimmed, ">") {
		return true
	}

	// Setext header underline.
	if isSetextUnderline(trimmed) {
		return true
	}

	// Indented code (4+ spaces or tab).
	if len(line) > 0 && (strings.HasPrefix(line, "    ") || line[0] == '\t') {
		return true
	}

	return false
}

// isSetextUnderline reports whether line looks like a setext header underline.
func isSetextUnderline(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	allEq := true
	allDash := true
	for _, ch := range trimmed {
		if ch != '=' {
			allEq = false
		}
		if ch != '-' {
			allDash = false
		}
	}
	return allEq || allDash
}
