package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
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

	if c.handleTurnEvent(ev) {
		return
	}
	if c.handleExtensionEvent(ev) {
		return
	}
	c.handleSessionEvent(ev)
}

// handleTurnEvent dispatches streaming/turn-lifecycle events. Returns true
// when ev was handled. The caller holds c.mu; each handler releases and
// re-acquires it around engine calls (engine methods take the render lock and
// some call back into render paths).
func (c *inlineChat) handleTurnEvent(ev tauchat.ChatEvent) bool {
	switch e := ev.(type) {
	case tauchat.ChatReasoningDeltaEvent:
		c.handleReasoningDelta(e)
	case tauchat.ChatResponseDeltaEvent:
		c.handleResponseDelta(e)
	case tauchat.ChatToolExecutionStartedEvent:
		c.handleToolStarted(e)
	case tauchat.ChatToolExecutionCompletedEvent:
		c.handleToolCompleted(e)
	case tauchat.ChatToolOutputEvent:
		c.handleToolOutput(e)
	case tauchat.ChatResponseCompletedEvent:
		c.handleResponseCompleted(e)
	case tauchat.ChatResponseCancelledEvent:
		c.handleResponseCancelled(e)
	case tauchat.ChatRuntimeErrorEvent:
		c.handleRuntimeError(e)
	case tauchat.ChatNotificationEvent:
		c.handleNotificationEvent(e)
	default:
		return false
	}
	return true
}

// handleExtensionEvent dispatches extension/plugin events. Returns true when
// ev was handled. The caller holds c.mu.
func (c *inlineChat) handleExtensionEvent(ev tauchat.ChatEvent) bool {
	switch e := ev.(type) {
	case tauchat.ExtensionsReloadedEvent:
		c.handleExtensionsReloaded(e)
	case tauchat.ExtensionCommandsChangedEvent:
		c.handleExtensionCommandsChanged(e)
	case tauchat.CommandsChangedEvent:
		c.registryCommands = e.Commands
	case tauchat.ExtensionCommandResultEvent:
		c.handleExtensionCommandResult(e)
	case tauchat.ExtensionViewRenderedEvent:
		c.handleViewRendered(e)
	case tauchat.ExtensionViewClosedEvent:
		c.handleViewClosed(e)
	case tauchat.InteractivePromptRequestedEvent:
		c.handleInteractivePromptRequested(e)
	default:
		return false
	}
	return true
}

// handleSessionEvent dispatches session/skills events. The caller holds c.mu.
func (c *inlineChat) handleSessionEvent(ev tauchat.ChatEvent) {
	switch e := ev.(type) {
	case tauchat.SkillsChangedEvent:
		c.handleSkillsChanged(e)
	case tauchat.ChatSessionSnapshotEvent:
		c.syncState(e.State)
	case tauchat.SessionsListedEvent:
		c.handleSessionsListed(e)
	case tauchat.SessionLoadedEvent:
		c.handleSessionLoaded(e)
	case tauchat.SessionDeletedEvent:
		c.handleSessionDeleted(e)
	case tauchat.SearchResultsEvent:
		c.handleSearchResults(e)
	case tauchat.SessionExportedEvent:
		c.handleSessionExported(e)
	case tauchat.SessionSummarizedEvent:
		c.handleSessionSummarized(e)
	case tauchat.ConfigCheckedEvent:
		c.handleConfigChecked(e)
	}
}

// handleSessionSummarized prints the ephemeral /summarize result. The session
// history is untouched, so there is no state to sync. The caller holds c.mu.
func (c *inlineChat) handleSessionSummarized(e tauchat.SessionSummarizedEvent) {
	text := fmt.Sprintf("Summary (%d messages, history unchanged):\n\n%s", e.MessageCount, e.Summary)
	c.mu.Unlock()
	c.engine.PrintAbove("%s", c.grey(text))
	c.mu.Lock()
}

// handleConfigChecked prints the full /config check report - every issue, not
// just the first. The caller holds c.mu.
func (c *inlineChat) handleConfigChecked(e tauchat.ConfigCheckedEvent) {
	var b strings.Builder
	if len(e.Issues) == 0 {
		b.WriteString("Config check: no issues found.")
	} else {
		errCount, warnCount := 0, 0
		for _, issue := range e.Issues {
			if issue.IsError() {
				errCount++
			} else {
				warnCount++
			}
		}
		fmt.Fprintf(&b, "Config check: %d error(s), %d warning(s)", errCount, warnCount)
		for _, issue := range e.Issues {
			fmt.Fprintf(&b, "\n  [%s] %s", issue.Severity, issue.Message)
		}
	}
	c.mu.Unlock()
	c.engine.PrintAbove("%s", c.grey(b.String()))
	c.mu.Lock()
}

// ── Turn lifecycle handlers (caller holds c.mu) ────────────────────────────

func (c *inlineChat) handleReasoningDelta(e tauchat.ChatReasoningDeltaEvent) {
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
}

func (c *inlineChat) handleResponseDelta(e tauchat.ChatResponseDeltaEvent) {
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
}

func (c *inlineChat) handleToolStarted(e tauchat.ChatToolExecutionStartedEvent) {
	c.mu.Unlock()
	c.engine.UpdateThenPrint(func() []string { return c.startToolBox(e) })
	c.mu.Lock()
}

// startToolBox flushes any in-progress turn paragraphs to scrollback and mounts
// the new tool's box in the stage. Runs inside engine.UpdateThenPrint.
func (c *inlineChat) startToolBox(e tauchat.ChatToolExecutionStartedEvent) []string {
	// A tool call ends the text segment (if any) that preceded it -
	// the backend commits it as its own message and starts a fresh
	// one after the tool runs. Flush it to scrollback and reset so
	// the next ChatResponseDeltaEvent starts a new Paragraph instead
	// of silently concatenating onto this one with no separator.
	var above []string
	if c.turnReasoning != nil {
		if t := c.turnReasoning.Text(); t != "" {
			above = append(above, c.dim(t))
		}
		c.stage.RemoveChild(c.turnReasoning)
		c.turnReasoning = nil
	}
	if c.turnText != nil {
		if t := c.turnText.Text(); t != "" {
			above = append(above, t+"\n")
		}
		c.stage.RemoveChild(c.turnText)
		c.turnText = nil
	}

	label := e.ArgumentsSummary
	bgStatus := theme.ToolRunning
	if e.ToolName == skillToolName {
		label = skillStatusLabel(e.ArgumentsSummary)
		bgStatus = theme.SkillRunning
	}
	tr := taui.NewToolRow(e.ToolName, label)
	tail := taui.NewTailLog(toolTailLines, c.grey)
	tbox := taui.NewBox().Padding(2, 1).Bg(toolBoxBg(bgStatus)).ExpandW().Build()
	tbox.AddChild(tr)
	tbox.AddChild(tail)
	if c.activeTools == nil {
		c.activeTools = make(map[string]*activeToolBox)
	}
	c.activeTools[e.CallID] = &activeToolBox{row: tr, box: tbox, tail: tail}
	c.stage.AddChild(tbox)
	return above
}

func (c *inlineChat) handleToolCompleted(e tauchat.ChatToolExecutionCompletedEvent) {
	if e.CallID == c.bashCallID {
		c.bashCallID = ""
		c.bashRunning.Store(false)
	}
	c.mu.Unlock()
	c.engine.Update(func() {
		tb, ok := c.activeTools[e.CallID]
		if !ok {
			return
		}
		resolveToolRow(tb, e)
	})
	c.engine.RequestRender()
	time.Sleep(450 * time.Millisecond)

	// Render the resolved tool box, remove it from the live frame, and commit
	// it to scrollback - all ordered safely by UpdateThenPrint.
	c.engine.UpdateThenPrint(func() []string { return c.commitToolBox(e.CallID) })
	c.mu.Lock()
}

// commitToolBox renders the resolved tool box, removes it from the live frame,
// and returns it as a scrollback line. Runs inside engine.UpdateThenPrint.
func (c *inlineChat) commitToolBox(callID string) []string {
	tb, ok := c.activeTools[callID]
	if !ok {
		return nil
	}
	line := c.blockString(tb.box.Render(c.width()))
	c.stage.RemoveChild(tb.box)
	delete(c.activeTools, callID)
	return []string{line}
}

// resolveToolRow marks the tool row resolved (success/fail) and restyles its
// box; the streamed tail is dropped since it was only a live peek, not part of
// the permanent scrollback record.
func resolveToolRow(tb *activeToolBox, e tauchat.ChatToolExecutionCompletedEvent) {
	// ResultSummary preserves line breaks for tui2's expandable box;
	// this row renders as a single formatted line, so collapse them.
	detail := strings.Join(strings.Fields(e.ResultSummary), " ")
	if e.ToolName == skillToolName && !e.IsError {
		// Don't dump the skill instructions in the box; the model already
		// received them as the tool result. A clean "loaded" label keeps
		// the box scannable.
		detail = "loaded"
	}
	if e.IsError {
		detail = "failed"
		tb.row.Fail(detail)
		tb.box.SetBgFn(toolResultBg(theme.ToolFailed, e.ToolName))
	} else {
		tb.row.Succeed(detail)
		tb.box.SetBgFn(toolResultBg(theme.ToolSuccess, e.ToolName))
	}
	if tb.tail != nil {
		tb.box.RemoveChild(tb.tail)
		tb.tail = nil
	}
}

// toolResultBg picks the resolved-box background for a tool, using the skill
// palette when the executing tool is the Skill tool.
func toolResultBg(status theme.ToolStatus, toolName string) taui.BgFn {
	if toolName == skillToolName {
		return skillBoxBg(status)
	}
	return toolBoxBg(status)
}

func (c *inlineChat) handleToolOutput(e tauchat.ChatToolOutputEvent) {
	c.mu.Unlock()
	c.engine.Update(func() {
		tb, ok := c.activeTools[e.CallID]
		if !ok || tb.tail == nil {
			return
		}
		tb.tail.Append(e.Chunk)
	})
	c.mu.Lock()
}

func (c *inlineChat) handleResponseCompleted(e tauchat.ChatResponseCompletedEvent) {
	// Flush the turn's reasoning, body, and any in-flight tool boxes to
	// scrollback. UpdateThenPrint mutates the frame under the lock and
	// commits the returned lines after it releases - no PrintAbove-in-Update
	// deadlock.
	c.steering.Store(false)
	c.generating.Store(false)
	c.mu.Unlock()
	var finalText string
	c.engine.UpdateThenPrint(func() []string {
		above, text := c.flushTurn()
		finalText = text
		return above
	})
	if finalText != "" {
		c.mu.Lock()
		c.lastResponseText = finalText
		c.mu.Unlock()
		// Only nudge the user via desktop notification when they've
		// actually looked away - while focused, the response already
		// streamed onto their screen.
		if !c.engine.Focused() {
			c.engine.Terminal.Write(termkit.Notify("tau", finalText))
		}
	}
	c.mu.Lock()
}

// flushTurn commits the turn's reasoning, body, and any in-flight tool boxes
// to scrollback and clears the stage. Runs inside engine.UpdateThenPrint.
// Returns the scrollback lines and the assistant's final response text.
func (c *inlineChat) flushTurn() (above []string, finalText string) {
	if c.turnReasoning != nil {
		if t := c.turnReasoning.Text(); t != "" {
			above = append(above, c.dim(t))
		}
		c.turnReasoning = nil
	}
	if c.turnText != nil {
		if t := c.turnText.Text(); t != "" {
			above = append(above, t+"\n")
			finalText = t
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
	return above, finalText
}

func (c *inlineChat) handleResponseCancelled(e tauchat.ChatResponseCancelledEvent) {
	c.steering.Store(false)
	c.generating.Store(false)
	c.mu.Unlock()
	c.engine.Update(c.clearTurnLocked)
	c.engine.PrintAbove("%s\n", c.grey("chat request cancelled"))
	c.mu.Lock()
}

func (c *inlineChat) handleRuntimeError(e tauchat.ChatRuntimeErrorEvent) {
	c.steering.Store(false)
	c.generating.Store(false)
	if c.notifyQueue != nil {
		c.notifyQueue.Push(notify.Notification{
			Message:  e.Message,
			Level:    notify.LevelError,
			Duration: notifyDurationFromChat(tauchat.ChatNotificationError),
		})
	}
	c.mu.Unlock()
	c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
	c.engine.Update(c.clearTurnLocked)
	c.mu.Lock()
}

func (c *inlineChat) handleNotificationEvent(e tauchat.ChatNotificationEvent) {
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
}

// ── Extension/plugin handlers (caller holds c.mu) ───────────────────────────

func (c *inlineChat) handleExtensionsReloaded(e tauchat.ExtensionsReloadedEvent) {
	c.mu.Unlock()
	c.setExtensionCommands(e.Result.Commands)
	msg := fmt.Sprintf("reloaded extensions: %d loaded", e.Result.ExtensionCount)
	c.pushNotice(msg)
	c.engine.PrintAbove("%s", c.grey(msg))
	c.mu.Lock()
}

func (c *inlineChat) handleExtensionCommandsChanged(e tauchat.ExtensionCommandsChangedEvent) {
	c.mu.Unlock()
	c.setExtensionCommands(e.Commands)
	c.mu.Lock()
}

func (c *inlineChat) handleExtensionCommandResult(e tauchat.ExtensionCommandResultEvent) {
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
}

func (c *inlineChat) handleViewRendered(e tauchat.ExtensionViewRenderedEvent) {
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
}

func (c *inlineChat) handleViewClosed(e tauchat.ExtensionViewClosedEvent) {
	c.mu.Unlock()
	c.engine.Update(func() {
		if comp, ok := c.panelsByID[e.ViewID]; ok {
			c.panels.RemoveChild(comp)
			delete(c.panelsByID, e.ViewID)
		}
	})
	c.mu.Lock()
}

func (c *inlineChat) handleInteractivePromptRequested(e tauchat.InteractivePromptRequestedEvent) {
	c.mu.Unlock()
	c.enqueuePrompt(e)
	c.mu.Lock()
}

// ── Session/skills handlers (caller holds c.mu) ─────────────────────────────

func (c *inlineChat) handleSessionsListed(e tauchat.SessionsListedEvent) {
	c.sessionSummaries = e.Sessions
	c.mu.Unlock()
	c.printSessionSummaries(e.Sessions, e.NextCursor)
	c.mu.Lock()
}

func (c *inlineChat) handleSessionLoaded(e tauchat.SessionLoadedEvent) {
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
}

func (c *inlineChat) handleSessionDeleted(e tauchat.SessionDeletedEvent) {
	msg := "Session deleted: " + e.SessionID
	c.mu.Unlock()
	c.pushNotice(msg)
	c.engine.PrintAbove("%s", c.grey(msg))
	c.mu.Lock()
}

func (c *inlineChat) handleSearchResults(e tauchat.SearchResultsEvent) {
	c.mu.Unlock()
	c.printSearchResults(e)
	c.mu.Lock()
}

func (c *inlineChat) handleSessionExported(e tauchat.SessionExportedEvent) {
	msg := "Session exported to stdout"
	if e.Path != "" {
		msg = "Session exported to " + e.Path
	}
	c.mu.Unlock()
	c.pushNotice(msg)
	if e.Content != "" {
		c.engine.PrintAbove("%s", e.Content)
	}
	c.engine.PrintAbove("%s", c.grey(msg))
	c.mu.Lock()
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

// printSearchResults renders full-text search hits above the input: a header
// line, then per hit the session ID, role, and seq plus the snippet (matched
// terms wrapped in ** by the store).
func (c *inlineChat) printSearchResults(e tauchat.SearchResultsEvent) {
	if len(e.Hits) == 0 {
		c.pushNotice("no matches for " + strconv.Quote(e.Query))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d match(es) for %s:\n", len(e.Hits), strconv.Quote(e.Query))
	for _, h := range e.Hits {
		fmt.Fprintf(&b, "- %s · %s · seq %d\n  %s\n", h.SessionID, h.Role, h.Seq, h.Snippet)
	}
	if e.NextCursor != "" {
		b.WriteString("(more available)")
	}
	c.engine.PrintAbove("%s", c.grey(strings.TrimRight(b.String(), "\n")))
}

func (c *inlineChat) printSessionSummaries(summaries []tauchat.SessionSummary, nextCursor string) {
	if len(summaries) == 0 {
		c.engine.PrintAbove("%s", c.grey("Sessions: no saved sessions"))
		return
	}
	var b strings.Builder
	b.WriteString("Sessions:\n")
	for _, s := range summaries {
		title := s.Title
		if title == "" {
			title = shortID(s.ID)
		}
		fmt.Fprintf(&b, "- %s (%s) · %d messages · %s · %s\n",
			title, shortID(s.ID), s.MessageCount, s.ModelID, humanize.Time(s.UpdatedAt))
		if line := sessionSummaryMetricsLine(s); line != "" {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	if nextCursor != "" {
		b.WriteString("More sessions available.")
	}
	c.engine.PrintAbove("%s", c.grey(strings.TrimRight(b.String(), "\n")))
}

// sessionSummaryMetricsLine builds a compact single-line metrics summary for a
// session entry when at least one metric field is non-zero.
func sessionSummaryMetricsLine(s tauchat.SessionSummary) string {
	var parts []string
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		parts = append(parts, "↑"+humanizeTokens(s.InputTokens)+" ↓"+humanizeTokens(s.OutputTokens))
	}
	if s.Cost > 0 {
		parts = append(parts, formatCost(s.Cost))
	}
	if s.ToolCalls > 0 {
		toolStr := fmt.Sprintf("%d tools", s.ToolCalls)
		if s.ToolErrors > 0 {
			toolStr += fmt.Sprintf(" (%d err)", s.ToolErrors)
		}
		parts = append(parts, toolStr)
	}
	if s.DurationMs > 0 {
		parts = append(parts, formatDurationCompact(s.DurationMs))
	}
	return strings.Join(parts, " · ")
}

// formatDurationCompact renders milliseconds as a compact duration string.
func formatDurationCompact(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

// shortID trims a session UUID to its first 8 characters for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (c *inlineChat) printSessionInfo(s tauchat.SessionSummary) {
	var b strings.Builder
	fmt.Fprintf(&b, "Session %s (%s)\n", shortID(s.ID), s.ID)
	if s.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", s.Title)
	}
	fmt.Fprintf(&b, "Model: %s\n", s.ModelID)
	fmt.Fprintf(&b, "Provider: %s\n", s.Provider)
	fmt.Fprintf(&b, "Messages: %d\n", s.MessageCount)
	fmt.Fprintf(&b, "Created: %s\nLast accessed: %s\n", humanize.Time(s.CreatedAt), humanize.Time(s.UpdatedAt))
	if s.TotalTokens > 0 {
		fmt.Fprintf(&b, "Tokens: ↑%s ↓%s (total %s)\n",
			humanizeTokens(s.InputTokens), humanizeTokens(s.OutputTokens),
			humanizeTokens(s.TotalTokens))
	}
	if s.Cost > 0 {
		fmt.Fprintf(&b, "Cost: %s\n", formatCost(s.Cost))
	}
	if s.DurationMs > 0 {
		fmt.Fprintf(&b, "Duration: %s\n", formatDurationCompact(s.DurationMs))
	}
	if s.ToolCalls > 0 {
		fmt.Fprintf(&b, "Tool calls: %d", s.ToolCalls)
		if s.ToolErrors > 0 {
			fmt.Fprintf(&b, " (%d errors)", s.ToolErrors)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Created: %s\n", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Updated: %s", s.UpdatedAt.Format(time.RFC3339))
	c.engine.PrintAbove("%s", c.grey(b.String()))
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

// notifyDurationFromChat returns the auto-dismiss duration for a level.
// Errors persist (0); warnings get 8s; info gets 5s.
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
