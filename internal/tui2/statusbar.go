package tui2

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui"
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

// inResponse reports whether a turn is in flight - derived from agentState
// rather than tracked as a second, independent field, so the two can never
// drift apart (see docs/specs/state-taxonomy.md, Category 1: Turn State).
func (m *model) inResponse() bool {
	switch m.agentState {
	case agentThinking, agentProcessing, agentRunningTool, agentStreaming:
		return true
	default: // agentReady, agentCancelled, agentError
		return false
	}
}

// --- status bar segments ---------------------------------------------------

// statusSeg is an alias for the frontend-neutral segment type shared with
// internal/tui (see pkg/taui/statusline.go) - the layout/width-pressure
// algorithm, segment joining, and usage formatters live there; this file
// keeps only this frontend's styling (lipgloss/theme-based) and the
// agent-state-driven segment assembly.
type statusSeg = taui.StatusLineSeg

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
	// renderStatusBar's width-pressure loop - used for "τ tau", the
	// active-state label (Thinking/Running <tool>/generating/Cancelled/
	// Error), the "Ctrl+C Stop" hint, and the steering indicator, so a
	// narrow terminal degrades by shedding secondary metadata first and
	// keeps showing what the agent is actually doing. Shared with
	// internal/tui via taui.StatusLinePrioTransient so the "never drop"
	// threshold can't silently drift between frontends.
	prioTransient = taui.StatusLinePrioTransient
)

// statusGrey renders secondary status-bar text (separators, unstyled
// segments) using terminal-native dim rather than a fixed grey - it dims
// whatever foreground the user's terminal actually has instead of
// overriding it.
func statusGrey(s string) string { return lipgloss.NewStyle().Faint(true).Render(s) }

// --- rendering -------------------------------------------------------------

// renderStatusBar assembles left and right segment groups into a single line
// that never exceeds width (see pkg/taui.RenderStatusLine for the shared
// layout/width-pressure algorithm).
func renderStatusBar(width int, left, right []statusSeg) string {
	return taui.RenderStatusLine(width, left, right, statusGrey)
}

func joinSegs(segs []statusSeg) (styled, plain string) {
	return taui.JoinStatusLineSegs(segs, statusGrey)
}

// identitySegs returns the model/provider/reasoning-effort segments shared
// by every state that shows identity - Ready and Cancelled want the full
// picture, Thinking wants just the model name (see the Suggested content
// templates this mirrors).
func (m *model) identitySegs(full bool) []statusSeg {
	var segs []statusSeg
	if m.modelName != "" {
		segs = append(segs, statusSeg{Text: m.modelName, Style: boldText, Prio: prioModel})
	}
	if !full {
		return segs
	}
	if m.provider != "" && m.provider != m.modelName {
		segs = append(segs, statusSeg{Text: m.provider, Prio: prioProvider})
	}
	if m.reasoningEffort != "" && m.reasoningEffort != "auto" {
		segs = append(segs, statusSeg{Text: m.reasoningEffort, Prio: prioEffort})
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
		Text: fmt.Sprintf("↑%s ↓%s", humanizeTokens(totals.PromptTokens), humanizeTokens(totals.CompletionTokens)),
		Prio: prioTokens,
	}}
	if totals.Cost > 0 {
		segs = append(segs, statusSeg{Text: formatCost(totals.Cost), Prio: prioCost})
	}
	if totals.TurnDurationMs > 0 {
		segs = append(segs, statusSeg{Text: formatDurationCompact(totals.TurnDurationMs), Prio: prioDuration})
	}
	if pct := contextPct(totals.LastPromptTokens, m.ctxWindow); pct >= 0 {
		segs = append(segs, statusSeg{
			Text:  fmt.Sprintf("ctx %d%%", pct),
			Style: contextStyle(pct),
			Prio:  prioContext,
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
		Text:           "web",
		StyledOverride: termkit.Hyperlink("web", url),
		Prio:           prioWeb,
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
		Text:  "Ctrl+C Stop",
		Style: func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) },
		Prio:  prioTransient,
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
	left := []statusSeg{{Text: "τ", Prio: prioTransient}}
	var right []statusSeg

	switch m.agentState {
	case agentThinking:
		left = append(left, statusSeg{
			Text:  "Thinking " + thinkingDots(m.spinnerFrame),
			Style: func(s string) string { return termkit.FgOnly(s, theme.AccentColor) },
			Prio:  prioTransient,
		})
		right = append(right, m.identitySegs(false)...)
		right = append(right, ctrlCStopSeg())

	case agentProcessing:
		left = append(left, statusSeg{
			Text:  "Processing " + thinkingDots(m.spinnerFrame),
			Style: func(s string) string { return termkit.FgOnly(s, theme.AccentColor) },
			Prio:  prioTransient,
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
			Text:  toolText,
			Style: func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) },
			Prio:  prioTransient,
		})
		right = append(right, statusSeg{Text: formatElapsed(elapsed), Prio: prioMetric})
		right = append(right, ctrlCStopSeg())

	case agentStreaming:
		left = append(left, statusSeg{
			Text:  "generating",
			Style: func(s string) string { return termkit.FgOnly(s, theme.ToneSuccess) },
			Prio:  prioTransient,
		})
		if rate, ok := approxTokensPerSecond(m.streaming, time.Since(m.streamStartedAt)); ok {
			right = append(right, statusSeg{Text: fmt.Sprintf("~%d tok/s", rate), Prio: prioMetric})
		}
		right = append(right, m.sessionTokenSegs()...)
		right = append(right, ctrlCStopSeg())

	case agentCancelled:
		left = append(left, statusSeg{
			Text:  "Cancelled",
			Style: func(s string) string { return termkit.FgOnly(s, theme.ToneMuted) },
			Prio:  prioTransient,
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
			Text:  "Error",
			Style: func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) },
			Prio:  prioTransient,
		})

	default: // agentReady
		left = append(left, statusSeg{Text: "Ready", Prio: prioTransient})
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
		dots := (m.spinnerFrame / 4) % 4
		left = append(left, statusSeg{
			Text:  "steering" + strings.Repeat(".", dots),
			Style: func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) },
			Prio:  prioTransient,
		})
	}

	return renderStatusBar(m.width, left, right)
}

// --- formatters ------------------------------------------------------------

func humanizeTokens(n int) string { return taui.HumanizeTokens(n) }

func formatCost(usd float64) string { return taui.FormatCost(usd) }

func contextPct(promptTok, ctxWindow int) int { return taui.ContextPct(promptTok, ctxWindow) }

// formatContextPct renders "N%", or "" when unavailable. Mirrors
// internal/tui/statusbar.go's function of the same name - used by /cost's
// full breakdown (unlike contextStyle, which is status-bar-only).
func formatContextPct(promptTok, ctxWindow int) string {
	return taui.FormatContextPct(promptTok, ctxWindow)
}

// contextStyle picks a severity colour for the context widget from the
// shared taui.ContextSeverityFor thresholds (75%/90%, kept in sync with
// internal/tui): grey below 75%, warn from 75%, error from 90%.
func contextStyle(pct int) func(string) string {
	switch taui.ContextSeverityFor(pct) {
	case taui.ContextCritical:
		return func(s string) string { return termkit.FgOnly(s, theme.ErrorColor) }
	case taui.ContextWarn:
		return func(s string) string { return termkit.FgOnly(s, theme.ToneWarn) }
	default:
		return nil
	}
}

// --- width helpers ---------------------------------------------------------

// visibleWidth approximates the rendered width of a plain string (no ANSI).
func visibleWidth(s string) int { return taui.VisibleWidth(s) }

// truncateANSIToWidth truncates a styled string to fit within maxWidth,
// appending an ellipsis marker when truncation occurs.
func truncateANSIToWidth(s string, maxWidth int, ellipsis string) string {
	return taui.TruncateANSIToWidth(s, maxWidth, ellipsis)
}

// stripANSI removes both CSI sequences (ESC '[' ... final byte in 0x40-0x7E,
// e.g. SGR colour codes ending in 'm') and OSC sequences (ESC ']' ... BEL or
// ST, e.g. OSC 8 hyperlinks) - delegates to pkg/taui, shared with
// internal/tui, which independently needed the identical OSC-aware fix (an
// earlier version here only recognised the SGR 'm' terminator, so an OSC 8
// hyperlink - whose payload rarely contains 'm' - would swallow everything
// up to the next unrelated 'm', corrupting width measurements for any
// segment built from it).
func stripANSI(s string) string { return taui.StripANSI(s) }

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
