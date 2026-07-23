package tui

import (
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// statusSeg is an alias for the frontend-neutral segment type shared with
// internal/tui2 (see pkg/taui/statusline.go) - the layout/width-pressure
// algorithm, segment joining, and usage formatters live there; this file
// keeps only this frontend's styling (termkit-based).
type statusSeg = taui.StatusLineSeg

// Drop priorities for right-group segments. Lower is dropped first under width
// pressure; prioTransient (shared with tui2 via taui.StatusLinePrioTransient)
// is never dropped.
const (
	prioWeb       = 1
	prioCost      = 2
	prioTokens    = 3
	prioContext   = 4
	prioTransient = taui.StatusLinePrioTransient
)

// statusGrey is the default segment / separator style: grey foreground with no
// trailing reset (the bar lives inside a Box background).
func statusGrey(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) }

// joinSegs renders a group to (styled, plain). The plain form carries no ANSI so
// it can be measured directly with uniseg/VisibleWidth; the styled form is what
// gets emitted.
func joinSegs(segs []statusSeg) (styled, plain string) {
	return taui.JoinStatusLineSegs(segs, statusGrey)
}

// renderStatusBar lays out an identity group pinned left and a live-metrics group
// right-justified to width. Width is measured on PLAIN text (never styled output)
// so ANSI never inflates the budget. The assembled line never exceeds width and
// is always a single line.
func renderStatusBar(width int, left, right []statusSeg) string {
	return taui.RenderStatusLine(width, left, right, statusGrey)
}

// ── Pure formatters ───────────────────────────────────────────────────────────

// humanizeTokens renders a token count compactly: "942", "15.2k", "1.3M".
func humanizeTokens(n int) string { return taui.HumanizeTokens(n) }

// formatCost renders a USD amount: "$0.0182" under $1 (4dp), else "$1.23" (2dp).
func formatCost(usd float64) string { return taui.FormatCost(usd) }

// contextPct returns the rounded context-window utilisation percentage, or -1
// when it can't be computed (no prompt tokens or no known window).
func contextPct(promptTok, ctxWindow int) int { return taui.ContextPct(promptTok, ctxWindow) }

// formatContextPct renders the context-window utilisation, e.g. "41%", or ""
// when it isn't computable.
func formatContextPct(promptTok, ctxWindow int) string {
	return taui.FormatContextPct(promptTok, ctxWindow)
}

// contextStyle picks a subtle severity colour for the context widget: grey below
// 75%, amber from 75%, red from 90% (thresholds shared with tui2 via
// taui.ContextSeverityFor). A nil result means default grey.
func contextStyle(pct int) func(string) string {
	switch taui.ContextSeverityFor(pct) {
	case taui.ContextCritical:
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorRed) }
	case taui.ContextWarn:
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorAmber) }
	default:
		return nil
	}
}
