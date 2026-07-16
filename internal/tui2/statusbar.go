package tui2

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- agent state -------------------------------------------------------------

// agentState is an explicit, closed set of what the agent is currently
// doing - the status bar's single source of truth for which content to
// show, instead of inferring "what's happening" by sniffing
// m.notification text or combining inResponse/streaming/tools ad hoc at
// render time. events.go's handleChatEvent sets this at each transition;
// computeStatusBar only ever reads it.
type agentState int

const (
	// agentReady is the zero value - correct for a freshly constructed
	// model with no turn yet in flight, and restored once a turn completes.
	agentReady agentState = iota
	agentThinking
	agentProcessing
	agentRunningTool
	agentStreaming
	agentCancelled
	agentError
)

// --- status bar segments ---------------------------------------------------

// statusSeg is one widget in the status bar. text is the PLAIN string used
// for width math; style applies ANSI styling and is nil for the default
// grey.
//
// When styledOverride is non-empty it replaces style(text) in the styled
// output (text is still used for width measurement). This lets a segment
// embed raw escape sequences the width math can't see through - e.g. an
// OSC 8 hyperlink, whose terminator isn't the SGR 'm' stripANSI/lipgloss
// expect. Mirrors internal/tui/statusbar.go's statusSeg (see webSeg below).
type statusSeg struct {
	text           string
	style          func(string) string // nil = default grey
	styledOverride string
	prio           int // lower is dropped first under width pressure
}

const (
	// Left-side identity segments (see identitySegs) - reasoning effort is
	// the least essential (dropped first), the model name the most (kept
	// longest), so a narrow terminal loses "high"/"ollama" before it ever
	// mangles "deepseek-v4-pro:cloud" into a meaningless character-level
	// truncation like "o…".
	prioEffort   = 1
	prioProvider = 2
	prioModel    = 3

	// Right-side metric segments. prioDuration (the session's cumulative
	// turn time) is the most disposable of the usage segments - nice
	// context, but cost/tokens/context% matter more under width pressure.
	prioDuration = 1
	prioWeb      = 2
	prioCost     = 3
	prioTokens   = 4
	prioContext  = 5
	// prioMetric is a busy state's supporting numeric segment (elapsed time
	// for Running tool, tok/s for Streaming) - more disposable than the
	// session-token/context segments above.
	prioMetric = 6
	// prioTransient marks a segment as effectively undroppable by
	// renderStatusBar's width-pressure loop (see its "prio >= prioTransient"
	// skip) - used for "τ tau", the active-state label (Thinking/Running
	// <tool>/generating/Cancelled/Error), the "Ctrl+C Stop" hint, and the
	// steering indicator, so a narrow terminal degrades by shedding
	// secondary metadata first and keeps showing what the agent is
	// actually doing.
	prioTransient = 100
)

const statusSep = " · "

// statusGrey renders secondary status-bar text (separators, unstyled
// segments) using terminal-native dim rather than a fixed grey - it dims
// whatever foreground the user's terminal actually has instead of
// overriding it.
func statusGrey(s string) string { return lipgloss.NewStyle().Faint(true).Render(s) }

// --- rendering -------------------------------------------------------------

// renderStatusBar assembles left and right segment groups into a single line
// that never exceeds width. Both sides are priority-dropped whole-segment-at-
// a-time under pressure before either resorts to character-level truncation
// - dropping whole segments first avoids chopping one mid-word (e.g. a
// provider name truncated to a meaningless "o…"). Width is measured on
// plain text so ANSI never inflates the budget.
func renderStatusBar(width int, left, right []statusSeg) string {
	if width < 1 {
		width = 1
	}

	lefts := make([]statusSeg, 0, len(left))
	for _, s := range left {
		if s.text != "" {
			lefts = append(lefts, s)
		}
	}
	leftStyled, leftPlain := joinSegs(lefts)
	leftW := visibleWidth(leftPlain)

	rights := make([]statusSeg, 0, len(right))
	for _, s := range right {
		if s.text != "" {
			rights = append(rights, s)
		}
	}
	rightStyled, rightPlain := joinSegs(rights)
	rightW := visibleWidth(rightPlain)

	gap := func(rw int) int {
		if leftW > 0 && rw > 0 {
			return 1
		}
		return 0
	}

	// dropLowestPrio removes the lowest-priority segment (skipping anything
	// >= prioTransient, which is meant to survive any width) from segs,
	// returning the updated slice/styled/plain/width, or ok=false when
	// nothing left is droppable.
	dropLowestPrio := func(segs []statusSeg) (out []statusSeg, styled, plain string, w int, ok bool) {
		idx := -1
		for i, s := range segs {
			if s.prio >= prioTransient {
				continue
			}
			if idx == -1 || s.prio < segs[idx].prio {
				idx = i
			}
		}
		if idx == -1 {
			return segs, "", "", 0, false
		}
		out = append(segs[:idx:idx], segs[idx+1:]...)
		styled, plain = joinSegs(out)
		return out, styled, plain, visibleWidth(plain), true
	}

	// floorWidth is the width of segs once every droppable (prio <
	// prioTransient) segment is gone - the minimum space this side is
	// guaranteed to need no matter how much gets dropped from it. The
	// right-drop loop below must reserve this much room for the left side
	// (and vice versa) rather than just checking whether it fits alone -
	// otherwise an undroppable left ("τ tau · Ready") could still lose
	// content to the character-truncation fallback while purely
	// decorative right segments (e.g. a low-priority model name) survive
	// untouched, which is backwards from "prioritize active state over
	// secondary metadata."
	floorWidth := func(segs []statusSeg) int {
		var floor []statusSeg
		for _, s := range segs {
			if s.prio >= prioTransient {
				floor = append(floor, s)
			}
		}
		_, plain := joinSegs(floor)
		return visibleWidth(plain)
	}
	leftFloorW := floorWidth(lefts)

	// Drop lowest-priority right segments until the right side no longer
	// crowds out the left side's guaranteed floor. This loop alone
	// establishes leftFloorW+gap+rightW <= width whenever right has
	// anything droppable left to give, and dropping left below (in the
	// next loop) never grows rightW back - so a second right-side pass
	// afterward would be redundant.
	for leftFloorW+gap(rightW)+rightW > width {
		next, styled, _, w, ok := dropLowestPrio(rights)
		if !ok {
			break
		}
		rights, rightStyled, rightW = next, styled, w
	}

	// Then drop lowest-priority left segments (whole segments, not
	// characters) until the combined line fits, or only left's own floor
	// remains - symmetric with the right-side loop above.
	for leftW+gap(rightW)+rightW > width && leftW > leftFloorW {
		next, styled, _, w, ok := dropLowestPrio(lefts)
		if !ok {
			break
		}
		lefts, leftStyled, leftW = next, styled, w
	}

	if rightW == 0 {
		if leftStyled == "" {
			return strings.Repeat(" ", width)
		}
		if leftW > width {
			return truncateANSIToWidth(leftStyled, width, "…")
		}
		return leftStyled
	}

	if leftW+gap(rightW)+rightW <= width {
		pad := max(width-leftW-rightW, 1)
		return leftStyled + strings.Repeat(" ", pad) + rightStyled
	}

	// Whole-segment dropping above couldn't make it fit (e.g. every
	// remaining segment is at or above prioTransient) - fall back to
	// character-level truncation as a last resort.
	avail := width - gap(rightW) - rightW
	if avail < 1 {
		return truncateANSIToWidth(rightStyled, width, "…")
	}
	leftTrunc := truncateANSIToWidth(leftStyled, avail, "…")
	pad := max(width-visibleWidth(leftTrunc)-rightW, 1)
	return leftTrunc + strings.Repeat(" ", pad) + rightStyled
}

func joinSegs(segs []statusSeg) (styled, plain string) {
	var sb, pb strings.Builder
	first := true
	for _, s := range segs {
		if s.text == "" {
			continue
		}
		if !first {
			sb.WriteString(statusGrey(statusSep))
			pb.WriteString(statusSep)
		}
		first = false
		if s.styledOverride != "" {
			sb.WriteString(s.styledOverride)
		} else {
			style := s.style
			if style == nil {
				style = statusGrey
			}
			sb.WriteString(style(s.text))
		}
		pb.WriteString(s.text)
	}
	return sb.String(), pb.String()
}

// identitySegs returns the model/provider/reasoning-effort segments shared
// by every state that shows identity - Ready and Cancelled want the full
// picture, Thinking wants just the model name (see the Suggested content
// templates this mirrors).
func (m *model) identitySegs(full bool) []statusSeg {
	var segs []statusSeg
	if m.modelName != "" {
		segs = append(segs, statusSeg{text: m.modelName, style: boldText, prio: prioModel})
	}
	if !full {
		return segs
	}
	if m.provider != "" && m.provider != m.modelName {
		segs = append(segs, statusSeg{text: m.provider, prio: prioProvider})
	}
	if m.reasoningEffort != "" && m.reasoningEffort != "auto" {
		segs = append(segs, statusSeg{text: m.reasoningEffort, prio: prioEffort})
	}
	return segs
}

// sessionTokenSegs returns the token/cost/duration/context-% right-side
// segments shared by every state that shows usage metrics (Ready,
// Cancelled, Streaming) - the same content computeStatusBar always showed,
// just factored out so each state doesn't repeat the m.usage plumbing.
func (m *model) sessionTokenSegs() []statusSeg {
	if m.usage == nil {
		return nil
	}
	totals := m.usage.Snapshot(m.sessionID)
	if totals == nil || totals.TotalTokens <= 0 {
		return nil
	}
	// "↑in ↓out" (input/output split) is more informative at a glance than
	// a single combined total - it's the split that actually drives cost
	// and context usage differently, not just a bigger/smaller number.
	segs := []statusSeg{{
		text: fmt.Sprintf("↑%s ↓%s", humanizeTokens(totals.PromptTokens), humanizeTokens(totals.CompletionTokens)),
		prio: prioTokens,
	}}
	if totals.Cost > 0 {
		segs = append(segs, statusSeg{text: formatCost(totals.Cost), prio: prioCost})
	}
	if totals.TurnDurationMs > 0 {
		segs = append(segs, statusSeg{text: formatDurationCompact(totals.TurnDurationMs), prio: prioDuration})
	}
	if pct := contextPct(totals.LastPromptTokens, m.ctxWindow); pct >= 0 {
		segs = append(segs, statusSeg{
			text:  fmt.Sprintf("ctx %d%%", pct),
			style: contextStyle(pct),
			prio:  prioContext,
		})
	}
	return segs
}

// webSeg is the local web UI's status segment - a short "web" label as an
// OSC 8 terminal hyperlink to the actual URL (clickable in supporting
// terminals: iTerm2, kitty, WezTerm, modern VTE; falls back to plain "web"
// text everywhere else, see termkit.Hyperlink) rather than spelling out the
// full "web: http://127.0.0.1:NNNNN" every time, which was most of this
// segment's width for no extra information a click doesn't already give.
func webSeg(url string) statusSeg {
	return statusSeg{
		text:           "web",
		styledOverride: termkit.Hyperlink("web", url),
		prio:           prioWeb,
	}
}

// ctrlCStopSeg is the interrupt hint shown across every busy state
// (Thinking/Running tool/Streaming) - the sole place this hint appears
// (the chat area used to duplicate it via steerHint; removed since Ctrl+C
// stop belongs in the status bar, not scrollback, and "[Enter] steer" was
// actively wrong once Enter started queueing by default instead). Marked
// prioTransient so it's one of the last things a narrow terminal drops.
func ctrlCStopSeg() statusSeg {
	return statusSeg{
		text:  "Ctrl+C Stop",
		style: func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) },
		prio:  prioTransient,
	}
}

// approxTokensPerSecond estimates a live generation rate from the actively
// streaming response text using a coarse ~4-characters-per-token heuristic
// (a common rule of thumb for English text) rather than a real
// provider-specific tokenizer - reaching into a provider's own tokenizer
// here would couple the status bar to provider internals, which the
// heuristic avoids while still being labeled "tok/s" rather than claiming
// exactness. ok is false before there's been enough elapsed time to produce
// a stable-looking number, since dividing by a near-zero duration right as
// streaming starts would otherwise flash a wildly large, meaningless rate.
func approxTokensPerSecond(streamed string, elapsed time.Duration) (rate int, ok bool) {
	if elapsed < 500*time.Millisecond {
		return 0, false
	}
	chars := utf8.RuneCountInString(streamed)
	if chars == 0 {
		return 0, false
	}
	tokens := max(chars/4, 1)
	rate = int(math.Round(float64(tokens) / elapsed.Seconds()))
	return rate, rate > 0
}

// computeStatusBar builds the left and right segment groups from current
// model state, branching on the explicit m.agentState (see "agent state"
// above) rather than re-deriving "what's happening" from inResponse/
// streaming/tools combinations at render time. Called synchronously in
// View() - no goroutine, no Tick needed.
//
// Layout is deliberately consistent across all 7 states: the left side is
// always "τ tau" plus the current activity (Ready/Thinking/Processing/
// Running <tool>/generating/Cancelled/Error), never anything else; the right side always
// carries model/provider/effort/token/cost/context/web - i.e. model sits on
// the opposite side of the bar from whatever the agent is currently doing,
// so the two never compete for the same reading position. Busy states
// (Thinking/Processing/RunningTool/Streaming) trim the right side down to just what's
// useful mid-turn (nothing else is as relevant, and it keeps the bar
// compact); idle-ish states (Ready/Cancelled) restore the full identity +
// token-usage picture. The steering indicator layers onto the left side on
// top of whichever state is active, so it never needs duplicating per
// branch.
func (m *model) computeStatusBar() string {
	// "τ tau" is prioTransient (undroppable) - it's the app's own identity,
	// the last thing that should ever disappear.
	left := []statusSeg{{text: "τ", prio: prioTransient}}
	var right []statusSeg

	switch m.agentState {
	case agentThinking:
		left = append(left, statusSeg{
			text:  "Thinking " + thinkingDots(m.spinnerFrame),
			style: func(s string) string { return termkit.FgOnly(s, theme.AccentColor) },
			prio:  prioTransient,
		})
		right = append(right, m.identitySegs(false)...)
		right = append(right, ctrlCStopSeg())

	case agentProcessing:
		left = append(left, statusSeg{
			text:  "Processing " + thinkingDots(m.spinnerFrame),
			style: func(s string) string { return termkit.FgOnly(s, theme.AccentColor) },
			prio:  prioTransient,
		})
		right = append(right, m.identitySegs(false)...)
		right = append(right, ctrlCStopSeg())

	case agentRunningTool:
		toolText := "Running"
		var elapsed time.Duration
		if t, ok := m.runningTool(); ok {
			if t.name != "" {
				toolText = "Running " + t.name
			}
			if t.summary != "" {
				toolText += " - " + t.summary
			}
			elapsed = time.Since(t.startedAt)
		}
		left = append(left, statusSeg{
			text:  toolText,
			style: func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) },
			prio:  prioTransient,
		})
		right = append(right, statusSeg{text: formatElapsed(elapsed), prio: prioMetric})
		right = append(right, ctrlCStopSeg())

	case agentStreaming:
		left = append(left, statusSeg{
			text:  "generating",
			style: func(s string) string { return termkit.FgOnly(s, theme.ToneSuccess) },
			prio:  prioTransient,
		})
		if rate, ok := approxTokensPerSecond(m.streaming, time.Since(m.streamStartedAt)); ok {
			right = append(right, statusSeg{text: fmt.Sprintf("~%d tok/s", rate), prio: prioMetric})
		}
		right = append(right, m.sessionTokenSegs()...)
		right = append(right, ctrlCStopSeg())

	case agentCancelled:
		left = append(left, statusSeg{
			text:  "Cancelled",
			style: func(s string) string { return termkit.FgOnly(s, theme.ToneMuted) },
			prio:  prioTransient,
		})
		right = append(right, m.identitySegs(true)...)
		right = append(right, m.sessionTokenSegs()...)
		if m.webURL != "" {
			right = append(right, webSeg(m.webURL))
		}

	case agentError:
		// Just the state, at a glance - the full message already has a home
		// in the notification banner (persists until dismissed) and
		// scrollback (see ChatRuntimeErrorEvent), both on screen at the same
		// time as this bar. Restating it here too was pure duplication.
		left = append(left, statusSeg{
			text:  "Error",
			style: func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) },
			prio:  prioTransient,
		})

	default: // agentReady
		left = append(left, statusSeg{text: "Ready", prio: prioTransient})
		right = append(right, m.identitySegs(true)...)
		right = append(right, m.sessionTokenSegs()...)
		if m.webURL != "" {
			right = append(right, webSeg(m.webURL))
		}
	}

	// Steering overlays whichever state is active - it's an orthogonal
	// mid-turn condition (the user interrupted to redirect the agent), not
	// one of the 6 primary states, so every branch above can stay unaware
	// of it and this stays the single place it's appended. Undroppable
	// (prioTransient): once the user has actually intervened mid-turn,
	// that's more important than any of the identity segments it might
	// otherwise be dropped in favor of.
	if m.steering {
		dots := int(time.Now().UnixMilli()/300) % 4
		left = append(left, statusSeg{
			text:  "steering" + strings.Repeat(".", dots),
			style: func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) },
			prio:  prioTransient,
		})
	}

	return renderStatusBar(m.width, left, right)
}

// --- formatters ------------------------------------------------------------

func humanizeTokens(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

func formatCost(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func contextPct(promptTok, ctxWindow int) int {
	if promptTok <= 0 || ctxWindow <= 0 {
		return -1
	}
	return int(math.Round(float64(promptTok) / float64(ctxWindow) * 100))
}

// formatContextPct renders "ctx N%", or "" when unavailable. Mirrors
// internal/tui/statusbar.go's function of the same name - used by /cost's
// full breakdown (unlike contextStyle, which is status-bar-only).
func formatContextPct(promptTok, ctxWindow int) string {
	p := contextPct(promptTok, ctxWindow)
	if p < 0 {
		return ""
	}
	return fmt.Sprintf("ctx %d%%", p)
}

func contextStyle(pct int) func(string) string {
	switch {
	case pct >= 90:
		return func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) }
	case pct >= 75:
		return func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) }
	default:
		return nil
	}
}

// --- width helpers ---------------------------------------------------------

// visibleWidth approximates the rendered width of a plain string (no ANSI).
func visibleWidth(s string) int {
	return lipgloss.Width(s)
}

// truncateANSIToWidth truncates a styled string to fit within maxWidth,
// appending an ellipsis marker when truncation occurs.
func truncateANSIToWidth(s string, maxWidth int, ellipsis string) string {
	if maxWidth <= 0 {
		return ""
	}
	plain := stripANSI(s)
	if visibleWidth(plain) <= maxWidth {
		return s
	}
	budget := max(maxWidth-visibleWidth(ellipsis), 0)
	style := lipgloss.NewStyle().MaxWidth(budget)
	return style.Render(s) + ellipsis
}

// stripANSI removes both CSI sequences (ESC '[' ... final byte in 0x40-0x7E,
// e.g. SGR colour codes ending in 'm') and OSC sequences (ESC ']' ... BEL or
// ST, e.g. OSC 8 hyperlinks). An earlier version only recognised the SGR
// 'm' terminator, so an OSC 8 hyperlink - whose payload (a URL) rarely
// contains 'm' - would swallow everything up to the next unrelated 'm' it
// found, corrupting width measurements for any segment built from it.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			b.WriteByte(s[i])
			i++
			continue
		}
		switch {
		case i+1 < len(s) && s[i+1] == ']': // OSC: ESC ']' ... (BEL | ESC '\')
			j := i + 2
			for j < len(s) && s[j] != '\x07' && !(s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			switch {
			case j < len(s) && s[j] == '\x07':
				i = j + 1
			case j+1 < len(s):
				i = j + 2
			default:
				i = len(s)
			}
		case i+1 < len(s) && s[i+1] == '[': // CSI: ESC '[' ... final byte 0x40-0x7E
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
			} else {
				i = len(s)
			}
		default:
			i++ // unrecognised escape - skip just the ESC byte
		}
	}
	return b.String()
}

// visualLineCount returns the number of visible lines in s, ignoring any
// trailing newlines. Empty strings contribute zero height so they do not
// inflate layout budgets.
func visualLineCount(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// --- helpers ---------------------------------------------------------------

func boldText(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}
