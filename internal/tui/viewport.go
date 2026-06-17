package tui

import (
	"strings"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// renderMessagesViewport builds the scrollable alternate-screen chat history.
// It renders persisted messages plus any active streaming content. Scroll
// position is preserved across re-renders and snaps to the bottom when new
// content arrives.
func (c *ChatPanel) renderMessagesViewport(app *gt.App) *gt.Element {
	width := max(
		// leave padding for readability
		c.messageWidth()-4, 1)

	viewport := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexGrow(1),
		gt.WithWidthPercent(100),
		gt.WithPadding(1),
		gt.WithGap(1),
		gt.WithScrollable(gt.ScrollVertical),
		gt.WithScrollbarHidden(true),
	)

	// Preserve scroll position across re-renders.
	if c.messageViewport != nil {
		x, y := c.messageViewport.ScrollOffset()
		viewport.ScrollTo(x, y)
	}
	c.messageViewport = viewport

	for _, msg := range c.messages.Get() {
		viewport.AddChild(c.renderViewportMessage(msg, width))
	}

	streaming := c.streamingContent.Get()
	if strings.TrimSpace(streaming) != "" {
		viewport.AddChild(c.renderViewportAssistantMessage(streaming, width, true))
	}

	for callID, log := range c.toolLogs.Get() {
		if strings.TrimSpace(log) != "" {
			viewport.AddChild(c.renderViewportToolLog(callID, log, width))
		}
	}

	if c.showReasoning.Get() {
		reasoning := c.streamingReasoning.Get()
		if strings.TrimSpace(reasoning) != "" {
			viewport.AddChild(c.renderViewportReasoning(reasoning, width, true))
		}
	}

	if c.scrollToBottomFlag {
		c.scrollToBottomFlag = false
		viewport.ScrollToBottom()
	}

	return viewport
}

func (c *ChatPanel) renderViewportMessage(msg tauchat.ChatMessage, width int) *gt.Element {
	switch msg.Role {
	case tauchat.ChatRoleUser:
		return c.renderViewportUserMessage(msg.Content, width)
	case tauchat.ChatRoleAssistant:
		container := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithGap(0),
		)
		if c.showReasoning.Get() && strings.TrimSpace(msg.ReasoningContent) != "" {
			container.AddChild(c.renderViewportReasoning(msg.ReasoningContent, width, false))
		}
		if strings.TrimSpace(msg.Content) != "" {
			container.AddChild(c.renderViewportAssistantMessage(msg.Content, width, false))
		}
		if len(container.Children()) == 0 {
			return gt.New()
		}
		return container
	default:
		return c.renderViewportSystemMessage(messageRoleLabel(msg.Role)+": "+msg.Content, width)
	}
}

func (c *ChatPanel) renderViewportUserMessage(text string, width int) *gt.Element {
	contentWidth := min(width, max(20, c.messageWidth()-8))
	row := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithWidthPercent(100),
	)
	row.AddChild(gt.New(gt.WithFlexGrow(1))) // left spacer

	bubble := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(contentWidth),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithBackground(theme.BodyStyle().Background(theme.ColorGray800)),
		gt.WithPadding(1),
		gt.WithGap(0),
	)
	bubble.AddChild(gt.New(
		gt.WithText(text),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithWrap(true),
	))
	row.AddChild(bubble)
	return row
}

func (c *ChatPanel) renderViewportAssistantMessage(text string, width int, streaming bool) *gt.Element {
	prefix := ""
	if streaming {
		prefix = "▌ "
	}
	return gt.New(
		gt.WithWidth(width),
		gt.WithComponent(gt.NewMarkdown(
			gt.WithMarkdownSource(prefix+text),
			gt.WithMarkdownWidth(width),
		)),
	)
}

func (c *ChatPanel) renderViewportToolLog(callID, log string, width int) *gt.Element {
	return gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(width),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.DimStyle()),
		gt.WithPadding(1),
		gt.WithChild(gt.New(
			gt.WithText("Tool Output ("+callID+"):"),
			gt.WithTextStyle(theme.DimStyle()),
		)),
		gt.WithChild(gt.New(
			gt.WithText(log),
			gt.WithTextStyle(theme.BodyStyle()),
			gt.WithWrap(true),
		)),
	)
}

func (c *ChatPanel) renderViewportReasoning(text string, width int, streaming bool) *gt.Element {
	prefix := "Reasoning: "
	if streaming {
		prefix = "Reasoning: ▌ "
	}
	return gt.New(
		gt.WithText(prefix+text),
		gt.WithTextStyle(theme.DescriptionStyle()),
		gt.WithWrap(true),
		gt.WithWidth(width),
	)
}

func (c *ChatPanel) renderViewportSystemMessage(text string, width int) *gt.Element {
	return gt.New(
		gt.WithText(text),
		gt.WithTextStyle(theme.DimStyle()),
		gt.WithWrap(true),
		gt.WithWidth(width),
	)
}
