package taui

import (
	"strconv"
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// List renders bulleted or numbered items, word-wrapping continuation lines
// with a hanging indent so wrapped text aligns under the item text.
type List struct {
	items   []string
	ordered bool
	fn      func(string) string
}

// NewList creates a List component.
func NewList(items []string, ordered bool) *List {
	return &List{items: items, ordered: ordered}
}

// SetStyle sets a colour callback applied to each rendered line.
func (l *List) SetStyle(fn func(string) string) { l.fn = fn }

// Invalidate is a no-op; List holds no cached render.
func (l *List) Invalidate() {}

// Render returns the wrapped, marker-prefixed lines for every item.
func (l *List) Render(width int) []string {
	if len(l.items) == 0 {
		return nil
	}
	var lines []string
	for i, item := range l.items {
		marker := "• "
		if l.ordered {
			marker = strconv.Itoa(i+1) + ". "
		}
		markerWidth := VisibleWidth(marker)
		indent := strings.Repeat(" ", markerWidth)
		contentWidth := max(1, width-markerWidth)

		wrapped := wrapWords(item, contentWidth)
		for j, w := range wrapped {
			line := w
			if l.fn != nil && termkit.ColorEnabled() {
				line = l.fn(line)
			}
			if j == 0 {
				lines = append(lines, marker+line)
			} else {
				lines = append(lines, indent+line)
			}
		}
	}
	return lines
}
