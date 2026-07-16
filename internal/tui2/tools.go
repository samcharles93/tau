package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"

	// summary is a short, model-authored one-liner describing what this
	// call is doing (see ChatToolExecutionStartedEvent.Summary), shown in
	// the status bar while the tool runs. Empty when the model didn't
	// supply one.
	summary string

	// startedAt is initialized when the call appears as a fallback, then reset
	// when execution begins. elapsed is frozen at completion so a settled row
	// keeps showing how long it took rather than ticking on (or dropping to
	// zero) after the turn ends.
	startedAt time.Time
	elapsed   time.Duration

	// Phase 1: live output streaming, per-tool spinner, and expand/collapse
	// interaction.
	tailLines  []string // live output streaming, max tailCap lines
	tailCap    int      // max tail lines to show (default 6)
	spinnerIdx int      // per-tool spinner animation frame (bumped on tickMsg)
	expanded   bool     // user has clicked Enter to expand this tool

	// details carries tool-specific structured data (e.g. tools.DiffDetails
	// for edit/write), as forwarded via ChatToolExecutionCompletedEvent.Details.
	// nil when the tool has nothing structured to offer.
	details any
}

// committedToolGroup is a multi-tool-call batch already committed to
// permanent scrollback (see commitToolGroup) that can still be
// unfolded/refolded and drilled into afterward, mirroring the live group's
// own two-level accordion (group -> per-tool rows -> one row's full output)
// instead of freezing forever into a single flat summary line the moment it
// scrolls into history. A lone committed tool call doesn't need one of
// these - it's already a full, permanently-detailed box (see
// commitToolGroup) with nothing left to toggle.
type committedToolGroup struct {
	tools      []toolState
	expanded   bool   // group unfolded into per-tool rows
	expandedID string // which row, if any, is further expanded to full output

	// lineIdx/lineCount record where in m.renderedLines this group's current
	// rendering lives, so a click can find it (toggleCommittedToolAtLine)
	// and a toggle can splice its replacement in without disturbing any
	// other content beyond a fixed line-count shift (spliceCommittedGroup).
	lineIdx   int
	lineCount int
}

// committedGroupKey identifies a committed tool-call group by its ordered
// tool-call IDs. Real tool calls keep their persisted call ID across an
// applySnapshot rebuild; bash-history-reconstructed entries key off the
// message's index instead (see applySnapshot) since they have no real call
// ID - either way the key is stable across rebuilds, which is what lets
// fold/expand state survive a snapshot instead of resetting to folded every
// time the user submits another prompt.
func committedGroupKey(tools []toolState) string {
	ids := make([]string, len(tools))
	for i, t := range tools {
		ids[i] = t.id
	}
	return strings.Join(ids, "\x00")
}

// childAgentResult holds the terminal state of a spawned child agent,
// extracted from the agent tool's result details. Rendered as a compact
// summary line above the tool result (see docs/specs/agents/05-ui.md).
type childAgentResult struct {
	instanceID string // e.g. "research#k3v9qp"
	specName   string // e.g. "research"
	status     string // completed, failed, cancelled, budget_exhausted, timed_out, working
	activity   string // tool verb or "thinking" (only for working state)
	turns      int
	tokens     int // total tokens
	durationMs int64
	errorMsg   string
	sessionID  string // child's own session ID, set once known - empty until then
}

// isChildTerminal reports whether status is one of childAgentResult's
// terminal states (as opposed to "working"/live). Only terminal children
// can be drilled into, since their transcript is fully persisted.
func isChildTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "budget_exhausted", "timed_out":
		return true
	default:
		return false
	}
}

// renderChildAgentLine renders the compact terminal summary line.
func renderChildAgentLine(c childAgentResult) string {
	statusStyle := termkit.FgOnly
	statusColour := theme.SuccessColor
	switch c.status {
	case "failed", "timed_out":
		statusColour = theme.ErrorColor
	case "cancelled":
		statusColour = theme.AccentColor
	case "budget_exhausted":
		statusColour = theme.AccentColor
	}
	status := statusStyle(strings.ReplaceAll(c.status, "_", " "), statusColour)
	durStr := formatDurationCompact(c.durationMs)
	return termkit.FgOnly(fmt.Sprintf("  agent %s  %s  turn %d · %dt · %s",
		c.instanceID, status, c.turns, c.tokens, durStr), theme.ToneMuted)
}

// renderChildStateBlock renders the live child agent state as a compact
// status element inside the tool box. Working state shows activity,
// turns, tokens, and elapsed; terminal states show the summary line.
func renderChildStateBlock(c childAgentResult, width int) string {
	if isChildTerminal(c.status) {
		return renderChildAgentLine(c)
	}

	// Live working state: activity line + metrics line.
	var lines []string

	// Activity line: spec name + instance, current activity.
	activity := c.activity
	if activity == "" {
		activity = "starting"
	}
	idLine := termkit.FgOnly(fmt.Sprintf("  agent %s  %s", c.instanceID, activity),
		theme.AccentColor)
	lines = append(lines, idLine)

	// Metrics line: turns, tokens, elapsed.
	tokensStr := formatTokensCompact(c.tokens)
	durStr := formatDurationCompact(c.durationMs)
	metrics := termkit.FgOnly(fmt.Sprintf("  turn %d · %s · %s", c.turns, tokensStr, durStr),
		theme.ToneMuted)
	lines = append(lines, metrics)

	return strings.Join(lines, "\n")
}

func formatTokensCompact(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// toolBoxGeometry is the inclusive [startY, endY] row range (0-based, in
// final rendered-view coordinates) that a tool box occupies.
type toolBoxGeometry struct {
	id     string
	startY int
	endY   int
}

func (m *model) upsertToolCall(callID, toolName, argumentsSummary, summary string) {
	for i := range m.tools {
		if m.tools[i].id == callID {
			if toolName != "" {
				m.tools[i].name = toolName
			}
			if argumentsSummary != "" {
				m.tools[i].args += argumentsSummary
			}
			if summary != "" {
				m.tools[i].summary = summary
			}
			return
		}
	}
	if m.adoptToolCallID(callID, toolName) {
		for i := range m.tools {
			if m.tools[i].id == callID {
				if argumentsSummary != "" {
					m.tools[i].args += argumentsSummary
				}
				if summary != "" {
					m.tools[i].summary = summary
				}
				return
			}
		}
	}
	// This tool call is new - any streaming/reasoning text accumulated so
	// far was authored before it started, so commit it now (see
	// flushStreamingText) rather than let it render after the tool group
	// that chronologically follows it.
	m.flushStreamingText()
	m.tools = append(m.tools, toolState{
		id:        callID,
		name:      toolName,
		args:      argumentsSummary,
		summary:   summary,
		status:    "pending",
		startedAt: time.Now(),
	})
}

// adoptToolCallID reconciles the synthetic call IDs used before some
// providers stream a real tool_call.id with the final IDs used by started /
// completed lifecycle events. Without this, tui2 can keep rendering the
// early "tool_call_0" row forever while the real call completes under a
// different ID.
func (m *model) adoptToolCallID(callID, toolName string) bool {
	if callID == "" || toolName == "" {
		return false
	}
	for i := range m.tools {
		if m.tools[i].id == callID {
			return true
		}
	}
	match := -1
	for i := range m.tools {
		t := m.tools[i]
		if t.name != toolName {
			continue
		}
		if t.status != "pending" && t.status != "running" {
			continue
		}
		if !strings.HasPrefix(t.id, "tool_call_") {
			continue
		}
		if match >= 0 {
			return false
		}
		match = i
	}
	if match < 0 {
		return false
	}
	m.tools[match].id = callID
	if m.expandedID != "" && strings.HasPrefix(m.expandedID, "tool_call_") {
		m.expandedID = callID
	}
	return true
}

// adoptStreamedToolCallID reconciles a synthetic ID with the real ID that a
// provider may reveal in a later delta without repeating the function name.
// The stream index identifies the synthetic row; lifecycle events then keep
// updating that same row by its final ID.
func (m *model) adoptStreamedToolCallID(callID string, index int) bool {
	if callID == "" || strings.HasPrefix(callID, "tool_call_") {
		return false
	}
	for i := range m.tools {
		if m.tools[i].id == callID {
			return true
		}
	}

	syntheticID := fmt.Sprintf("tool_call_%d", index)
	for i := range m.tools {
		t := &m.tools[i]
		if t.id != syntheticID || (t.status != "pending" && t.status != "running") {
			continue
		}
		t.id = callID
		if m.expandedID == syntheticID {
			m.expandedID = callID
		}
		return true
	}
	return false
}

func (m *model) setToolStatus(id, status string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			// A tool-call delta can arrive well before execution actually starts.
			// Start the elapsed clock on the pending-to-running transition, while
			// ignoring duplicate start events so they cannot reset it.
			if status == "running" && m.tools[i].status != "running" {
				m.tools[i].startedAt = time.Now()
			}
			m.tools[i].status = status
			// Freeze the elapsed clock the moment a row settles so it reports
			// how long the call actually took, rather than continuing to tick
			// (or resetting) once the turn ends and the frame stops advancing.
			if status == "done" || status == "error" {
				m.tools[i].elapsed = time.Since(m.tools[i].startedAt)
			}
			return
		}
	}
}

// anyToolRunning reports whether any tool in the current batch is still
// executing - used by handleChatEvent to decide whether completing one tool
// call should drop the status bar back to agentThinking or leave it on
// agentRunningTool for a still-running sibling.
func (m *model) anyToolRunning() bool {
	for i := range m.tools {
		if m.tools[i].status == "running" {
			return true
		}
	}
	return false
}

// runningTool returns the first tool currently executing, for the status
// bar's "Running <tool>" segment - and its elapsed time. ok is false when
// nothing is running (agentRunningTool should never be the current state in
// that case, but computeStatusBar stays defensive rather than assuming it).
func (m *model) runningTool() (t toolState, ok bool) {
	for i := range m.tools {
		if m.tools[i].status == "running" {
			return m.tools[i], true
		}
	}
	return toolState{}, false
}

// setToolResult appends a streamed output chunk (ChatToolOutputEvent) to a
// tool's live-peek result.
func (m *model) setToolResult(id, result string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].result += result
			return
		}
	}
}

// finalizeToolResult replaces a tool's result with its final summary
// (ChatToolExecutionCompletedEvent.ResultSummary) rather than appending -
// the summary is the authoritative final output, not an increment on top of
// whatever ChatToolOutputEvent chunks streamed in beforehand. Mirrors
// internal/tui/inline_events.go discarding the streamed tail before setting
// the resolved row's label from ResultSummary alone.
func (m *model) finalizeToolResult(id, result string, details any) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].result = result
			m.tools[i].details = details
			return
		}
	}
}

// appendToolTail appends a streamed output chunk to a tool's live tail buffer
// (Phase 1). Lines are split by newline and capped at tailCap (default 6)
// using a ring-buffer approach that discards the oldest lines.
func (m *model) appendToolTail(id, chunk string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			t := &m.tools[i]
			if t.tailCap <= 0 {
				t.tailCap = 6
			}
			for line := range strings.SplitSeq(chunk, "\n") {
				if line == "" {
					continue
				}
				t.tailLines = append(t.tailLines, line)
				// Ring-buffer: discard oldest when over cap.
				if len(t.tailLines) > t.tailCap {
					t.tailLines = t.tailLines[len(t.tailLines)-t.tailCap:]
				}
			}
			return
		}
	}
}

// flushToolGroup commits the current live tool-call batch to scrollback as
// one group (see commitToolGroup) and clears it from the live/chrome list.
// Called when new streaming text resumes after a batch of tool calls, and
// at turn end for a trailing batch with no following text - both mean the
// batch is chronologically over.
func (m *model) flushToolGroup() {
	if len(m.tools) == 0 {
		return
	}
	m.commitToolGroup(m.tools, nil)
	m.viewport.SetContentLines(m.renderedLines)
	m.tools = nil
	m.focusedTool = -1
	m.expandedID = ""
}

// renderCommittedToolsSummary renders a multi-tool-call group's folded
// (collapsed) one-line form for permanent scrollback - otherwise a turn
// with many tool calls would bury the assistant text around it (real bug: a
// 100+ tool-call turn made the preceding/following message unreachable in
// the scrollback). Tool names are deliberately omitted - with the group now
// re-openable (see committedToolGroup), the count plus a click is enough,
// and a long, deduped name list just added noise.
func renderCommittedToolsSummary(tools []toolState) string {
	metrics := collectToolMetrics(tools)
	glyph := "✓"
	if metrics.errored > 0 {
		glyph = "✗"
	}
	summary := glyph + " " + metrics.String()
	return toolMetaStyle.Render(summary)
}

// renderCommittedGroup renders a committedToolGroup as it currently stands.
// Single tools use the ordinary tool box collapsed/expanded states; multi-tool
// groups fold to a one-line summary or unfold into the same per-tool-row
// accordion the live group uses (see renderToolGroupBox).
func (m *model) renderCommittedGroup(g *committedToolGroup) string {
	if len(g.tools) == 1 {
		return m.renderToolBox(g.tools[0], g.expanded, 1, m.width)
	}
	if !g.expanded {
		return renderCommittedToolsSummary(g.tools)
	}
	box, _ := m.renderToolGroupBox(g.tools, g.expandedID, -1, m.width)
	return box
}

// commitToolGroup appends tools to scrollback as permanent content and
// registers a committedToolGroup so it can still be unfolded after commit (see
// toggleCommittedToolAtLine).
// restore carries over fold/row-expand state from before a full
// applySnapshot rebuild, keyed by committedGroupKey (nil for a brand-new
// live-turn commit via flushToolGroup, which always starts folded).
func (m *model) commitToolGroup(tools []toolState, restore map[string]*committedToolGroup) {
	if len(tools) == 0 {
		return
	}
	lineIdx := len(m.renderedLines)

	g := &committedToolGroup{tools: append([]toolState(nil), tools...)}
	if old, ok := restore[committedGroupKey(tools)]; ok {
		g.expanded, g.expandedID = old.expanded, old.expandedID
	}
	rendered := m.renderCommittedGroup(g)
	g.lineIdx = lineIdx
	g.lineCount = visualLineCount(rendered)
	m.committedGroups = append(m.committedGroups, g)
	m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)

	// Append child agent summary lines for any "agent" tools that completed.
	for _, t := range tools {
		if t.name == "agent" {
			if child, ok := m.childAgents[t.id]; ok {
				line := renderChildAgentLine(child)
				m.renderedLines = append(m.renderedLines, line)
			}
		}
	}
}

// shouldNavigateTools returns true when tool focus navigation is appropriate:
// not in a running response, no active prompt, and input is empty (Phase 1).
func (m *model) shouldNavigateTools() bool {
	return !m.inResponse && m.activePrompt == nil && m.input == ""
}

// focusNextTool moves focusedTool to the next (delta=1) or previous (delta=-1)
// completed tool (status "done" or "error"). Wraps around when at the edges.
func (m *model) focusNextTool(delta int) {
	// Collect indices of eligible tools.
	var eligible []int
	for i, t := range m.tools {
		if t.status == "done" || t.status == "error" {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		m.focusedTool = -1
		return
	}

	// Find current position in eligible list and advance by delta.
	cur := -1
	for ei, ti := range eligible {
		if ti == m.focusedTool {
			cur = ei
			break
		}
	}
	newIdx := (cur + delta) % len(eligible)
	if newIdx < 0 {
		newIdx += len(eligible)
	}
	m.focusedTool = eligible[newIdx]
	m.focusedChild = -1
}

// recordChildAgentOrder appends callID to childAgentOrder the first time
// it's seen, giving focusNextChild a stable Tab-cycle order independent of
// childAgents' map iteration order.
func (m *model) recordChildAgentOrder(callID string) {
	if slices.Contains(m.childAgentOrder, callID) {
		return
	}
	m.childAgentOrder = append(m.childAgentOrder, callID)
}

// focusNextChild moves focusedChild to the next (delta=1) or previous
// (delta=-1) terminal child agent state block, in the order their tool
// calls were made (m.childAgentOrder). Wraps around when at the edges.
// Mirrors focusNextTool's eligibility/wrap logic. Setting focusedChild
// clears focusedTool so the two focus rings stay mutually exclusive.
func (m *model) focusNextChild(delta int) {
	var eligible []int
	for i, id := range m.childAgentOrder {
		if isChildTerminal(m.childAgents[id].status) {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		m.focusedChild = -1
		return
	}

	cur := -1
	for ei, ci := range eligible {
		if ci == m.focusedChild {
			cur = ei
			break
		}
	}
	newIdx := (cur + delta) % len(eligible)
	if newIdx < 0 {
		newIdx += len(eligible)
	}
	m.focusedChild = eligible[newIdx]
	m.focusedTool = -1
}

// toggleToolBoxAtY toggles expand/collapse for whichever live tool box (from
// a fresh layout computation) contains absolute view row y - the
// click-to-expand behavior for a plain click (no drag) on the tool group.
// When the live group is collapsed, any click inside the tools area expands
// it. When expanded, a click on a tool row toggles per-tool expansion
// (existing behavior), while a click on the header/border/padding area
// collapses the whole group back to its one-line summary.
func (m *model) toggleToolBoxAtY(y int) {
	geom := m.computeLayout()

	// A click inside the tools area when the group is collapsed: expand it.
	if m.toolGroupCollapsed && y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.toolGroupCollapsed = false
		return
	}

	for _, tb := range geom.toolBoxes {
		if y < tb.startY || y > tb.endY {
			continue
		}
		m.focusedTool = m.findLiveTool(tb.id)
		if m.expandedID == tb.id {
			m.expandedID = ""
		} else {
			m.expandedID = tb.id
			if realIdx := m.findLiveTool(tb.id); realIdx >= 0 {
				m.tools[realIdx].expanded = true
			}
		}
		return
	}

	// Click landed inside the tools area but on the header/border/padding -
	// collapse the whole live group to its one-line summary.
	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.toolGroupCollapsed = true
	}
}

// toggleCommittedToolAtLine handles a plain click (no drag) landing on a
// committed tool-call group in scrollback (idx is the absolute logical line
// index from m.viewportSel's anchor - see logicalLineAtRow): folds/unfolds
// the group, or, if it's already unfolded, expands/collapses whichever tool
// row the click landed on - restoring the same accordion interaction the
// group had live, which would otherwise be lost for good the moment it
// scrolls into history (see committedToolGroup). Returns whether a group
// actually handled the click, so the caller can tell that apart from an
// ordinary click on plain text (which should just clear the selection).
func (m *model) toggleCommittedToolAtLine(idx int) bool {
	for _, g := range m.committedGroups {
		if idx < g.lineIdx || idx >= g.lineIdx+g.lineCount {
			continue
		}
		if !g.expanded {
			g.expanded = true
			m.spliceCommittedGroup(g)
			return true
		}
		if len(g.tools) == 1 {
			g.expanded = false
			m.spliceCommittedGroup(g)
			return true
		}

		// Unfolded: a click on a specific tool row toggles just that row's
		// full-detail expansion. Anything else - the header text, the
		// group's top/bottom border, the padding around a row - folds the
		// whole group back down. rows only covers the per-tool lines (see
		// renderToolGroupBox: row 0 is the border, row 1 is the header
		// text), so a click landing outside every row's range is exactly
		// "not on a tool row", which is deliberately the fold trigger
		// rather than a dead click - a header/border click is the obvious
		// place a user would click to close what they just opened.
		rel := idx - g.lineIdx
		toggledRow := false
		if _, rows := m.renderToolGroupBox(g.tools, g.expandedID, -1, m.width); rows != nil {
			for _, tb := range rows {
				if rel < tb.startY || rel > tb.endY {
					continue
				}
				if g.expandedID == tb.id {
					g.expandedID = ""
				} else {
					g.expandedID = tb.id
				}
				toggledRow = true
				break
			}
		}
		if !toggledRow {
			g.expanded = false
			g.expandedID = ""
		}
		m.spliceCommittedGroup(g)
		return true
	}
	return false
}

// spliceCommittedGroup re-renders g after toggleCommittedToolAtLine folded,
// unfolded, or expanded/collapsed one of its rows, and splices the result
// into m.renderedLines in place of its previous lines - shifting every
// other committed group AND every recorded messageRange that comes after it
// by the resulting line-count delta so their recorded positions stay
// accurate for the next click. This is the only place that mutates
// renderedLines in place after initial construction (every other write is a
// pure trailing append) - anything else added to renderedLines' bookkeeping
// in the future needs the same shift treatment here.
func (m *model) spliceCommittedGroup(g *committedToolGroup) {
	rendered := m.renderCommittedGroup(g)
	newLines := strings.Split(rendered, "\n")
	delta := len(newLines) - g.lineCount

	tail := append([]string(nil), m.renderedLines[g.lineIdx+g.lineCount:]...)
	m.renderedLines = append(m.renderedLines[:g.lineIdx], newLines...)
	m.renderedLines = append(m.renderedLines, tail...)

	g.lineCount = len(newLines)
	for _, other := range m.committedGroups {
		if other != g && other.lineIdx > g.lineIdx {
			other.lineIdx += delta
		}
	}
	for _, rb := range m.committedReasoning {
		if rb.lineIdx > g.lineIdx {
			rb.lineIdx += delta
		}
	}
	for i := range m.messageRanges {
		if m.messageRanges[i].startLine > g.lineIdx {
			m.messageRanges[i].startLine += delta
			m.messageRanges[i].endLine += delta
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
}

// toggleToolExpansion toggles the expanded state for the currently focused
// tool. Returns a tea.Cmd (always nil for now) to match dispatchKey's
// return signature.
func (m *model) toggleToolExpansion() tea.Cmd {
	if m.focusedTool < 0 || m.focusedTool >= len(m.tools) {
		return nil
	}
	t := &m.tools[m.focusedTool]
	if m.expandedID == t.id {
		m.expandedID = ""
	} else {
		m.expandedID = t.id
		t.expanded = true
	}
	return nil
}

// renderTool renders a single-line tool call summary for the inline tool
// list shown during a running turn. Full markdown-rendered tool output
// (including glamour-rendered code blocks and diffs) is handled by
// renderToolBox, which has access to the model's glamour renderer cache.
func renderTool(t toolState, frame int) string {
	style := toolStyleForStatus(t.name, t.status)

	// Build the label: tool name, or for skill tool, parse JSON args for
	// the human-readable skill name.
	label := t.name
	if t.name == "skill" {
		label = skillLabelFromArgs(t.args)
	}

	// Lead with a lifecycle glyph - use per-tool spinnerIdx (Phase 1) when
	// available, falling back to the shared frame for backward compatibility.
	spIdx := t.spinnerIdx
	if spIdx == 0 {
		spIdx = frame
	}
	line := toolGlyph(t.status, spIdx) + " " + label

	switch t.status {
	case "pending", "running":
		// A running row shows how long it's been going; a frozen number would
		// read as stuck, so this ticks with the frame.
		if !t.startedAt.IsZero() {
			line += toolMetaStyle.Render("  (" + formatElapsed(time.Since(t.startedAt)) + ")")
		}
	default:
		// N11: render a short result summary when available.
		const resultLimit = 60
		if t.result != "" {
			summary := strings.ReplaceAll(t.result, "\n", " ")
			if len(summary) > resultLimit {
				summary = summary[:resultLimit] + "…"
			}
			line += " - " + summary
		}
		if t.elapsed > 0 {
			line += toolMetaStyle.Render("  (" + formatElapsed(t.elapsed) + ")")
		}
	}
	return style.Render(line)
}

// renderToolGroup renders the current live (uncommitted) tool-call batch -
// see flushToolGroup for when it moves to permanent scrollback.
func (m *model) renderToolGroup() (string, []toolBoxGeometry) {
	if len(m.tools) == 0 {
		return "", nil
	}
	if m.toolGroupCollapsed && len(m.tools) > 1 {
		return renderLiveToolsSummary(m.tools, m.spinnerFrame), nil
	}
	return m.renderToolGroupBox(m.tools, m.expandedID, m.focusedTool, m.width)
}

// renderLiveToolsSummary renders a collapsed one-line summary for the live
// tool-call group - mirrors renderCommittedToolsSummary but keeps the live
// spinner and running/pending/error counts up-to-date.
func renderLiveToolsSummary(tools []toolState, frame int) string {
	metrics := collectToolMetrics(tools)
	glyph := toolGlyph("running", frame)
	if metrics.running+metrics.pending == 0 {
		if metrics.errored > 0 {
			glyph = "✗"
		} else {
			glyph = "✓"
		}
	}
	summary := glyph + " " + metrics.String()
	return toolGroupSummaryStyle.Render(summary)
}

type toolMetrics struct {
	total   int
	pending int
	running int
	errored int
}

func collectToolMetrics(tools []toolState) toolMetrics {
	metrics := toolMetrics{total: len(tools)}
	for _, t := range tools {
		switch t.status {
		case "pending":
			metrics.pending++
		case "running":
			metrics.running++
		case "error":
			metrics.errored++
		}
	}
	return metrics
}

func (m toolMetrics) String() string {
	summary := fmt.Sprintf("%d tool calls", m.total)
	if m.pending > 0 {
		summary += fmt.Sprintf(" · %d pending", m.pending)
	}
	if m.running > 0 {
		summary += fmt.Sprintf(" · %d running", m.running)
	}
	if m.errored > 0 {
		summary += fmt.Sprintf(" · %d error", m.errored)
	}
	return summary
}

// maxToolRowsVisible is the most per-tool rows renderToolGroupBox will show
// inside a group before windowing - prevents a turn with 40+ tool calls from
// pushing everything off-screen. Overflowed rows get an "↑ N more" indicator.
const maxToolRowsVisible = 8

// renderToolGroupBox renders any batch of tool calls - live or a committed
// group reopened from scrollback (see committedToolGroup) - as one unit. A
// lone tool keeps its full interactive box (matches the pre-grouping look);
// multiple tools render as one bordered group - a header summary line, then
// each tool as a compact one-line row, except the tool whose id matches
// expandedID, which renders its full box in place of its row. focusedIdx
// draws the keyboard-focus marker on that row (pass -1 for a committed
// group, which has no live keyboard focus). Returns the rendered string
// plus each *visible* tool's row range (relative to the string's own first
// line, i.e. row 0 is the box's top border) for mouse hit-testing.
//
// When there are more tools than maxToolRowsVisible, the box windows to show
// the last N rows (most-recent calls at the bottom) and prepends an
// "↑ N more" overflow indicator so the user knows there's more above.
func (m *model) renderToolGroupBox(tools []toolState, expandedID string, focusedIdx int, width int) (string, []toolBoxGeometry) {
	if width < 20 {
		width = 80
	}
	if len(tools) == 1 {
		t := tools[0]
		expanded := expandedID != "" && expandedID == t.id
		box := m.renderToolBox(t, expanded, 1, width)
		return box, []toolBoxGeometry{{id: t.id, startY: 0, endY: visualLineCount(box) - 1}}
	}

	header := collectToolMetrics(tools).String()

	n := len(tools)
	visibleCount := n
	start := 0
	if visibleCount > maxToolRowsVisible {
		visibleCount = maxToolRowsVisible
		start = n - visibleCount
	}

	lines := []string{toolGroupHeaderStyle.Render(header)}
	rows := make([]toolBoxGeometry, 0, visibleCount)
	row := 2 // top border row (0) + header row (1)

	// Overflow indicator for windowed groups.
	if start > 0 {
		overflow := toolGroupOverflowStyle.Render(fmt.Sprintf("  ↑ %d more", start))
		lines = append(lines, overflow)
		row++ // account for the overflow line
	}

	for i := start; i < n; i++ {
		t := tools[i]
		upperI := i // relative to the full slice, for focusedIdx comparison
		expanded := expandedID != "" && expandedID == t.id
		var line string
		if expanded {
			line = m.renderToolBox(t, true, len(tools), width)
		} else {
			marker := "  "
			if focusedIdx == upperI {
				marker = toolFocusMarkerStyle.Render("› ")
			}
			line = marker + renderTool(t, m.spinnerFrame)
		}
		h := visualLineCount(line)
		rows = append(rows, toolBoxGeometry{id: t.id, startY: row, endY: row + h - 1})
		row += h
		lines = append(lines, line)
	}

	box := toolGroupBoxStyle.Width(width).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return box, rows
}

// renderToolBox renders a complete background-colored tool box with borders,
// glyph, status, elapsed clock, live tail output, and full result when
// expanded (Phase 1). width is the render width (callers normally pass
// m.width; the child transcript overlay passes its own narrower inner
// width instead).
func (m *model) renderToolBox(t toolState, expanded bool, _ int, width int) string {
	label := t.name
	if t.name == "skill" {
		label = skillLabelFromArgs(t.args)
	}

	glyph := toolGlyph(t.status, t.spinnerIdx)
	var elapsed time.Duration
	if t.status == "pending" || t.status == "running" {
		elapsed = time.Since(t.startedAt)
	} else {
		elapsed = t.elapsed
	}

	elapsedStr := ""
	if !t.startedAt.IsZero() || elapsed > 0 {
		elapsedStr = " (" + formatElapsed(elapsed) + ")"
	}

	// Title line content: glyph + name + elapsed. For pending/running, the
	// status word ("pending" vs "running") is real information the shared
	// spinner glyph alone doesn't distinguish. For done/error, the glyph
	// and box color (green/red) already say everything - repeating "done"
	// or "error" as text is redundant log-speak, not state.
	statusWord := ""
	if t.status == "pending" || t.status == "running" {
		statusWord = " " + t.status
	}
	title := glyph + " " + label + statusWord + elapsedStr

	// Pick the style based on status.
	boxStyle := toolBoxStyleForStatus(t.name, t.status)
	if expanded {
		boxStyle = toolBoxExpandedStyle
	}
	focused := m.focusedTool >= 0 && m.focusedTool < len(m.tools) && m.tools[m.focusedTool].id == t.id
	if focused {
		boxStyle = boxStyle.BorderForeground(themeHex(theme.AccentColor)).Bold(true)
	}

	if width < 20 {
		width = 80
	}
	boxStyle = boxStyle.Width(width).Padding(0, 1)

	// Build body lines.
	var bodyLines []string

	// For agent tools, render the live child state block while running
	// or the terminal summary once complete.
	if t.name == "agent" {
		if child, ok := m.childAgents[t.id]; ok && child.instanceID != "" {
			bodyLines = append(bodyLines, renderChildStateBlock(child, width-4))
		}
	}

	if expanded {
		// Expanded mode: show full result content in an inner box.
		// When the output looks like markdown (code blocks, lists,
		// tables), render it through glamour for syntax highlight.
		bodyLines = append(bodyLines, "")
		innerWidth := max(
			// inner box narrower than outer
			width-8, 10,
		)
		innerStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(themeHex(theme.SecondaryColor)).
			Width(innerWidth).
			Padding(0, 1)
		var innerContent strings.Builder
		if len(t.result) == 0 {
			innerContent.WriteString("No output\n")
		}

		if looksLikeMarkdown(t.result) {
			// Wrap as a fenced markdown block and render through
			// the width-memoized glamour cache for syntax highlight.
			var mdBuilder strings.Builder
			mdBuilder.Grow(len(t.result) + len("```result.md\n\n```"))
			mdBuilder.WriteString("```result.md\n")
			mdBuilder.WriteString(t.result)
			mdBuilder.WriteString("\n```")
			md := mdBuilder.String()
			ensureMDRenderer(m.mdCache, innerWidth)
			if r, ok := m.mdCache[mdCacheWidth(innerWidth)]; ok && r != nil {
				if out, err := r.Render(md); err == nil {
					for line := range strings.SplitSeq(out, "\n") {
						innerContent.WriteString(line)
						innerContent.WriteByte('\n')
					}
					innerRendered := innerStyle.Render(strings.TrimRight(innerContent.String(), "\n"))
					for line := range strings.SplitSeq(innerRendered, "\n") {
						bodyLines = append(bodyLines, line)
					}
					content := title + "\n" + strings.Join(bodyLines, "\n")
					return boxStyle.Render(content)
				}
			}
			// Fall through to plain-text rendering if glamour failed.
		}

		resultLines := strings.SplitSeq(t.result, "\n")
		for line := range resultLines {
			innerContent.WriteString(line)
			innerContent.WriteByte('\n')
		}
		// Render inner box and append each line to body.
		innerRendered := innerStyle.Render(strings.TrimRight(innerContent.String(), "\n"))
		for line := range strings.SplitSeq(innerRendered, "\n") {
			bodyLines = append(bodyLines, line)
		}
	} else {
		// Compact mode: show first tail line if any, otherwise truncated result summary.
		detail := ""
		if len(t.tailLines) > 0 && (t.status == "pending" || t.status == "running") {
			detail = t.tailLines[len(t.tailLines)-1]
		} else if t.result != "" {
			const resultLimit = 60
			summary := strings.ReplaceAll(t.result, "\n", " ")
			if len(summary) > resultLimit {
				summary = summary[:resultLimit] + "…"
			}
			detail = summary
		}
		if detail != "" {
			bodyLines = append(bodyLines, detail)
		}
	}

	// Build content: title on first line, body on subsequent lines.
	content := title
	if len(bodyLines) > 0 {
		content += "\n" + strings.Join(bodyLines, "\n")
	}
	return boxStyle.Render(content)
}

// looksLikeMarkdown reports whether content contains markdown syntax markers
// that glamour would meaningfully render. A lightweight heuristic - false
// positives are harmless (empty glamour render), false negatives leave
// plain text which is already the default.
func looksLikeMarkdown(content string) bool {
	patterns := []string{
		"# ", "## ", "**", "```", // headings, bold, code fences
		"- ", "1. ", "> ", // lists, blockquotes
		"---", "***", // horizontal rules
	}
	for _, p := range patterns {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// toolStyleForStatus returns the lipgloss style for a tool's current lifecycle
// state - warm peach for pending/running, green for done, red for error -
// with lilac variants for the Skill tool to keep it visually distinct.
func toolStyleForStatus(toolName, status string) lipgloss.Style {
	skill := toolName == "skill"
	switch status {
	case "done":
		if skill {
			return skillSuccessStyle
		}
		return toolSuccessStyle
	case "error":
		if skill {
			return skillFailedStyle
		}
		return toolErrorStyle
	default: // pending, running
		if skill {
			return skillRunningStyle
		}
		return toolRunningStyle
	}
}

// toolBoxStyleForStatus returns the lipgloss style for a tool box (Phase 1)
// with background color, rounded border, and per-status coloring.
func toolBoxStyleForStatus(toolName, status string) lipgloss.Style {
	skill := toolName == "skill"
	switch status {
	case "done":
		if skill {
			return toolBoxSkillSuccessStyle
		}
		return toolBoxSuccessStyle
	case "error":
		if skill {
			return toolBoxSkillFailedStyle
		}
		return toolBoxErrorStyle
	default: // pending, running
		if skill {
			return toolBoxSkillRunningStyle
		}
		return toolBoxRunningStyle
	}
}

// skillLabelFromArgs extracts a human-readable skill name from tool arguments
// (JSON object with a "name": key). Falls back to "skill" when unparseable.
func skillLabelFromArgs(args string) string {
	// Look for "name": followed by a JSON string value and extract it.
	// Search for the exact key delimiter to avoid matching "no-name" etc.
	key := `","name":"`
	// Also try at the start of the object, right after the opening brace.
	for _, prefix := range []string{`{"name":"`, key} {
		if _, after, ok := strings.Cut(args, prefix); ok {
			rest := after
			if before, _, ok := strings.Cut(rest, "\""); ok {
				return "skill: " + before
			}
		}
	}
	return "skill"
}

// findLiveTool returns the index of the live (uncommitted) tool with the
// given id, or -1.
func (m *model) findLiveTool(id string) int {
	for i, t := range m.tools {
		if t.id == id {
			return i
		}
	}
	return -1
}

// findCommittedGroupWithTool returns the committed group containing a tool
// with the given id, or nil. A tool id is never simultaneously live and
// committed - flushToolGroup/commitToolGroup always clear m.tools at the
// same time a batch is committed - so live and committed lookups never
// collide.
func (m *model) findCommittedGroupWithTool(id string) *committedToolGroup {
	for _, g := range m.committedGroups {
		for _, t := range g.tools {
			if t.id == id {
				return g
			}
		}
	}
	return nil
}

// extractChildAgentResult parses the agent tool's result details into a
// childAgentResult. The agent tool's details carry a JSON object with
// "status", "instance_id", "session_id", "usage" (turns, tokens), and
// error fields (see tools/agent.go, assembleStatusLine).
func extractChildAgentResult(details any) (childAgentResult, bool) {
	d, ok := details.(map[string]any)
	if !ok {
		return childAgentResult{}, false
	}
	id, _ := d["instance_id"].(string)
	if id == "" {
		return childAgentResult{}, false
	}
	status, _ := d["status"].(string)
	if status == "" {
		status = "completed"
	}
	var turns int
	var tokens int
	var durationMs int64
	if usage, ok := d["usage"].(map[string]any); ok {
		if t, ok := usage["turns"].(float64); ok {
			turns = int(t)
		} else if t, ok := usage["turns"].(int); ok {
			turns = t
		}
		if it, ok := usage["input_tokens"].(float64); ok {
			tokens += int(it)
		} else if it, ok := usage["input_tokens"].(int); ok {
			tokens += it
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			tokens += int(ot)
		} else if ot, ok := usage["output_tokens"].(int); ok {
			tokens += ot
		}
	}
	if dur, ok := d["duration_ms"].(float64); ok {
		durationMs = int64(dur)
	} else if dur, ok := d["duration_ms"].(int); ok {
		durationMs = int64(dur)
	} else if dur, ok := d["duration_ms"].(int64); ok {
		durationMs = dur
	}
	errMsg, _ := d["error"].(string)
	sessionID, _ := d["session_id"].(string)
	return childAgentResult{
		instanceID: id,
		status:     status,
		turns:      turns,
		tokens:     tokens,
		durationMs: durationMs,
		errorMsg:   errMsg,
		sessionID:  sessionID,
	}, true
}
