package tui

import (
	"fmt"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
)

// eventLoop bridges runtime chat events into the inline UI until the session
// closes.
func (c *inlineChat) eventLoop() {
	defer c.runtime.Close()
	for {
		select {
		case <-c.done:
			return
		case ev, ok := <-c.sub.Events():
			if !ok {
				return
			}
			c.handleEvent(ev)
		}
	}
}

// clearTurnLocked resets per-turn streaming state. Must be called inside an
// engine.Update closure (it mutates the render tree).
func (c *inlineChat) clearTurnLocked() {
	c.stage.Clear()
	c.turnText = nil
	c.turnReasoning = nil
	c.activeTools = nil
	c.header.SetText(c.grey(headerIdle))
}

func (c *inlineChat) handleEvent(ev tauchat.ChatEvent) {
	switch e := ev.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		c.syncState(e.State)

	case tauchat.ChatReasoningDeltaEvent:
		c.mu.Lock()
		show := c.showReasoning
		c.mu.Unlock()
		if !show {
			return
		}
		c.engine.Update(func() {
			if c.turnReasoning == nil {
				c.turnReasoning = taui.NewParagraph("")
				c.turnReasoning.SetStyle(c.dim)
				c.stage.Clear()
				c.stage.AddChild(c.turnReasoning)
			}
			c.turnReasoning.Append(e.Delta)
		})

	case tauchat.ChatResponseDeltaEvent:
		c.engine.Update(func() {
			if c.turnText == nil {
				c.turnText = taui.NewParagraph("")
				c.stage.Clear()
				c.stage.AddChild(c.turnText)
			}
			c.turnText.Append(e.Delta)
		})

	case tauchat.ChatToolExecutionStartedEvent:
		c.engine.Update(func() {
			tr := taui.NewToolRow(e.ToolName, e.ArgumentsSummary)
			tbox := taui.NewBox().Padding(2, 1).Bg(toolBoxBg(theme.ToolRunning)).ExpandW().Build()
			tbox.AddChild(tr)
			if c.activeTools == nil {
				c.activeTools = make(map[string]*activeToolBox)
			}
			c.activeTools[e.CallID] = &activeToolBox{row: tr, box: tbox}
			c.stage.AddChild(tbox)
		})

	case tauchat.ChatToolExecutionCompletedEvent:
		c.engine.Update(func() {
			tb, ok := c.activeTools[e.CallID]
			if !ok {
				return
			}
			detail := e.ResultSummary
			if e.IsError {
				detail = "failed"
				tb.row.Fail(detail)
				tb.box.SetBgFn(toolBoxBg(theme.ToolFailed))
			} else {
				tb.row.Succeed(detail)
				tb.box.SetBgFn(toolBoxBg(theme.ToolSuccess))
			}
		})
		c.engine.RequestRender()
		time.Sleep(450 * time.Millisecond)

		// Render the resolved tool box, remove it from the live frame, and commit
		// it to scrollback — all ordered safely by UpdateThenPrint.
		c.engine.UpdateThenPrint(func() []string {
			tb, ok := c.activeTools[e.CallID]
			if !ok {
				return nil
			}
			line := c.blockString(tb.box.Render(c.width()))
			c.stage.RemoveChild(tb.box)
			delete(c.activeTools, e.CallID)
			return []string{line}
		})

	case tauchat.ChatToolOutputEvent:
		c.engine.PrintAbove("%s", c.grey("  "+e.Chunk))

	case tauchat.ChatResponseCompletedEvent:
		// Flush the turn's reasoning, body, and any in-flight tool boxes to
		// scrollback. UpdateThenPrint mutates the frame under the lock and
		// commits the returned lines after it releases — no PrintAbove-in-Update
		// deadlock.
		c.engine.UpdateThenPrint(func() []string {
			var above []string
			if c.turnReasoning != nil {
				if t := c.turnReasoning.Text(); t != "" {
					above = append(above, c.dim(t))
				}
				c.turnReasoning = nil
			}
			if c.turnText != nil {
				if t := c.turnText.Text(); t != "" {
					above = append(above, t)
				}
				c.turnText = nil
			}
			for _, tb := range c.activeTools {
				above = append(above, c.blockString(tb.box.Render(c.width())))
				c.stage.RemoveChild(tb.box)
			}
			c.activeTools = nil
			c.stage.Clear()
			c.header.SetText(c.grey(headerIdle))
			return above
		})

	case tauchat.ChatResponseCancelledEvent:
		c.engine.Update(c.clearTurnLocked)
		c.engine.PrintAbove("%s", c.grey("chat request cancelled"))

	case tauchat.ChatRuntimeErrorEvent:
		c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
		c.engine.Update(c.clearTurnLocked)

	case tauchat.ChatNotificationEvent:
		c.mu.Lock()
		if c.notifyQueue != nil {
			c.notifyQueue.Push(notify.Notification{
				Message:  e.Message,
				Level:    notifyLevelFromChat(e.Level),
				Duration: notifyDurationFromChat(e.Level),
			})
		}
		c.mu.Unlock()
		if e.Level == tauchat.ChatNotificationError {
			c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
		}

	case tauchat.ExtensionsReloadedEvent:
		c.setExtensionCommands(e.Result.Commands)
		msg := fmt.Sprintf("reloaded extensions: %d loaded", e.Result.ExtensionCount)
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))

	case tauchat.ExtensionCommandsChangedEvent:
		c.setExtensionCommands(e.Commands)

	case tauchat.CommandsChangedEvent:
		c.mu.Lock()
		c.registryCommands = e.Commands
		c.mu.Unlock()

	case tauchat.ExtensionCommandResultEvent:
		if strings.TrimSpace(e.Output) != "" {
			c.engine.PrintAbove("%s", e.Output)
		}

	case tauchat.InteractivePromptRequestedEvent:
		msg := e.Title + ": " + e.Message
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))

	case tauchat.SessionsListedEvent:
		c.mu.Lock()
		c.sessionSummaries = e.Sessions
		c.mu.Unlock()
		c.printSessionSummaries(e.Sessions, e.NextCursor)

	case tauchat.SessionLoadedEvent:
		c.syncState(e.State)
		msg := fmt.Sprintf("Session %s loaded (%d messages)", e.State.SessionID, len(e.State.Messages))
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
		for _, m := range e.State.Messages {
			c.printMessage(m)
		}

	case tauchat.SessionDeletedEvent:
		msg := "Session deleted: " + e.SessionID
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))

	case tauchat.SessionExportedEvent:
		msg := "Session exported to stdout"
		if e.Path != "" {
			msg = "Session exported to " + e.Path
		}
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
	}
}

// ── State updates from events ─────────────────────────────────────────────────

func (c *inlineChat) syncState(state tauchat.ChatSessionState) {
	c.mu.Lock()
	if state.SessionID != "" {
		c.sessionID = state.SessionID
	}
	if state.Model.ID != "" {
		c.modelName = state.Model.ID
	}
	c.mu.Unlock()
}

func (c *inlineChat) setExtensionCommands(commands []tauchat.ExtensionCommand) {
	next := make(map[string]tauchat.ExtensionCommand, len(commands))
	for _, cmd := range commands {
		name := strings.TrimSpace(cmd.Name)
		if name == "" {
			continue
		}
		next[name] = cmd
	}
	c.mu.Lock()
	c.extensionCommands = next
	c.mu.Unlock()
}

// ── Scrollback rendering of events ────────────────────────────────────────────

func (c *inlineChat) printSessionSummaries(summaries []tauchat.SessionSummary, nextCursor string) {
	if len(summaries) == 0 {
		c.engine.PrintAbove("%s", c.grey("Sessions: no saved sessions"))
		return
	}
	var b strings.Builder
	b.WriteString("Sessions:\n")
	for _, s := range summaries {
		fmt.Fprintf(&b, "- %s · %d messages · %s\n", s.ID, s.MessageCount, s.ModelID)
	}
	if nextCursor != "" {
		b.WriteString("More sessions available.")
	}
	c.engine.PrintAbove("%s", c.grey(strings.TrimRight(b.String(), "\n")))
}

func (c *inlineChat) printSessionInfo(s tauchat.SessionSummary) {
	c.engine.PrintAbove("%s", c.grey(fmt.Sprintf(
		"Session %s\nModel: %s\nProvider: %s\nMessages: %d\nTokens: %d\nCreated: %s\nUpdated: %s",
		s.ID, s.ModelID, s.Provider, s.MessageCount, s.TotalTokens,
		s.CreatedAt.Format(time.RFC3339), s.UpdatedAt.Format(time.RFC3339),
	)))
}

func (c *inlineChat) printMessage(msg tauchat.ChatMessage) {
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	switch msg.Role {
	case tauchat.ChatRoleUser:
		c.engine.PrintAbove("%s", c.bold("› "+msg.Content))
	case tauchat.ChatRoleAssistant:
		c.engine.PrintAbove("%s", msg.Content)
	default:
		c.engine.PrintAbove("%s", c.grey(string(msg.Role)+": "+msg.Content))
	}
}

// notifyLevelFromChat maps a chat notification level to the notify package level.
func notifyLevelFromChat(level tauchat.ChatNotificationLevel) notify.Level {
	switch level {
	case tauchat.ChatNotificationError:
		return notify.LevelError
	case tauchat.ChatNotificationWarn:
		return notify.LevelWarn
	default:
		return notify.LevelInfo
	}
}

// notifyDurationFromChat returns the auto-dismiss duration for a level. Errors
// persist (0); warnings get 8s; info gets 5s.
func notifyDurationFromChat(level tauchat.ChatNotificationLevel) time.Duration {
	switch level {
	case tauchat.ChatNotificationError:
		return 0
	case tauchat.ChatNotificationWarn:
		return 8 * time.Second
	default:
		return 5 * time.Second
	}
}
