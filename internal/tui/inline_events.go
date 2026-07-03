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
	c.mu.Lock()
	defer c.mu.Unlock()

	switch e := ev.(type) {
	case tauchat.SkillsChangedEvent:
		c.handleSkillsChanged(e)
	case tauchat.ChatSessionSnapshotEvent:
		c.syncState(e.State)

	case tauchat.ChatReasoningDeltaEvent:
		show := c.showReasoning
		c.mu.Unlock()
		if !show {
			c.mu.Lock()
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
		c.mu.Lock()

	case tauchat.ChatResponseDeltaEvent:
		c.mu.Unlock()
		c.engine.Update(func() {
			if c.turnText == nil {
				c.turnText = taui.NewParagraph("")
				c.stage.Clear()
				c.stage.AddChild(c.turnText)
			}
			c.turnText.Append(e.Delta)
		})
		c.mu.Lock()

	case tauchat.ChatToolExecutionStartedEvent:
		c.mu.Unlock()
		c.engine.Update(func() {
			label := e.ArgumentsSummary
			bgStatus := theme.ToolRunning
			if e.ToolName == skillToolName {
				label = skillStatusLabel(e.ArgumentsSummary)
				bgStatus = theme.SkillRunning
			}
			tr := taui.NewToolRow(e.ToolName, label)
			tbox := taui.NewBox().Padding(2, 1).Bg(toolBoxBg(bgStatus)).ExpandW().Build()
			tbox.AddChild(tr)
			if c.activeTools == nil {
				c.activeTools = make(map[string]*activeToolBox)
			}
			c.activeTools[e.CallID] = &activeToolBox{row: tr, box: tbox}
			c.stage.AddChild(tbox)
		})
		c.mu.Lock()

	case tauchat.ChatToolExecutionCompletedEvent:
		c.mu.Unlock()
		c.engine.Update(func() {
			tb, ok := c.activeTools[e.CallID]
			if !ok {
				return
			}
			detail := e.ResultSummary
			if e.ToolName == skillToolName {
				// Don't dump the skill instructions in the box; the model already
				// received them as the tool result. A clean "loaded" label keeps
				// the box scannable.
				if !e.IsError {
					detail = "loaded"
				}
			}
			if e.IsError {
				detail = "failed"
				tb.row.Fail(detail)
				if e.ToolName == skillToolName {
					tb.box.SetBgFn(skillBoxBg(theme.SkillFailed))
				} else {
					tb.box.SetBgFn(toolBoxBg(theme.ToolFailed))
				}
			} else {
				tb.row.Succeed(detail)
				if e.ToolName == skillToolName {
					tb.box.SetBgFn(skillBoxBg(theme.SkillSuccess))
				} else {
					tb.box.SetBgFn(toolBoxBg(theme.ToolSuccess))
				}
			}
		})
		c.engine.RequestRender()
		time.Sleep(450 * time.Millisecond)
		c.mu.Lock()

		// Render the resolved tool box, remove it from the live frame, and commit
		// it to scrollback — all ordered safely by UpdateThenPrint.
		c.mu.Unlock()
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
		c.mu.Lock()

	case tauchat.ChatToolOutputEvent:
		c.mu.Unlock()
		c.engine.PrintAbove("%s", c.grey("  "+e.Chunk))
		c.mu.Lock()

	case tauchat.ChatResponseCompletedEvent:
		// Flush the turn's reasoning, body, and any in-flight tool boxes to
		// scrollback. UpdateThenPrint mutates the frame under the lock and
		// commits the returned lines after it releases — no PrintAbove-in-Update
		// deadlock.
		c.steering.Store(false)
		c.generating.Store(false)
		c.mu.Unlock()
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
					above = append(above, t+"\n")
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
		c.mu.Lock()

	case tauchat.ChatResponseCancelledEvent:
		c.steering.Store(false)
		c.generating.Store(false)
		c.mu.Unlock()
		c.engine.Update(c.clearTurnLocked)
		c.engine.PrintAbove("%s\n", c.grey("chat request cancelled"))
		c.mu.Lock()

	case tauchat.ChatRuntimeErrorEvent:
		c.steering.Store(false)
		c.generating.Store(false)
		c.mu.Unlock()
		c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
		c.engine.Update(c.clearTurnLocked)
		c.mu.Lock()

	case tauchat.ChatNotificationEvent:
		if c.notifyQueue != nil {
			c.notifyQueue.Push(notify.Notification{
				Message:  e.Message,
				Level:    notifyLevelFromChat(e.Level),
				Duration: notifyDurationFromChat(e.Level),
			})
		}
		if e.Level == tauchat.ChatNotificationError {
			c.mu.Unlock()
			c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
			c.mu.Lock()
		}

	case tauchat.ExtensionsReloadedEvent:
		c.mu.Unlock()
		c.setExtensionCommands(e.Result.Commands)
		msg := fmt.Sprintf("reloaded extensions: %d loaded", e.Result.ExtensionCount)
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
		c.mu.Lock()

	case tauchat.ExtensionCommandsChangedEvent:
		c.mu.Unlock()
		c.setExtensionCommands(e.Commands)
		c.mu.Lock()

	case tauchat.CommandsChangedEvent:
		c.registryCommands = e.Commands

	case tauchat.ExtensionCommandResultEvent:
		// A view, when present, replaces the plain-text output. Sync command
		// views are one-shot (commands only run while idle - see
		// handleRunExtensionCommand's isIdle guard) and aren't part of a
		// streaming turn, so they render straight to scrollback rather than
		// mounting into c.stage, mirroring the plain-text path exactly.
		if e.View != nil {
			c.mu.Unlock()
			lines := buildViewComponent(*e.View).Render(c.width())
			if len(lines) > 0 {
				c.engine.PrintAbove("%s", c.blockString(lines))
			}
			c.mu.Lock()
		} else if strings.TrimSpace(e.Output) != "" {
			c.mu.Unlock()
			c.engine.PrintAbove("%s", e.Output)
			c.mu.Lock()
		}

	case tauchat.ExtensionViewRenderedEvent:
		c.mu.Unlock()
		c.engine.Update(func() {
			comp := buildViewComponent(e.View)
			if old, ok := c.panelsByID[e.ViewID]; ok {
				c.panels.RemoveChild(old)
			}
			c.panelsByID[e.ViewID] = comp
			c.panels.AddChild(comp)
		})
		c.mu.Lock()

	case tauchat.ExtensionViewClosedEvent:
		c.mu.Unlock()
		c.engine.Update(func() {
			if comp, ok := c.panelsByID[e.ViewID]; ok {
				c.panels.RemoveChild(comp)
				delete(c.panelsByID, e.ViewID)
			}
		})
		c.mu.Lock()

	case tauchat.InteractivePromptRequestedEvent:
		c.mu.Unlock()
		c.enqueuePrompt(e)
		c.mu.Lock()

	case tauchat.SessionsListedEvent:
		c.sessionSummaries = e.Sessions
		c.mu.Unlock()
		c.printSessionSummaries(e.Sessions, e.NextCursor)
		c.mu.Lock()

	case tauchat.SessionLoadedEvent:
		c.syncState(e.State)
		msg := fmt.Sprintf("Session %s loaded (%d messages)", e.State.SessionID, len(e.State.Messages))
		c.mu.Unlock()
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
		for _, m := range e.State.Messages {
			c.printMessage(m)
		}
		// Seed the input history from persisted user messages.
		c.seedHistoryFromMessages(e.State.Messages)
		c.mu.Lock()

	case tauchat.SessionDeletedEvent:
		msg := "Session deleted: " + e.SessionID
		c.mu.Unlock()
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
		c.mu.Lock()

	case tauchat.SessionExportedEvent:
		msg := "Session exported to stdout"
		if e.Path != "" {
			msg = "Session exported to " + e.Path
		}
		c.mu.Unlock()
		c.pushNotice(msg)
		c.engine.PrintAbove("%s", c.grey(msg))
		c.mu.Lock()
	}
}

// handleSkillsChanged displays the list of available skills in the TUI.
func (c *inlineChat) handleSkillsChanged(e tauchat.SkillsChangedEvent) {
	if len(e.Skills) == 0 {
		c.mu.Unlock()
		c.engine.PrintAbove("%s", c.grey("no skills available"))
		c.mu.Lock()
		return
	}

	var b strings.Builder
	b.WriteString("Available Skills:")
	for _, skill := range e.Skills {
		fmt.Fprintf(&b, "\n  %-20s %s", skill.Name, skill.Description)
		if skill.Scope != "" {
			fmt.Fprintf(&b, " (%s)", skill.Scope)
		}
	}
	c.mu.Unlock()
	c.engine.PrintAbove("%s", c.grey(b.String()))
	c.mu.Lock()
}

// ── State updates from events ─────────────────────────────────────────────────

func (c *inlineChat) syncState(state tauchat.ChatSessionState) {
	if state.SessionID != "" {
		c.sessionID = state.SessionID
	}
	if state.Model.ID != "" {
		c.modelName = state.Model.ID
	}
	if state.ProviderName != "" {
		c.provider = state.ProviderName
	}
}

// setExtensionCommands updates the extension command registry.
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
		c.engine.PrintAbove("%s\n", msg.Content)
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
