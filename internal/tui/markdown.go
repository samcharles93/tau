package tui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"github.com/samcharles93/tau/internal/theme"
)

const defaultMarkdownWidth = 80

// mdCache memoizes glamour renderers per width so we avoid expensive
// allocation on every render call. Invalidate via invalidateMarkdownCache
// if the style changes at runtime.
var (
	mdCacheMu sync.Mutex
	mdCache   = map[int]*glamour.TermRenderer{}
)

// markdownRenderer returns a cached glamour renderer for the given width.
func markdownRenderer(width int) *glamour.TermRenderer {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()

	if r, ok := mdCache[width]; ok {
		return r
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.MarkdownStyle()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	mdCache[width] = r
	return r
}

// renderMarkdown renders a markdown string using Glamour with the given width.
// Falls back to the raw text if rendering fails.
func renderMarkdown(content string, width int) string {
	if width <= 0 {
		width = defaultMarkdownWidth
	}

	renderer := markdownRenderer(width)
	if renderer == nil {
		return content
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimRight(rendered, "\n")
}
