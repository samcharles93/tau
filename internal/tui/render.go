package tui

import (
	"fmt"
	"strings"
	"time"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

func (c *ChatPanel) printAbovef(format string, args ...any) {
	if c.app == nil {
		return
	}
	c.app.PrintAboveln(format, args...)
}

func (c *ChatPanel) ensureStreamWriter() *gt.StreamWriter {
	if c.app == nil {
		return nil
	}
	if c.streamWriter == nil {
		c.streamWriter = c.app.StreamAbove()
	}
	return c.streamWriter
}

func (c *ChatPanel) writeAssistantDelta(delta string) {
	if delta == "" {
		return
	}
	w := c.ensureStreamWriter()
	if w == nil {
		return
	}
	write := func(s string) {
		_, _ = w.Write([]byte(s))
	}
	if !c.streamContentWritten && !c.reasoningWritten {
		write("\n")
	}
	if c.reasoningWritten && c.streamContentWritten {
		write("\n")
	}
	write(delta)
	c.streamContentWritten = true
}

func (c *ChatPanel) writeReasoningDelta(delta string) {
	if delta == "" {
		return
	}
	w := c.ensureStreamWriter()
	if w == nil {
		return
	}
	if !c.reasoningWritten {
		_, _ = w.Write([]byte("Reasoning: "))
		c.reasoningWritten = true
	}
	_, _ = w.Write([]byte(delta))
}

func (c *ChatPanel) closeStream() {
	if c.streamWriter != nil {
		if c.streamContentWritten || c.reasoningWritten {
			_, _ = c.streamWriter.Write([]byte("\n\n"))
		}
		_ = c.streamWriter.Close()
	}
	c.streamWriter = nil
	c.streamContentWritten = false
	c.reasoningWritten = false
}

func (c *ChatPanel) printLatestAssistantMessage(state tauchat.ChatSessionState) {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		if msg.Role == tauchat.ChatRoleAssistant && strings.TrimSpace(msg.Content) != "" {
			c.printMessage(msg)
			return
		}
	}
}

func (c *ChatPanel) printSessionMessages(messages []tauchat.ChatMessage) {
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" {
			continue
		}
		c.printMessage(msg)
	}
}

func (c *ChatPanel) printMessage(msg tauchat.ChatMessage) {
	if c.showReasoning.Get() && strings.TrimSpace(msg.ReasoningContent) != "" {
		c.printStyledAbovef("\n%s\n", ansify("reasoning:\n"+msg.ReasoningContent, theme.ColorDimGray))
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	switch msg.Role {
	case tauchat.ChatRoleUser:
		c.printStyledAbovef("%s", c.userMessageBlock(msg.Content))
	case tauchat.ChatRoleAssistant:
		c.printStyledAbovef("\n%s\n\n", msg.Content)
	default:
		c.printStyledAbovef("\n%s\n\n", ansify(fmt.Sprintf("%s: %s", messageRoleLabel(msg.Role), msg.Content), theme.ColorDimGray))
	}
}

func (c *ChatPanel) printStyledAbovef(format string, args ...any) {
	if c.app == nil {
		return
	}
	c.app.PrintAboveStyled(format, args...)
}

// ansify wraps text in ANSI SGR sequences matching the given go-tui Style.
// The result can be passed to PrintAboveStyled which preserves ANSI escapes.
func ansify(text string, c gt.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("\033[38;2;%d;%d;%dm%s\033[0m", r, g, b, text)
}

func (c *ChatPanel) userMessageBlock(text string) string {
	lines := wrapUserMessageLines(text, max(1, c.messageWidth()-2))
	rows := make([]string, 0, len(lines)+2)
	rows = append(rows, ansiBackgroundLine("", theme.ColorLightGray, theme.ColorGray800))
	for _, line := range lines {
		rows = append(rows, ansiBackgroundLine(" "+line, theme.ColorLightGray, theme.ColorGray800))
	}
	rows = append(rows, ansiBackgroundLine("", theme.ColorLightGray, theme.ColorGray800))
	return strings.Join(rows, "\n")
}

func (c *ChatPanel) messageWidth() int {
	if c.app != nil {
		if w, _ := c.app.Size(); w > 0 {
			return w
		}
		// Size() returned 0 despite a non-nil app; try the
		// terminal directly as a fallback.
		if w, _ := c.app.Terminal().Size(); w > 0 {
			return w
		}
	}
	// Last resort: a conservative default that prevents
	// messages from being narrower than a typical terminal.
	return 120
}

func wrapUserMessageLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		for len(runes) > width {
			// Prefer breaking at a word boundary so we
			// don't split words mid-grapheme.
			breakAt := width
			for i := width - 1; i >= width/2; i-- {
				if runes[i] == ' ' {
					breakAt = i + 1 // include the space on this line
					break
				}
			}
			out = append(out, strings.TrimRight(string(runes[:breakAt]), " "))
			runes = runes[breakAt:]
			// Drop leading spaces on continuation lines.
			for len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		}
		if len(runes) > 0 {
			out = append(out, string(runes))
		}
	}
	return out
}

func ansiBackgroundLine(text string, fg gt.Color, bg gt.Color) string {
	fr, fgG, fb := fg.RGB()
	br, bgG, bb := bg.RGB()
	return fmt.Sprintf("\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm%s\033[K\033[0m", fr, fgG, fb, br, bgG, bb, text)
}

func (c *ChatPanel) printSessionSummaries(summaries []tauchat.SessionSummary, nextCursor string) {
	if len(summaries) == 0 {
		c.printStyledAbovef("\n%s", ansify("Sessions: no saved sessions", theme.ColorDimGray))
		return
	}
	var b strings.Builder
	b.WriteString("Sessions:\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "- %s · %d messages · %s\n", summary.ID, summary.MessageCount, summary.ModelID)
	}
	if nextCursor != "" {
		b.WriteString("More sessions available.")
	}
	c.printStyledAbovef("\n%s", ansify(strings.TrimRight(b.String(), "\n"), theme.ColorDimGray))
}

func (c *ChatPanel) printSessionInfo(summary tauchat.SessionSummary) {
	c.printStyledAbovef("\n%s", ansify(fmt.Sprintf(
		"Session %s\nModel: %s\nProvider: %s\nMessages: %d\nTokens: %d\nCreated: %s\nUpdated: %s",
		summary.ID, summary.ModelID, summary.Provider,
		summary.MessageCount, summary.TotalTokens,
		summary.CreatedAt.Format(time.RFC3339),
		summary.UpdatedAt.Format(time.RFC3339),
	), theme.ColorDimGray))
}

func messageRoleLabel(role tauchat.ChatRole) string {
	switch role {
	case tauchat.ChatRoleTool:
		return "tool"
	case tauchat.ChatRoleSystem:
		return "system"
	default:
		return "message"
	}
}
