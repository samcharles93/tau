package tui

import (
	"strings"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// renderMessagesViewport builds the scrollable alternate-screen chat history.
// It renders persisted messages plus any active streaming content.
//
// The viewport element is created once and reused across renders. go-tui
// rebuilds the entire element tree on every dirty frame, and the scroll offset
// (scrollX/scrollY) lives on the element — so creating a fresh element each
// render would reset the offset to the top on every frame (including the
// once-per-second notify timer tick). Reusing the same element preserves the
// offset; only its children are swapped.
//
// Scroll follows new content only when the user is already pinned at the bottom
// (sticky scroll). If they have scrolled up to read history, their position is
// left untouched.
func (c *ChatPanel) renderMessagesViewport(app *gt.App) *gt.Element {
	width := max(
		// leave padding for readability
		c.messageWidth()-4, 1)

	viewport := c.messageViewport
	firstRender := viewport == nil
	if firstRender {
		viewport = gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithFlexGrow(1),
			gt.WithWidthPercent(100),
			gt.WithPadding(1),
			gt.WithGap(1),
			gt.WithScrollable(gt.ScrollVertical),
			gt.WithScrollbarHidden(true),
		)
		c.messageViewport = viewport
	}

	// Decide whether to follow the bottom before mutating children, while the
	// previous frame's layout (and thus IsAtBottom) is still valid. A freshly
	// created viewport counts as "at the bottom" so the first frame starts there.
	stickToBottom := firstRender || viewport.IsAtBottom()

	viewport.RemoveAllChildren()

	for _, msg := range c.messages.Get() {
		viewport.AddChild(c.renderViewportMessage(app, msg, width))
	}

	streaming := c.streamingContent.Get()
	if strings.TrimSpace(streaming) != "" {
		viewport.AddChild(c.renderViewportAssistantMessage(app, streaming, width, true))
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

	if stickToBottom {
		viewport.ScrollToBottom()
	}

	return viewport
}

func (c *ChatPanel) renderViewportMessage(app *gt.App, msg tauchat.ChatMessage, width int) *gt.Element {
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
			container.AddChild(c.renderViewportAssistantMessage(app, msg.Content, width, false))
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

func (c *ChatPanel) renderViewportAssistantMessage(app *gt.App, text string, width int, streaming bool) *gt.Element {
	prefix := ""
	if streaming {
		prefix = "▌ "
	}
	return gt.NewMarkdown(
		gt.WithMarkdownSource(prefix+text),
		gt.WithMarkdownWidth(width),
		gt.WithMarkdownTheme(theme.MarkdownTheme()),
	).Render(app)
}

func (c *ChatPanel) renderViewportToolLog(callID, log string, width int) *gt.Element {
	container := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(width),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.DimStyle()),
		gt.WithPadding(1),
	)
	container.AddChild(gt.New(
		gt.WithText("Tool Output ("+callID+"):"),
		gt.WithTextStyle(theme.DimStyle()),
	))
	container.AddChild(gt.New(
		gt.WithText(log),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithWrap(true),
	))
	return container
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
