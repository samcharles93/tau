package tui2

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// messageLineRange records the [startLine, endLine) span (half-open,
// indices into m.renderedLines at the time of recording) that one
// ChatMessage's rendered lines occupy. Unlike toolBoxGeometry, this indexes
// m.renderedLines directly rather than final screen rows — logicalLineAtRow
// already maps a screen row to a renderedLines index, so no separate
// box-relative-to-absolute translation is needed in computeLayout.
type messageLineRange struct {
	id        string
	content   string // raw (unstyled) message content, for Copy — renderedLines is lipgloss-styled, same reason lastAssistantText is kept separately
	startLine int
	endLine   int
}

// streamCursor is the block cursor shown immediately after the actively
// streaming assistant response. It is presentation-only and never written
// into persisted content or copied response text.
const streamCursor = "▋"

// renderStreamingLines word-wraps the active response and appends
// streamCursor where the next character will land. Reserving its width
// prevents the cursor from causing an extra terminal wrap.
func renderStreamingLines(text string, width int) []string {
	cursorWidth := lipgloss.Width(streamCursor)
	wrapped := wrapWords(text, max(width-cursorWidth, 1))
	lines := make([]string, len(wrapped))
	for i, line := range wrapped {
		rendered := streamStyle.Render(line)
		if i == len(wrapped)-1 {
			rendered += streamCursorStyle.Render(streamCursor)
		}
		lines[i] = rendered
	}
	return lines
}

// appendMessage writes a styled message line to the viewport and scrolls to
// the bottom. Multi-line content is split so each visual line gets its own
// style wrapping; only the first line carries the role prefix.
func (m *model) appendMessage(role, content string) {
	if role == "assistant" {
		m.lastAssistantText = content
	}
	lines := strings.Split(content, "\n")
	if role == "tool" {
		m.renderedLines = append(m.renderedLines, lines...)
		m.viewport.SetContentLines(m.renderedLines)
		return
	}
	for i, l := range lines {
		if i == 0 {
			m.renderedLines = append(m.renderedLines, renderLine(role, l))
		} else {
			m.renderedLines = append(m.renderedLines, renderContinuationLine(role, l))
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
}

// renderLine styles a scrollback line by role. Matches taui's convention
// (internal/tui/inline_chat.go's submit echo, PrintAbove("%s %s",
// c.bold("⏎"), prompt)): a user message gets a bold return-glyph prefix, an
// assistant message gets none — neither ever gets a literal "You:"/"tau:"
// name label, which is legacy behaviour from an earlier renderer.
func renderLine(role, content string) string {
	switch role {
	case "user":
		return userGlyphStyle.Render("⏎") + " " + userStyle.Render(content)
	case "assistant":
		return assistantStyle.Render(content)
	default:
		return content
	}
}

func renderContinuationLine(role, content string) string {
	switch role {
	case "user":
		return userContinuationStyle.Render(content)
	case "assistant":
		return assistantContinuationStyle.Render(content)
	default:
		return continuationStyle.Render(content)
	}
}

// sessionSummariesText renders the /session list output as a lineage tree:
// child sessions (parent_session_id) nest under their parent with indent
// glyphs, and each row carrying an agent_instance_id shows it as attribution
// (the instance id is already "specname#suffix", so no separate lookup
// against agent_instances is needed to display it). Sessions whose parent
// isn't present in this page (or that have none) render as roots.
func sessionSummariesText(summaries []tauchat.SessionSummary, nextCursor string) string {
	if len(summaries) == 0 {
		return "Sessions: no saved sessions"
	}

	byID := make(map[string]bool, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = true
	}
	childrenOf := make(map[string][]tauchat.SessionSummary)
	var roots []tauchat.SessionSummary
	for _, s := range summaries {
		if s.ParentSessionID != "" && byID[s.ParentSessionID] {
			childrenOf[s.ParentSessionID] = append(childrenOf[s.ParentSessionID], s)
		} else {
			roots = append(roots, s)
		}
	}

	var b strings.Builder
	b.WriteString("Sessions:")
	var writeNode func(s tauchat.SessionSummary, depth int)
	writeNode = func(s tauchat.SessionSummary, depth int) {
		indent := strings.Repeat("  ", depth)
		glyph := "-"
		if depth > 0 {
			glyph = "└─"
		}
		fmt.Fprintf(&b, "\n%s%s %s · %d messages · %s", indent, glyph, s.ID, s.MessageCount, s.ModelID)
		if s.AgentInstanceID != "" {
			fmt.Fprintf(&b, " · agent %s", s.AgentInstanceID)
		}
		if line := sessionSummaryMetricsLine(s); line != "" {
			fmt.Fprintf(&b, "\n%s  %s", indent, line)
		}
		for _, child := range childrenOf[s.ID] {
			writeNode(child, depth+1)
		}
	}
	for _, root := range roots {
		writeNode(root, 0)
	}
	if nextCursor != "" {
		b.WriteString("\nMore sessions available.")
	}
	return b.String()
}

// sessionInfoText renders full detail for a single session (/session info
// <id>) — mirrors internal/tui/inline_events.go's printSessionInfo.
func sessionInfoText(s tauchat.SessionSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Session %s\n", s.ID)
	fmt.Fprintf(&b, "Model: %s\n", s.ModelID)
	fmt.Fprintf(&b, "Provider: %s\n", s.Provider)
	fmt.Fprintf(&b, "Messages: %d", s.MessageCount)
	if s.TotalTokens > 0 {
		fmt.Fprintf(&b, "\nTokens: ↑%s ↓%s (total %s)",
			humanizeTokens(s.InputTokens), humanizeTokens(s.OutputTokens), humanizeTokens(s.TotalTokens))
	}
	if s.Cost > 0 {
		fmt.Fprintf(&b, "\nCost: %s", formatCost(s.Cost))
	}
	if s.DurationMs > 0 {
		fmt.Fprintf(&b, "\nDuration: %s", formatDurationCompact(s.DurationMs))
	}
	if s.ToolCalls > 0 {
		fmt.Fprintf(&b, "\nTool calls: %d", s.ToolCalls)
		if s.ToolErrors > 0 {
			fmt.Fprintf(&b, " (%d errors)", s.ToolErrors)
		}
	}
	fmt.Fprintf(&b, "\nCreated: %s", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "\nUpdated: %s", s.UpdatedAt.Format(time.RFC3339))
	return b.String()
}

// skillsChangedText renders the formatted skill catalog (name, description,
// scope) shown on SkillsChangedEvent — mirrors
// internal/tui/inline_events.go's handleSkillsChanged.
func skillsChangedText(skills []tauchat.SkillInfo) string {
	if len(skills) == 0 {
		return "_no skills available_"
	}
	var b strings.Builder
	b.WriteString("**Available Skills**\n\n")
	for _, skill := range skills {
		fmt.Fprintf(&b, "- **%s** — %s", skill.Name, skill.Description)
		if skill.Scope != "" {
			fmt.Fprintf(&b, " _(%s)_", skill.Scope)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sessionSummaryMetricsLine builds a compact single-line metrics summary for
// a session entry when at least one metric field is non-zero. Mirrors
// internal/tui/inline_events.go's function of the same name.
func sessionSummaryMetricsLine(s tauchat.SessionSummary) string {
	var parts []string
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		parts = append(parts, "↑"+humanizeTokens(s.InputTokens)+" ↓"+humanizeTokens(s.OutputTokens))
	}
	if s.Cost > 0 {
		parts = append(parts, formatCost(s.Cost))
	}
	if s.ToolCalls > 0 {
		toolStr := fmt.Sprintf("%d tools", s.ToolCalls)
		if s.ToolErrors > 0 {
			toolStr += fmt.Sprintf(" (%d err)", s.ToolErrors)
		}
		parts = append(parts, toolStr)
	}
	if s.DurationMs > 0 {
		parts = append(parts, formatDurationCompact(s.DurationMs))
	}
	return strings.Join(parts, " · ")
}

// formatDurationCompact renders a millisecond duration compactly (e.g.
// "450ms", "3s", "2m", "1h 5m"). Mirrors internal/tui/inline_events.go's
// function of the same name.
func formatDurationCompact(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

// renderCompletions draws the dropdown: a scrolling window (so a selection
// past the visible window is never invisible/unreachable), group headers,
// a chevron + bold-highlighted matched characters on the selected row, and
// a description column aligned across the visible rows. Mirrors
// pkg/taui/completions.go's Render.
func renderCompletions(rows []compRow, selected, width int) string {
	const window = 10
	n := len(rows)
	size := min(n, window)
	start := max(selected-size/2, 0)
	if start+size > n {
		start = n - size
	}
	end := start + size

	descCol := completionDescColumn(rows)

	var out []string
	lastGroup := ""
	for i := start; i < end; i++ {
		row := rows[i]
		if row.group != lastGroup {
			out = append(out, compTitleStyle.Render(row.group))
			lastGroup = row.group
		}
		out = append(out, renderCompletionRow(row, i == selected, descCol))
	}
	if start > 0 {
		out = append([]string{compMoreStyle.Render(fmt.Sprintf("  ↑ %d more", start))}, out...)
	}
	if remaining := n - end; remaining > 0 {
		out = append(out, compMoreStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)))
	}

	if width > 0 {
		for i := range out {
			out[i] = truncateANSIToWidth(out[i], width, "…")
		}
	}
	return strings.Join(out, "\n")
}

// completionDescColumn returns the width to pad words to so descriptions
// line up in a column — the widest word among visible rows that carry a
// description, capped so an outlier doesn't push the column off-screen.
func completionDescColumn(rows []compRow) int {
	const maxCol = 44
	col := 0
	for _, r := range rows {
		if r.Description == "" {
			continue
		}
		if w := visibleWidth(r.Word); w > col {
			col = w
		}
	}
	return min(col, maxCol)
}

func renderCompletionRow(row compRow, selected bool, descCol int) string {
	chevron := "  "
	wordStyle := compItemStyle
	if selected {
		chevron = "▶ "
		wordStyle = compSelectedStyle
	}

	word := renderHighlightedWord(row.Word, row.highlight, wordStyle)
	line := chevron + word
	if row.Description != "" {
		pad := descCol - visibleWidth(row.Word)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += compDescStyle.Render(" " + row.Description)
	}
	return line
}

// renderHighlightedWord renders word with base applied throughout and
// compHighlightStyle layered on top of the matched rune spans (bold, so a
// fuzzy match's relevance is visible at a glance, same as taui's dropdown).
func renderHighlightedWord(word string, spans [][2]int, base lipgloss.Style) string {
	if len(spans) == 0 {
		return base.Render(word)
	}
	runes := []rune(word)
	var sb strings.Builder
	si := 0
	for i := 0; i < len(runes); {
		if si < len(spans) && i == spans[si][0] {
			end := min(spans[si][1], len(runes))
			sb.WriteString(compHighlightStyle.Render(string(runes[i:end])))
			i = end
			si++
			continue
		}
		next := len(runes)
		if si < len(spans) {
			next = spans[si][0]
		}
		sb.WriteString(base.Render(string(runes[i:next])))
		i = next
	}
	return sb.String()
}
