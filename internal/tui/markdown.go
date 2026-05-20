package tui

import (
	"strings"

	"charm.land/glamour/v2"
)

const defaultMarkdownWidth = 80

// renderMarkdown renders a markdown string using Glamour with the given width.
// Falls back to the raw text if rendering fails.
func renderMarkdown(content string, width int) string {
	if width <= 0 {
		width = defaultMarkdownWidth
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimRight(rendered, "\n")
}
