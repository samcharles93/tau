package tui2

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// handleChatEvent mutates model state and returns an optional Cmd (e.g. for
// notification-clear timers or auto-responses). It MUST be called from within
// Update so the returned Cmd is properly composed.
func (m *model) handleChatEvent(evt tauchat.ChatEvent) tea.Cmd {
	switch e := evt.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		m.applySnapshot(e)

	case tauchat.ChatResponseStartedEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.toolGroupCollapsed = m.toolCallsDefaultCollapsed
		m.inResponse = true
		m.spinnerFrame = 0
		m.agentState = agentThinking
		return spinTick()

	case tauchat.ChatResponseDeltaEvent:
		// New text after a tool-calling batch means that batch is over -
		// commit it now so it lands in scrollback before (not after) this
		// text, and so the live tool-call box doesn't linger below text that
		// chronologically comes after it.
		if len(m.tools) > 0 {
			m.flushToolGroup()
		}
		if m.agentState != agentStreaming {
			m.agentState = agentStreaming
			m.streamStartedAt = time.Now()
		}
		m.streaming += e.Delta

	case tauchat.ChatReasoningDeltaEvent:
		m.reasoning += e.Delta
		// Some providers can stream reasoning across tool boundaries. Keep the
		// active tool visible until every tool in this batch has settled.
		if !m.anyToolRunning() {
			m.agentState = agentThinking
		}

	case tauchat.ChatToolCallDeltaEvent:
		m.adoptStreamedToolCallID(e.CallID, e.Index)
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary, "")

	case tauchat.ChatToolExecutionStartedEvent:
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary, e.Summary)
		m.setToolStatus(e.CallID, "running")
		if e.ToolName == "shell" {
			m.toolGroupCollapsed = false
			m.expandedID = e.CallID
		}
		m.agentState = agentRunningTool

	case tauchat.ChatToolExecutionCompletedEvent:
		m.adoptToolCallID(e.CallID, e.ToolName)
		status := "done"
		if e.IsError {
			status = "error"
		}
		m.setToolStatus(e.CallID, status)
		if e.ResultSummary != "" || e.Details != nil {
			m.finalizeToolResult(e.CallID, e.ResultSummary, e.Details)
		}
		// Extract child agent terminal state from the agent tool result.
		if e.ToolName == "agent" && e.Details != nil {
			if child, ok := extractChildAgentResult(e.Details); ok {
				m.childAgents[e.CallID] = child
				m.recordChildAgentOrder(e.CallID)
			}
		}
		if m.bashCallID != "" && e.CallID == m.bashCallID {
			m.bashRunning = false
			m.bashCallID = ""
		}
		// Show a neutral processing state once every tool in this batch has
		// settled. A later reasoning delta transitions explicitly to Thinking;
		// tool completion alone is not evidence that the model is reasoning.
		// a still-running sibling call means the status bar should keep
		// showing "Running <tool>", not flicker to Thinking and back.
		if !m.anyToolRunning() {
			m.agentState = agentProcessing
		}

	case tauchat.ChildAgentStateEvent:
		// Live child agent state update (per 05-ui.md). Terminal
		// states from the event stream update the summary; the UI
		// collapses working states and shows terminal summaries.
		// ChildAgentStateEvent never carries a session ID (only the
		// completed-tool-call details JSON does, via extractChildAgentResult
		// above) - preserve any sessionID already recorded rather than
		// clobbering it with a live update that arrives after the terminal
		// one (ordering isn't guaranteed across event types).
		sessionID := m.childAgents[e.CallID].sessionID
		m.childAgents[e.CallID] = childAgentResult{
			instanceID: e.InstanceID,
			specName:   e.SpecName,
			status:     e.Status,
			activity:   e.Activity,
			turns:      e.Turns,
			tokens:     e.InputTokens + e.OutputTokens,
			durationMs: e.ElapsedMs,
			errorMsg:   e.Error,
			sessionID:  sessionID,
		}
		m.recordChildAgentOrder(e.CallID)

	case tauchat.ChatToolOutputEvent:
		m.setToolResult(e.CallID, e.Chunk)
		m.appendToolTail(e.CallID, e.Chunk)

	case tauchat.ChatResponseCompletedEvent:
		content := m.finalizeResponse(lastAssistantMessageID(e.State))
		m.agentState = agentReady
		// noopCmd guarantees a non-nil Batch even when there's no queued turn
		// to drain and no desktop notification to fire.
		cmds := []tea.Cmd{noopCmd, m.drainTurnQueue()}
		// Only nudge the user via desktop notification when they've
		// actually looked away - while focused, the response already
		// streamed onto their screen. Matches internal/tui/inline_events.go.
		if content != "" && !m.focused {
			cmds = append(cmds, tea.Raw(termkit.Notify("tau", content)))
		}
		return tea.Batch(cmds...)

	case tauchat.ChatResponseCancelledEvent:
		m.flushInterruptedTurn("chat request cancelled")
		m.inResponse = false
		m.steering = false
		m.agentState = agentCancelled
		return m.drainTurnQueue()

	case tauchat.ChatRuntimeErrorEvent:
		// A failed background session-list prefetch (see
		// maybePrefetchSessions) must not leave sessionsFetchInFlight stuck
		// true forever with no retry - harmless no-op otherwise.
		m.sessionsFetchInFlight = false
		// A failed child-transcript load is not a failed chat turn - it must
		// not fall through into the turn-interruption logic below. Close the
		// stuck "Loading…" overlay (if this error matches it) and notify,
		// same stale-response guard as ChildTranscriptLoadedEvent.
		if m.childTranscriptViewer != nil && m.childTranscriptViewer.loading && m.childTranscriptViewer.sessionID == e.SessionID {
			m.childTranscriptViewer = nil
			return m.setNotification("failed to load transcript: " + e.Message)
		}
		// The turn cannot continue, but any text/tools that already streamed
		// are still useful scrollback. Commit them before clearing the live
		// buffers so failed in-flight tool groups do not vanish when the next
		// prompt starts. hadLoneTool is captured before flushing (which
		// clears m.tools) so the appendMessage call below knows whether that
		// commit already put this exact reason in scrollback - a lone tool
		// box (renderCommittedGroup/renderToolBox: len(tools)==1) shows a
		// compact result summary inline with nothing to expand, so it
		// already displays the reason; a MULTI-tool group instead collapses
		// to a bare count ("2 tool calls (1 error)") until clicked open, so
		// the reason isn't visible there without the echo below.
		hadLoneTool := len(m.tools) == 1
		m.flushInterruptedTurn(e.Message)
		m.steering = false
		m.inResponse = false
		// Shown via the notification banner (above the input area) at error
		// level, auto-clearing after notificationClearDelay like any other
		// notification - the scrollback line below is the permanent record,
		// so the banner doesn't need to persist until dismissed too. Also
		// printed to scrollback so it isn't lost once the banner clears -
		// but only when nothing else in scrollback already shows it (see
		// hadLoneTool above). Truncated: some providers return an unbounded
		// error body (e.g. a full HTML edge error page) that would
		// otherwise flood scrollback and bury the prompt (N-something).
		msg := truncateErrorText(e.Message)
		m.agentState = agentError
		cmd := m.setNotificationWithLevel(msg, notify.LevelError, notificationClearDelay)
		if !hadLoneTool {
			m.appendMessage("system", "✗ "+msg)
		}
		return cmd

	case tauchat.ChatNotificationEvent:
		// Covers the "session persistence unavailable" path a background
		// session-list prefetch can hit - see the ChatRuntimeErrorEvent case
		// above for why this reset matters.
		m.sessionsFetchInFlight = false
		msg := truncateErrorText(e.Message)
		cmd := m.setNotificationWithLevel(msg, notifyLevelFromChat(e.Level), notifyDurationFromChat(e.Level))
		if e.Level == tauchat.ChatNotificationError {
			m.appendMessage("system", msg)
		}
		return cmd

	case tauchat.InteractivePromptRequestedEvent:
		return m.enqueuePrompt(e)

	// Session events.
	case tauchat.SessionsListedEvent:
		m.sessionSummaries = e.Sessions
		m.sessionsFetchInFlight = false
		if e.Silent {
			// A background cache-refresh for the /session and /resume
			// argument-completion dropdowns (see maybePrefetchSessions) -
			// not a user-facing "/session list", so nothing prints to
			// scrollback. The dropdown picks up m.sessionSummaries on its
			// own next render.
			return nil
		}
		m.appendMessage("system", sessionSummariesText(e.Sessions, e.NextCursor))
		return nil

	case tauchat.SessionLoadedEvent:
		// Reuses the same state-sync + message-replay logic as an initial
		// snapshot - mirrors internal/tui/inline_chat.go's syncState +
		// printMessage-per-message replay, so a loaded session's history is
		// actually visible instead of just a notification.
		m.applySnapshot(tauchat.ChatSessionSnapshotEvent(e))
		m.seedHistoryFromMessages(e.State.Messages)
		return m.setNotification(fmt.Sprintf("Session %s loaded (%d messages)", e.State.SessionID, len(e.State.Messages)))

	case tauchat.ChildTranscriptLoadedEvent:
		// Guard against a stale response: the user may have closed the
		// overlay, or closed-and-reopened it on a different child, before
		// this async load returned.
		if m.childTranscriptViewer == nil || m.childTranscriptViewer.sessionID != e.SessionID {
			return nil
		}
		boxW := max(20, int(float64(m.width)*diffViewerWidthFrac))
		innerWidth := max(20, boxW-4)
		lines := m.renderChildTranscriptLines(e.Messages, innerWidth)
		m.childTranscriptViewer.viewport.SetContent(strings.Join(lines, "\n"))
		m.childTranscriptViewer.loading = false
		return nil

	case tauchat.SessionDeletedEvent:
		return m.setNotification("session deleted: " + e.SessionID)

	case tauchat.SessionExportedEvent:
		return m.setNotification("session exported: " + e.Path)

	// Extension / plugin events.
	case tauchat.ExtensionsReloadedEvent:
		return m.setNotification(fmt.Sprintf("extensions: %d loaded, %d diagnostics", e.Result.ExtensionCount, len(e.Result.Diagnostics)))

	case tauchat.ExtensionCommandsChangedEvent:
		m.extensionCommands = make(map[string]tauchat.ExtensionCommand, len(e.Commands))
		for _, ext := range e.Commands {
			m.extensionCommands[ext.Name] = ext
		}
		return nil

	case tauchat.CommandsChangedEvent:
		// Registry commands changed - no-op for now (completion picks them up
		// from slashTable which is static).
		return nil

	case tauchat.ExtensionCommandResultEvent:
		if e.Output != "" {
			m.appendMessage("system", e.Output)
		}
		if e.View != nil {
			text := renderPluginView(*e.View)
			if e.View.Title != "" {
				text = e.View.Title + "\n" + text
			}
			m.appendMessage("system", text)
		}
		return nil

	case tauchat.ExtensionViewRenderedEvent:
		m.panels[e.ViewID] = pluginPanel{
			id:      e.ViewID,
			title:   e.View.Title,
			content: renderPluginView(e.View),
		}
		return nil

	case tauchat.ExtensionViewClosedEvent:
		delete(m.panels, e.ViewID)
		return nil

	// Skills events. Rendered through glamour (like assistant markdown)
	// rather than appendMessage, so long descriptions wrap with a hanging
	// indent instead of spilling into one unstyled blob - see
	// skillsChangedText.
	case tauchat.SkillsChangedEvent:
		rendered := m.renderMarkdown(skillsChangedText(e.Skills), m.width)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		m.viewport.SetContentLines(m.renderedLines)
		return nil

	default:
		return nil
	}
	return nil
}

func (m *model) applySnapshot(e tauchat.ChatSessionSnapshotEvent) {
	state := e.State
	if state.SessionID != "" {
		m.sessionID = state.SessionID
	}
	if state.Model.ID != "" {
		m.modelName = state.Model.ID
	}
	if state.ProviderName != "" {
		m.provider = state.ProviderName
	}
	// N15: statusText consistently means "model @ provider"; never carry
	// error text here - errors go through m.notification.
	m.statusText = fmt.Sprintf("%s @ %s", m.modelName, m.provider)

	// Rebuild viewport content from session history. Snapshots arrive
	// routinely (not just on session load/resume) - e.g. after every
	// submitted prompt - so this must reproduce finalizeResponse's/
	// flushToolGroup's rendering exactly, or an already-committed message
	// reverts to raw/unbatched form the next time a snapshot rebuilds the
	// viewport.
	//
	// toolCallsByID tracks each assistant tool_call's name/arguments so a
	// later ChatRoleTool message (which only carries the call's ID and
	// result) can be rendered as a real tool call instead of being dropped -
	// history has no per-call elapsed time or error flag, so replayed calls
	// always show status "done" with no timing. pendingTools batches a run of
	// consecutive tool results (uninterrupted by user/assistant text) so a
	// turn with many tool calls renders as one compact group instead of
	// burying the surrounding conversation.
	// oldGroups lets a fold/row-expand the user made on a committed group
	// survive this rebuild - without it, every submitted prompt (which
	// triggers a fresh snapshot) would silently refold anything they'd
	// opened. Keyed by committedGroupKey, which stays stable across
	// rebuilds (see its doc comment).
	oldGroups := make(map[string]*committedToolGroup, len(m.committedGroups))
	for _, g := range m.committedGroups {
		oldGroups[committedGroupKey(g.tools)] = g
	}
	m.committedGroups = m.committedGroups[:0]

	// oldReasoning is the same fold-survival mechanism as oldGroups, for
	// completed reasoning blocks - keyed by committedReasoningKey.
	oldReasoning := make(map[string]*committedReasoningBlock, len(m.committedReasoning))
	for _, b := range m.committedReasoning {
		oldReasoning[b.key] = b
	}
	m.committedReasoning = m.committedReasoning[:0]
	m.lastReasoningKey = ""

	toolCallsByID := map[string]tauchat.ChatToolCall{}
	var pendingTools []toolState
	flushPendingTools := func() {
		if len(pendingTools) == 0 {
			return
		}
		m.commitToolGroup(pendingTools, oldGroups)
		pendingTools = nil
	}

	m.renderedLines = m.renderedLines[:0]
	m.messageRanges = m.messageRanges[:0]
	m.viewportSel.clear()
	for idx, msg := range state.Messages {
		switch msg.Role {
		case tauchat.ChatRoleUser:
			if cmd, output, ok := parseBashHistoryMessage(msg.Content); ok {
				pendingTools = append(pendingTools, toolState{
					// A stable, index-derived ID (not uuid.NewString()) -
					// this entry has no real call ID, and committedGroupKey
					// needs the same ID back on every rebuild for fold
					// state to carry over instead of resetting.
					id:     fmt.Sprintf("bash-%d", idx),
					name:   "shell",
					args:   cmd,
					result: output,
					status: "done",
				})
				continue
			}
			flushPendingTools()
			start := len(m.renderedLines)
			lines := strings.Split(msg.Content, "\n")
			for i, l := range lines {
				if i == 0 {
					m.renderedLines = append(m.renderedLines, renderLine("user", l))
				} else {
					m.renderedLines = append(m.renderedLines, renderContinuationLine("user", l))
				}
			}
			if msg.ID != "" {
				m.messageRanges = append(m.messageRanges, messageLineRange{id: msg.ID, content: msg.Content, startLine: start, endLine: len(m.renderedLines)})
			}
		case tauchat.ChatRoleAssistant:
			// Reasoning is persisted per-message (ChatMessage.ReasoningContent)
			// specifically so it survives a snapshot rebuild - without this,
			// finalizeResponse's live-turn commit would render fine once, then
			// vanish the moment the next prompt triggered a rebuild here, since
			// this function is the sole source of truth for renderedLines.
			if msg.ReasoningContent != "" && m.showReasoning {
				flushPendingTools()
				key := committedReasoningKey(msg.ID, idx)
				m.commitReasoningBlock(msg.ReasoningContent, key, msg.Content != "", oldReasoning)
			}
			if msg.Content != "" {
				flushPendingTools()
				start := len(m.renderedLines)
				m.lastAssistantText = msg.Content
				rendered := m.renderMarkdown(msg.Content, m.width)
				m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
				if msg.ID != "" {
					m.messageRanges = append(m.messageRanges, messageLineRange{id: msg.ID, content: msg.Content, startLine: start, endLine: len(m.renderedLines)})
				}
			}
			for _, tc := range msg.ToolCalls {
				toolCallsByID[tc.ID] = tc
			}
		case tauchat.ChatRoleTool:
			tc := toolCallsByID[msg.ToolCallID]
			pendingTools = append(pendingTools, toolState{
				id:     msg.ToolCallID,
				name:   tc.Function.Name,
				args:   tc.Function.Arguments,
				result: msg.Content,
				status: "done",
			})
		default:
			continue
		}
	}
	flushPendingTools()
	m.viewport.SetContentLines(m.renderedLines)
}

// renderChildTranscriptLines renders a finished child agent's full message
// history for the transcript drill-down overlay (childtranscript.go). It is
// a local, non-mutating counterpart to applySnapshot: applySnapshot cannot
// be reused directly because it mutates live model state (m.renderedLines,
// m.committedGroups, fold-state maps) via struct-copy-unsafe map aliasing -
// see the CAT-65 plan. Tool call groups always render collapsed (no
// accordion state needed for a static, read-only overlay); reasoning always
// renders expanded (no fold state to restore).
func (m *model) renderChildTranscriptLines(messages []tauchat.ChatMessage, width int) []string {
	var lines []string
	toolCallsByID := map[string]tauchat.ChatToolCall{}
	var pendingTools []toolState
	flushPendingTools := func() {
		if len(pendingTools) == 0 {
			return
		}
		if len(pendingTools) == 1 {
			lines = append(lines, m.renderToolBox(pendingTools[0], false, 1, width))
		} else {
			lines = append(lines, renderCommittedToolsSummary(pendingTools))
		}
		pendingTools = nil
	}

	for _, msg := range messages {
		switch msg.Role {
		case tauchat.ChatRoleUser:
			if cmd, output, ok := parseBashHistoryMessage(msg.Content); ok {
				pendingTools = append(pendingTools, toolState{
					id:     fmt.Sprintf("bash-%d", len(pendingTools)),
					name:   "shell",
					args:   cmd,
					result: output,
					status: "done",
				})
				continue
			}
			flushPendingTools()
			for i, l := range strings.Split(msg.Content, "\n") {
				if i == 0 {
					lines = append(lines, renderLine("user", l))
				} else {
					lines = append(lines, renderContinuationLine("user", l))
				}
			}
		case tauchat.ChatRoleAssistant:
			if msg.ReasoningContent != "" && m.showReasoning {
				flushPendingTools()
				lines = append(lines, renderReasoningLines(msg.ReasoningContent, width)...)
			}
			if msg.Content != "" {
				flushPendingTools()
				rendered := m.renderMarkdown(msg.Content, width)
				lines = append(lines, strings.Split(rendered, "\n")...)
			}
			for _, tc := range msg.ToolCalls {
				toolCallsByID[tc.ID] = tc
			}
		case tauchat.ChatRoleTool:
			tc := toolCallsByID[msg.ToolCallID]
			pendingTools = append(pendingTools, toolState{
				id:     msg.ToolCallID,
				name:   tc.Function.Name,
				args:   tc.Function.Arguments,
				result: msg.Content,
				status: "done",
			})
		default:
			continue
		}
	}
	flushPendingTools()
	return lines
}

// finalizeResponse returns the finalized assistant text (empty if none) so
// the caller can decide whether to fire a desktop notification. id, if
// non-empty, is the just-completed assistant ChatMessage's ID (see
// lastAssistantMessageID) - recorded into messageRanges the same way
// applySnapshot records every other message's range, so this message is
// immediately right-clickable without waiting for the next snapshot rebuild.
func (m *model) finalizeResponse(id string) string {
	// A turn that ends purely on tool calls (no trailing text) still needs
	// its tools committed to scrollback - flushToolGroup renders the real
	// group instead of a synthetic placeholder, so the user sees exactly
	// what ran.
	if len(m.tools) > 0 {
		m.failUnfinishedTools("inference ended before tool execution completed")
		m.flushToolGroup()
	}
	// Commit reasoning to scrollback before the final answer, styled the
	// same as the live reasoning view. Previously reasoning only ever
	// existed in the live view and was discarded here unread - on a fast
	// model, reasoning and the answer can both finish inside a second, so
	// it would flash on screen and vanish before anyone could read it.
	//
	// autoCollapse: fold the block automatically only when the turn also
	// produced a trailing answer - final-answer generation actually began,
	// so there's an answer for the block to sit above once folded. A
	// reasoning-only turn (no answer) stays expanded, matching the existing
	// "reasoning-only" regression behavior.
	if m.reasoning != "" && m.showReasoning {
		if id == "" {
			m.reasoningKeySeq++
		}
		key := committedReasoningKey(id, m.reasoningKeySeq)
		m.commitReasoningBlock(m.reasoning, key, m.streaming != "", nil)
	}
	content := m.streaming
	if content != "" {
		// Store raw markdown for /copy before glamour renders it.
		m.lastAssistantText = content
		// Render through glamour (markdown → ANSI) and append to viewport.
		// Glamour output is already fully styled, so each line goes
		// directly into renderedLines without passing through renderLine().
		start := len(m.renderedLines)
		rendered := m.renderMarkdown(content, m.width)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		if id != "" {
			m.messageRanges = append(m.messageRanges, messageLineRange{id: id, content: content, startLine: start, endLine: len(m.renderedLines)})
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
	m.streaming = ""
	m.reasoning = ""
	m.inResponse = false
	return content
}

// lastAssistantMessageID returns the ID of state's final message if it's the
// assistant turn CompleteTurnWithReasoning just appended, or "" if that turn
// ended purely on tool calls (no trailing assistant text message).
func lastAssistantMessageID(state tauchat.ChatSessionState) string {
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == tauchat.ChatRoleAssistant {
		return state.Messages[n-1].ID
	}
	return ""
}

// mdCacheWidth normalizes a raw width to the key mdCache actually stores
// renderers under. Glamour renders unusably narrow at very small widths, so
// anything below 20 clamps up to 20 - every populate and lookup against
// mdCache must go through this same normalization, or a narrow width (or
// width 0, before the first WindowSizeMsg) silently misses a renderer that
// was stored under its clamped key.
func mdCacheWidth(width int) int {
	if width < 20 {
		return 20
	}
	return width
}

// ensureMDRenderer creates a glamour TermRenderer for the given width in
// the cache if one doesn't already exist. Uses the "dark" bundled style;
// a custom glamour theme (glamour.WithStyles) would give full visual
// control over code blocks and headings, matching tau's theme palette.
// A nil cache (e.g. markdown rendering deliberately disabled) is a no-op,
// not a panic - renderMarkdown/renderToolBox then fall back to raw text.
func ensureMDRenderer(cache map[int]*glamour.TermRenderer, width int) {
	if cache == nil {
		return
	}
	width = mdCacheWidth(width)
	if _, ok := cache[width]; ok {
		return
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStylePath("dark"),
	)
	if err != nil {
		return
	}
	cache[width] = r
}

// renderMarkdown converts raw markdown to ANSI-styled terminal output using
// a glamour renderer memoized for width. Falls back to plain text when no
// renderer exists for width, and before the real terminal width is known
// (width == 0, prior to the first WindowSizeMsg) - rendering at a guessed
// width would wrap content for a size that isn't the actual terminal's.
func (m *model) renderMarkdown(content string, width int) string {
	if width <= 0 {
		return content
	}
	ensureMDRenderer(m.mdCache, width)
	r, ok := m.mdCache[mdCacheWidth(width)]
	if !ok || r == nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return out
}

// flushStreamingText commits any accumulated streaming text to scrollback
// without ending the turn. Called right before a new tool call starts: that
// text was authored before the call, so it must land in history now -
// otherwise the tool call/group that commits later (flushToolGroup) would
// render ahead of text that chronologically came first. Reasoning is
// discarded rather than persisted, matching finalizeResponse.
func (m *model) flushStreamingText() {
	if m.streaming != "" {
		rendered := m.renderMarkdown(m.streaming, m.width)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		m.viewport.SetContentLines(m.renderedLines)
	}
	m.streaming = ""
	m.reasoning = ""
}

// flushInterruptedTurn settles whatever was still live when a turn was
// cancelled or failed, then commits it to scrollback. Completed/error tools
// keep their final state; pending/running tools become error rows with the
// interruption reason so they do not disappear as an unfinished live panel.
func (m *model) flushInterruptedTurn(reason string) {
	m.flushStreamingText()
	if len(m.tools) == 0 {
		return
	}
	m.failUnfinishedTools(reason)
	m.flushToolGroup()
}

func (m *model) failUnfinishedTools(reason string) {
	for i := range m.tools {
		t := &m.tools[i]
		switch t.status {
		case "done":
			// Keep completed tools exactly as they resolved.
		case "error":
			if strings.TrimSpace(t.result) == "" {
				t.result = reason
			}
		default:
			t.status = "error"
			t.result = appendInterruptReason(t.result, reason)
		}
		if !t.startedAt.IsZero() && t.elapsed == 0 {
			t.elapsed = time.Since(t.startedAt)
		}
	}
}

func appendInterruptReason(result, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "interrupted"
	}
	result = strings.TrimRight(result, "\n")
	if result == "" {
		return reason
	}
	return result + "\n" + reason
}

// bashHistoryPrefix/bashHistoryInfix delimit the synthetic message format
// runBashCommand (internal/agent/coordinator.go) persists for "!" bash-mode
// commands: `fmt.Sprintf("Ran `+"`"+`%s`+"`"+`\n\n```\n%s\n```", cmd, output)`.
// Those commands run outside the normal tool-call loop, so the backend
// records them as plain ChatRoleUser text rather than a tool_call/tool
// result pair - parseBashHistoryMessage reverses that back into a command
// and output so a replayed session still shows the tool box instead of raw
// markdown text.
const (
	bashHistoryPrefix = "Ran `"
	bashHistoryInfix  = "`\n\n```\n"
	bashHistorySuffix = "\n```"
)

func parseBashHistoryMessage(content string) (cmd, output string, ok bool) {
	rest, ok := strings.CutPrefix(content, bashHistoryPrefix)
	if !ok {
		return "", "", false
	}
	cmd, body, ok := strings.Cut(rest, bashHistoryInfix)
	if !ok {
		return "", "", false
	}
	output, ok = strings.CutSuffix(body, bashHistorySuffix)
	if !ok {
		return "", "", false
	}
	return cmd, output, true
}

const (
	defaultNotificationClearDelay = 5 * time.Second
	defaultNotifyWarnDuration     = 8 * time.Second
	defaultNotifyInfoDuration     = 5 * time.Second
)

// notificationClearDelay is the auto-dismiss duration for transient
// notifications set via setNotification. Exported as a package variable
// so tests can set it to time.Millisecond (via TestMain) and avoid a
// real multi-second sleep per test through drainCmd.
var notificationClearDelay = defaultNotificationClearDelay

// setNotification sets an info-level notification that auto-clears.
// For a level (drives its color) or a custom duration - e.g. an error that
// should persist until superseded rather than silently vanish - use
// setNotificationWithLevel.
func (m *model) setNotification(text string) tea.Cmd {
	return m.setNotificationWithLevel(text, notify.LevelInfo, notificationClearDelay)
}

// setNotificationWithLevel sets the notification banner (rendered above the
// input area, wrapping to as many lines as it needs - see computeLayout)
// with an explicit level and auto-clear duration, using a generation counter
// so a newer notification is not clobbered by an older timer that fires
// late (N1). duration <= 0 means the notification persists until replaced,
// rather than auto-clearing - used for errors.
func (m *model) setNotificationWithLevel(text string, level notify.Level, duration time.Duration) tea.Cmd {
	m.notificationGen++
	m.notification = text
	m.notificationLevel = level
	if duration <= 0 {
		return func() tea.Msg { return nil }
	}
	gen := m.notificationGen
	return tea.Tick(duration, func(t time.Time) tea.Msg {
		return clearNotificationMsg{gen: gen}
	})
}

// notifyLevelFromChat maps a chat notification level to the notify package
// level. Matches internal/tui/inline_events.go's notifyLevelFromChat.
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
// Errors persist (0); warnings get 8s; info gets 5s. Matches
// internal/tui/inline_events.go's notifyDurationFromChat.
// notifyWarnDuration/notifyInfoDuration are package variables (not literals
// inline in notifyDurationFromChat) so tests can shrink them the same way
// TestMain shrinks notificationClearDelay - otherwise a test that drives a
// ChatNotificationEvent through Update actually waits out the real
// tea.Tick duration now that it schedules a genuine auto-clear Cmd instead
// of being handed to the old (never-rendered) notifyQ.
var (
	notifyWarnDuration = defaultNotifyWarnDuration
	notifyInfoDuration = defaultNotifyInfoDuration
)

func notifyDurationFromChat(level tauchat.ChatNotificationLevel) time.Duration {
	switch level {
	case tauchat.ChatNotificationError:
		return 0
	case tauchat.ChatNotificationWarn:
		return notifyWarnDuration
	default:
		return notifyInfoDuration
	}
}

// notifyReservedLines is how many rows the notification banner always
// occupies (see padOrClipLines) - enough for most messages to show in
// full without wrapping past it.
const notifyReservedLines = 2

// padOrClipLines pads or clips s to exactly n lines, so a fixed-height
// chrome region's reserved space never changes based on whether there's
// currently anything to show in it - that's what stops the viewport from
// visibly growing/shrinking every time a notification appears or clears.
// A message that doesn't fit within n lines is clipped, not wrapped
// further; that's rare (most notifications are one line) and preferable to
// letting the reserved height vary. Padding lines are a single space rather
// than empty, since visualLineCount trims trailing blank lines when
// measuring height and a single space is visually indistinguishable from
// truly empty in a terminal.
func padOrClipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, " ")
	}
	return strings.Join(lines, "\n")
}

// maxErrorTextLines and maxErrorTextChars bound how much of a runtime error
// or notification message reaches the notification banner and scrollback.
// Some providers return an unbounded response body verbatim in their error
// (e.g. a full HTML edge/gateway error page), and without a cap that text
// floods scrollback and effectively takes over the screen.
const (
	maxErrorTextLines = 20
	maxErrorTextChars = 2000
)

// truncateErrorText caps s to maxErrorTextLines lines and maxErrorTextChars
// characters, appending a marker when either limit trims content. Line
// count is checked first since a single huge line (no newlines) would
// otherwise pass the line check and still need the char cap.
func truncateErrorText(s string) string {
	s = strings.TrimSpace(s)
	truncated := false
	lines := strings.Split(s, "\n")
	if len(lines) > maxErrorTextLines {
		lines = lines[:maxErrorTextLines]
		s = strings.Join(lines, "\n")
		truncated = true
	}
	if len(s) > maxErrorTextChars {
		s = s[:maxErrorTextChars]
		truncated = true
	}
	if truncated {
		s = strings.TrimRight(s, "\n") + "\n… (truncated)"
	}
	return s
}

// notifyStyleForLevel returns the notification banner's color for a level.
func notifyStyleForLevel(level notify.Level) lipgloss.Style {
	switch level {
	case notify.LevelError:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ErrorColor)).Bold(true)
	case notify.LevelWarn:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn)).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ToneInfo)).Bold(true)
	}
}
