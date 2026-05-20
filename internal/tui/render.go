package tui

import (
	"strings"

	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
)

// turnBlock represents one rendered conversation turn for the viewport.
type turnBlock struct {
	role    aimchat.ChatRole
	content string
	// rendered caches the styled output for completed turns.
	rendered string
	// width tracks the render width so we can invalidate on resize.
	width int
}

// renderConversation builds the full viewport content from structured turns.
func renderConversation(turns []turnBlock, streamingContent string, width int, t theme) string {
	var b strings.Builder

	for i := range turns {
		if turns[i].rendered == "" || turns[i].width != width {
			turns[i].rendered = renderTurn(turns[i].role, turns[i].content, width, t)
			turns[i].width = width
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(turns[i].rendered)
	}

	if streamingContent != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderStreamingTurn(streamingContent, width, t))
	}

	return b.String()
}

// renderTurn styles a completed turn with its role label and markdown content.
func renderTurn(role aimchat.ChatRole, content string, width int, t theme) string {
	var b strings.Builder

	label := renderRoleLabel(role, t)
	b.WriteString(label)
	b.WriteByte('\n')

	contentWidth := max(width-2, 20)

	switch role {
	case aimchat.ChatRoleAssistant:
		b.WriteString(renderMarkdown(content, contentWidth))
	default:
		b.WriteString(t.StreamingText.Width(contentWidth).Render(content))
	}

	return b.String()
}

// renderStreamingTurn renders the in-flight assistant response as plain text
// with width constraint for proper viewport wrapping.
func renderStreamingTurn(content string, width int, t theme) string {
	var b strings.Builder
	contentWidth := max(width-2, 20)
	b.WriteString(t.AssistantLabel.Render("Assistant"))
	b.WriteByte('\n')
	b.WriteString(t.StreamingText.Width(contentWidth).Render(content))
	return b.String()
}

// renderRoleLabel returns the styled label for a given role.
func renderRoleLabel(role aimchat.ChatRole, t theme) string {
	switch role {
	case aimchat.ChatRoleUser:
		return t.UserLabel.Render("You")
	case aimchat.ChatRoleAssistant:
		return t.AssistantLabel.Render("Assistant")
	default:
		return t.Dim.Render(string(role))
	}
}
