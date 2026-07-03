package taui

import "strings"

// StackDirection selects how Stack lays out its children.
type StackDirection int

const (
	StackVertical StackDirection = iota
	StackHorizontal
)

// Stack lays out child components vertically (delegating to Container, which
// only concatenates lines top-to-bottom) or horizontally, rendering each
// child independently and zipping their lines side by side with Gap spaces
// between columns, padding both mismatched widths and heights.
type Stack struct {
	Container
	Direction StackDirection
	Gap       int
}

// NewStack creates an empty Stack. Add children with AddChild.
func NewStack(direction StackDirection, gap int) *Stack {
	return &Stack{Direction: direction, Gap: gap}
}

// Render lays out children per Direction.
func (s *Stack) Render(width int) []string {
	if s.Direction == StackVertical {
		return s.Container.Render(width)
	}
	return s.renderHorizontal(width)
}

func (s *Stack) renderHorizontal(width int) []string {
	n := len(s.Children)
	if n == 0 {
		return nil
	}
	gap := max(0, s.Gap)
	totalGap := gap * (n - 1)
	perChild := max(1, (width-totalGap)/n)

	columns := make([][]string, n)
	colWidth := make([]int, n)
	maxHeight := 0
	for i, child := range s.Children {
		lines := child.Render(perChild)
		columns[i] = lines
		for _, l := range lines {
			if vw := VisibleWidth(l); vw > colWidth[i] {
				colWidth[i] = vw
			}
		}
		if len(lines) > maxHeight {
			maxHeight = len(lines)
		}
	}

	gapStr := strings.Repeat(" ", gap)
	rows := make([]string, maxHeight)
	for row := range maxHeight {
		parts := make([]string, n)
		for i := range n {
			var cell string
			if row < len(columns[i]) {
				cell = columns[i][row]
			}
			if pad := colWidth[i] - VisibleWidth(cell); pad > 0 {
				cell += strings.Repeat(" ", pad)
			}
			parts[i] = cell
		}
		rows[row] = strings.Join(parts, gapStr)
	}
	return rows
}
