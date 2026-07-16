package tui2

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// committedReasoningBlock is a completed reasoning block already committed
// to permanent scrollback that can still be collapsed/expanded afterward -
// mirroring committedToolGroup's same "stay interactive after it scrolls
// into history" shape (see spliceCommittedReasoning). Only a finished turn's
// reasoning gets one of these; the in-progress turn's reasoning is rendered
// fresh every frame straight from m.reasoning (see viewportLinesForView) and
// is never a candidate for collapse - "streaming reasoning stays visible
// while active" falls out of that split for free, no separate flag needed.
type committedReasoningBlock struct {
	key       string // stable across an applySnapshot rebuild - see committedReasoningKey
	text      string // raw reasoning text; unaffected by collapse (presentation-only)
	collapsed bool

	// lineIdx/lineCount mirror committedToolGroup's own fields - see
	// spliceCommittedReasoning.
	lineIdx   int
	lineCount int
}

// committedReasoningKey derives a stable key for a completed reasoning
// block so its collapse state survives an applySnapshot rebuild instead of
// resetting to expanded every time the user submits another prompt -
// mirrors committedGroupKey. id is the owning assistant ChatMessage's ID
// when known: applySnapshot's msg.ID, or finalizeResponse's id param, which
// is the same persisted ID once the turn completes (see messageRanges). seq
// is a fallback ordinal (m.reasoningKeySeq) for the rare case a message
// commits reasoning before it has a real ID - that block's collapse state
// won't survive a later rebuild, same known limitation as any other
// synthetic key in this file (e.g. the "bash-%d" tool IDs in applySnapshot).
func committedReasoningKey(id string, seq int) string {
	if id != "" {
		return id
	}
	return fmt.Sprintf("reasoning-seq-%d", seq)
}

// reasoningBar prefixes every wrapped line of a reasoning block in Warm
// Ochre, blockquote-style. A single first-line glyph read as too subtle in
// testing - barely distinguishable from body text at a glance - so the bar
// runs the full height of the block instead, giving reasoning its own
// visual lane next to the user message above and the answer below, without
// tinting the body text itself or using a repeated "Reasoning"/"Tau" label.
const reasoningBar = "│ "

// renderReasoningLines wraps and styles reasoning text for display. The body
// uses an explicit muted foreground (not just ANSI faint, which several
// terminals render as visually identical to normal weight) paired with
// italic, so it reads as clearly secondary to the (unstyled, full-brightness)
// final answer even where faint has no effect. reasoningBar is the only
// place Warm Ochre accent appears. Shared by the live view, turn finalize,
// and snapshot rebuild so all three stay visually identical.
func renderReasoningLines(text string, width int) []string {
	// Mirror wrapWords' own "<=0 means unset, default to 80" rule before
	// subtracting the bar's width, so an unset/zero width (e.g. before the
	// first WindowSizeMsg) still wraps at a sane column count instead of
	// collapsing toward 1.
	if width <= 0 {
		width = 80
	}
	barWidth := lipgloss.Width(reasoningBar)
	wrapped := wrapWords(text, max(width-barWidth, 1))
	lines := make([]string, len(wrapped))
	bar := reasoningLabelStyle.Render(reasoningBar)
	for i, line := range wrapped {
		lines[i] = bar + reasoningStyle.Render(line)
	}
	return lines
}

// reasoningCollapsedGlyph marks a folded reasoning block with the same Warm
// Ochre accent as an expanded block's bar, so collapsed and expanded read as
// one affordance rather than two different UI elements.
const reasoningCollapsedGlyph = "▸ "

// renderReasoningCollapsedLine renders the single-line, subtle stand-in for
// a folded reasoning block: a small accent glyph plus a muted line count, no
// border or panel, and deliberately no keybinding hint inline - that lives
// in /help (see help.go) instead of repeating on every collapsed block.
func renderReasoningCollapsedLine(lineCount int) string {
	noun := "line"
	if lineCount != 1 {
		noun = "lines"
	}
	glyph := reasoningLabelStyle.Render(reasoningCollapsedGlyph)
	body := reasoningStyle.Render(fmt.Sprintf("reasoning collapsed (%d %s)", lineCount, noun))
	return glyph + body
}

// renderCommittedReasoning renders b in its current collapsed/expanded
// state - the single place that decides between the full block and the
// one-line stand-in, so commitReasoningBlock and spliceCommittedReasoning
// can't drift apart on how a block looks.
func (m *model) renderCommittedReasoning(b *committedReasoningBlock) string {
	lines := renderReasoningLines(b.text, m.width)
	if b.collapsed {
		return renderReasoningCollapsedLine(len(lines))
	}
	return strings.Join(lines, "\n")
}

// commitReasoningBlock appends a completed reasoning block to scrollback and
// registers a committedReasoningBlock so it can still be collapsed/expanded
// afterward (see toggleReasoningBlock). autoCollapse is the block's initial
// fold state for a brand-new commit - finalizeResponse and applySnapshot
// both pass "the turn produced a trailing answer", so a block collapses the
// moment final-answer generation actually happened (there's something to
// fold *into*), while a reasoning-only turn stays expanded, since collapsing
// it would just hide the entire response. restore, when key matches a prior
// block, always wins over autoCollapse - a user's manual toggle survives a
// snapshot rebuild regardless of what the message shape defaults to. restore
// is nil for a fresh live-turn commit, which has no prior state to inherit.
func (m *model) commitReasoningBlock(text, key string, autoCollapse bool, restore map[string]*committedReasoningBlock) {
	if text == "" {
		return
	}
	lineIdx := len(m.renderedLines)
	b := &committedReasoningBlock{key: key, text: text, collapsed: autoCollapse}
	if old, ok := restore[key]; ok {
		b.collapsed = old.collapsed
	}
	rendered := m.renderCommittedReasoning(b)
	b.lineIdx = lineIdx
	b.lineCount = visualLineCount(rendered)
	m.committedReasoning = append(m.committedReasoning, b)
	m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
	m.lastReasoningKey = key
}

// toggleReasoningBlock flips the collapse state of the committed reasoning
// block identified by key and splices its re-rendered replacement into
// renderedLines. Returns false if no block with that key is currently
// committed (e.g. reasoning got disabled, or nothing has completed yet).
func (m *model) toggleReasoningBlock(key string) bool {
	for _, b := range m.committedReasoning {
		if b.key != key {
			continue
		}
		b.collapsed = !b.collapsed
		m.spliceCommittedReasoning(b)
		return true
	}
	return false
}

// spliceCommittedReasoning re-renders b after a collapse/expand toggle and
// splices the result into m.renderedLines in place of its previous lines -
// shifting every committed tool group, message range, and other reasoning
// block that comes after it by the resulting line-count delta, exactly like
// spliceCommittedGroup (see its doc comment: this is the second of the two
// places that mutate renderedLines in place after initial construction, and
// anything added to renderedLines' bookkeeping in the future needs the same
// shift treatment in both).
func (m *model) spliceCommittedReasoning(b *committedReasoningBlock) {
	rendered := m.renderCommittedReasoning(b)
	newLines := strings.Split(rendered, "\n")
	delta := len(newLines) - b.lineCount

	tail := append([]string(nil), m.renderedLines[b.lineIdx+b.lineCount:]...)
	m.renderedLines = append(m.renderedLines[:b.lineIdx], newLines...)
	m.renderedLines = append(m.renderedLines, tail...)

	b.lineCount = len(newLines)
	for _, other := range m.committedReasoning {
		if other != b && other.lineIdx > b.lineIdx {
			other.lineIdx += delta
		}
	}
	for _, g := range m.committedGroups {
		if g.lineIdx > b.lineIdx {
			g.lineIdx += delta
		}
	}
	for i := range m.messageRanges {
		if m.messageRanges[i].startLine > b.lineIdx {
			m.messageRanges[i].startLine += delta
			m.messageRanges[i].endLine += delta
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
}

func (m *model) viewportLinesForView(visibleReasoning bool) []string {
	lines := make([]string, 0, len(m.renderedLines)+4)
	lines = append(lines, m.renderedLines...)
	m.highlightSelection(lines)

	// While thinking (in response, no tool activity, nothing streamed yet,
	// no visible reasoning), the status bar's own "Thinking ●●●" already
	// covers this state - nothing to add here.
	if visibleReasoning {
		lines = append(lines, renderReasoningLines(m.reasoning, m.width)...)
	}
	if m.streaming != "" {
		lines = append(lines, renderStreamingLines(m.streaming, m.width)...)
	}
	return lines
}
