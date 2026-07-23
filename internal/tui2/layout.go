package tui2

// Horizontal content padding — the indent applied to continuation lines
// and the left gutter for all chat content inside the viewport.
const (
	contentPadLeft  = 6 // left gutter for wrapped/continuation lines
	contentPadRight = 0 // right gutter (reserved for future use)
)

// Vertical spacing between distinct components in the scrollback.
// These are blank-line counts inserted between adjacent blocks.
const (
	spacingReasoningToContent = 0 // reasoning block → tool box or assistant text
	spacingToolToContent      = 0 // tool box/group → next content
	spacingBetweenMessages    = 0 // one message → the next message
)

// Tool box internal spacing.
const (
	toolBoxPadTopBottom    = 0
	toolBoxPadLeftRight    = 1
	spacingBetweenToolRows = 0 // gap between per-tool rows inside a group
)

// Separator (between viewport and input chrome) margins.
const (
	separatorMarginTop    = 0
	separatorMarginBottom = 0
)

// interposeBlankLines inserts count blank strings between each element of
// lines, used only in the presentation layer to create vertical spacing
// without mutating persisted content.
func interposeBlankLines(lines []string, count int) []string {
	if count <= 0 || len(lines) <= 1 {
		return lines
	}
	out := make([]string, 0, len(lines)+(len(lines)-1)*count)
	for i, l := range lines {
		if i > 0 {
			for range count {
				out = append(out, "")
			}
		}
		out = append(out, l)
	}
	return out
}
