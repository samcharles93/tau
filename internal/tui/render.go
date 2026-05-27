package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// turnBlock represents one rendered conversation turn for the viewport.
type turnBlock struct {
	role      tauchat.ChatRole
	content   string
	tools     []toolActivity
	reasoning string
	// rendered caches the styled output for completed turns.
	rendered string
	// width tracks the render width so we can invalidate on resize.
	width int
	// finished marks the turn as immutable so it can be frozen.
	finished bool
}

type toolActivity struct {
	callID           string
	toolName         string
	argumentsSummary string
	status           string
	duration         string
	resultSummary    string
	isError          bool
	truncated        bool
}

// renderConversation builds the full viewport content from structured turns.
func renderConversation(
	turns []turnBlock,
	streamingContent string,
	streamingTools []toolActivity,
	streamingReasoning string,
	showReasoning bool,
	streamMD *streamingMarkdown,
	width int,
	t tuiTheme,
) string {
	var b strings.Builder

	for i := range turns {
		if turns[i].rendered == "" || turns[i].width != width {
			turns[i].rendered = renderTurn(turns[i], showReasoning, width, t)
			turns[i].width = width
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(turns[i].rendered)
	}

	if streamingContent != "" || len(streamingTools) > 0 || (showReasoning && streamingReasoning != "") {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderStreamingTurn(streamingContent, streamingTools, streamingReasoning, showReasoning, streamMD, width, t))
	}

	return b.String()
}

// renderTurn styles a completed turn with its role label and markdown content.
func renderTurn(turn turnBlock, showReasoning bool, width int, t tuiTheme) string {
	contentWidth := max(width-4, 20)

	var rendered string
	switch turn.role {
	case tauchat.ChatRoleAssistant:
		rendered = renderAssistantContent(turn.content, contentWidth, t)
	default:
		rendered = t.StreamingText.Width(contentWidth).Render(turn.content)
	}
	rendered = appendObservabilitySections(rendered, turn.tools, turn.reasoning, showReasoning, contentWidth, t)

	border := renderTurnBorder(turn.role, contentWidth)
	return border + "\n" + rendered
}

// renderStreamingTurn renders the in-flight assistant response using
// incremental markdown rendering for non-thinking segments.
func renderStreamingTurn(
	content string,
	tools []toolActivity,
	reasoning string,
	showReasoning bool,
	streamMD *streamingMarkdown,
	width int,
	t tuiTheme,
) string {
	contentWidth := max(width-4, 20)
	border := renderTurnBorder(tauchat.ChatRoleAssistant, contentWidth)
	rendered := renderAssistantStreaming(content, streamMD, contentWidth, t)
	rendered = appendObservabilitySections(rendered, tools, reasoning, showReasoning, contentWidth, t)
	return border + "\n" + rendered
}

func appendObservabilitySections(
	rendered string,
	tools []toolActivity,
	reasoning string,
	showReasoning bool,
	width int,
	t tuiTheme,
) string {
	var b strings.Builder
	b.WriteString(rendered)
	if len(tools) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t.Dim.Render("Tools"))
		for _, tool := range tools {
			line := "• " + tool.toolName
			if tool.status != "" {
				line += " " + tool.status
			}
			if tool.duration != "" {
				line += " (" + tool.duration + ")"
			}
			if tool.argumentsSummary != "" {
				line += " args: " + tool.argumentsSummary
			}
			if tool.resultSummary != "" {
				line += " → " + tool.resultSummary
			}
			if tool.truncated {
				line += " [truncated]"
			}
			if tool.isError {
				b.WriteByte('\n')
				b.WriteString(t.Error.Width(width).Render(line))
			} else {
				b.WriteByte('\n')
				b.WriteString(t.Dim.Width(width).Render(line))
			}
		}
	}
	if showReasoning && reasoning != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t.Dim.Render("Reasoning"))
		b.WriteByte('\n')
		b.WriteString(t.Dim.Width(width).Render(reasoning))
	}
	return b.String()
}

// renderAssistantContent renders a completed assistant turn, dimming <think> blocks.
func renderAssistantContent(content string, width int, t tuiTheme) string {
	segments := splitThinkSegments(content)
	if len(segments) == 1 && !segments[0].thinking {
		return renderMarkdown(content, width)
	}

	var b strings.Builder
	for _, seg := range segments {
		if seg.text == "" {
			continue
		}
		if seg.thinking {
			dimmed := t.Dim.Width(width).Render(seg.text)
			b.WriteString(dimmed)
		} else {
			b.WriteString(renderMarkdown(seg.text, width))
		}
	}
	return b.String()
}

// renderAssistantStreaming renders in-flight content, dimming <think> blocks
// and using incremental markdown rendering for output segments.
func renderAssistantStreaming(content string, streamMD *streamingMarkdown, width int, t tuiTheme) string {
	segments := splitThinkSegments(content)
	if len(segments) == 1 && !segments[0].thinking {
		renderer := markdownRenderer(width)
		if renderer == nil {
			return t.StreamingText.Width(width).Render(content)
		}
		return streamMD.Render(content, width, renderer)
	}

	var b strings.Builder
	for _, seg := range segments {
		if seg.text == "" {
			continue
		}
		if seg.thinking {
			b.WriteString(t.Dim.Width(width).Render(seg.text))
		} else {
			renderer := markdownRenderer(width)
			if renderer == nil {
				b.WriteString(t.StreamingText.Width(width).Render(seg.text))
			} else {
				b.WriteString(streamMD.Render(seg.text, width, renderer))
			}
		}
	}
	return b.String()
}

// thinkSegment is a piece of content that is either thinking or output.
type thinkSegment struct {
	text     string
	thinking bool
}

// splitThinkSegments splits content into thinking and non-thinking segments.
// Handles <think>...</think> blocks. An unclosed <think> tag (streaming)
// treats the remainder as thinking content.
func splitThinkSegments(content string) []thinkSegment {
	var segments []thinkSegment
	remaining := content

	for remaining != "" {
		openIdx := strings.Index(remaining, "<think>")
		if openIdx == -1 {
			segments = append(segments, thinkSegment{text: remaining, thinking: false})
			break
		}

		// Text before the <think> tag.
		if openIdx > 0 {
			segments = append(segments, thinkSegment{text: remaining[:openIdx], thinking: false})
		}

		// Advance past the opening tag.
		remaining = remaining[openIdx+len("<think>"):]

		// Find closing tag.
		closeIdx := strings.Index(remaining, "</think>")
		if closeIdx == -1 {
			// Unclosed tag (still streaming thinking) — rest is thinking.
			segments = append(segments, thinkSegment{text: remaining, thinking: true})
			break
		}

		// Extract thinking content.
		segments = append(segments, thinkSegment{text: remaining[:closeIdx], thinking: true})
		remaining = remaining[closeIdx+len("</think>"):]
	}

	return segments
}

// renderTurnBorder returns a thin horizontal rule styled by role.
func renderTurnBorder(role tauchat.ChatRole, width int) string {
	line := strings.Repeat("─", max(width, 1))
	switch role {
	case tauchat.ChatRoleUser:
		return lipgloss.NewStyle().Foreground(theme.ColorNavyBlue).Render(line)
	case tauchat.ChatRoleAssistant:
		return lipgloss.NewStyle().Foreground(theme.ColorPurple).Render(line)
	default:
		return lipgloss.NewStyle().Foreground(theme.ColorDimGray).Render(line)
	}
}
