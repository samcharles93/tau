package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// statusSeg is one widget in the status bar. text is the PLAIN string (used for
// width math); style applies ANSI styling and is nil for the default grey. prio
// orders width-pressure dropping within the right group: lower prios are dropped
// first, prioTransient is never dropped.
//
// When styledOverride is non-empty it replaces style(text) in the styled output
// (text is still used for width measurement). This allows embedding raw ANSI
// sequences - e.g. OSC 8 hyperlinks - that can't be produced by applying style
// to text alone.
type statusSeg struct {
	text           string
	style          func(string) string
	prio           int
	styledOverride string
}

// Drop priorities for right-group segments. Lower is dropped first under width
// pressure; transient items (steering / notifications) are never dropped.
const (
	prioWeb       = 1
	prioCost      = 2
	prioTokens    = 3
	prioContext   = 4
	prioTransient = 100
)

// statusSep is the dotted separator joining segments within a group.
const statusSep = " · "

// statusGrey is the default segment / separator style: grey foreground with no
// trailing reset (the bar lives inside a Box background).
func statusGrey(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) }

// joinSegs renders a group to (styled, plain). The plain form carries no ANSI so
// it can be measured directly with uniseg/VisibleWidth; the styled form is what
// gets emitted.
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

// renderStatusBar lays out an identity group pinned left and a live-metrics group
// right-justified to width. Width is measured on PLAIN text (never styled output)
// so ANSI never inflates the budget. The assembled line never exceeds width and
// is always a single line.
func renderStatusBar(width int, left, right []statusSeg) string {
	if width < 1 {
		width = 1
	}

	leftStyled, leftPlain := joinSegs(left)
	leftW := taui.VisibleWidth(leftPlain)

	// Mutable copy of non-empty right segments to drop from under width pressure.
	rights := make([]statusSeg, 0, len(right))
	for _, s := range right {
		if s.text != "" {
			rights = append(rights, s)
		}
	}
	rightStyled, rightPlain := joinSegs(rights)
	rightW := taui.VisibleWidth(rightPlain)

	gap := func(rw int) int {
		if leftW > 0 && rw > 0 {
			return 1
		}
		return 0
	}

	// Drop the lowest-priority droppable right segment until the two groups fit.
	for leftW+gap(rightW)+rightW > width && len(rights) > 0 {
		idx := -1
		for i, s := range rights {
			if s.prio >= prioTransient {
				continue
			}
			if idx == -1 || s.prio < rights[idx].prio {
				idx = i
			}
		}
		if idx == -1 {
			break // only transient segments remain
		}
		rights = append(rights[:idx], rights[idx+1:]...)
		rightStyled, rightPlain = joinSegs(rights)
		rightW = taui.VisibleWidth(rightPlain)
	}

	// No right group: the left group occupies the line on its own.
	if rightW == 0 {
		if leftW > width {
			return taui.TruncateANSIToWidth(leftStyled, width, "…")
		}
		return leftStyled
	}

	// Both groups present and fitting: right-justify the right group.
	if leftW+gap(rightW)+rightW <= width {
		pad := max(width-leftW-rightW, 1)
		return leftStyled + strings.Repeat(" ", pad) + rightStyled
	}

	// Right group can't shrink further (transient only) yet still overflows.
	// Truncate the left group to make room, keeping the right group flush-right.
	avail := width - 1 - rightW
	if avail < 1 {
		return taui.TruncateANSIToWidth(rightStyled, width, "…")
	}
	leftTrunc := taui.TruncateANSIToWidth(leftStyled, avail, "…")
	pad := max(width-taui.VisibleWidth(leftTrunc)-rightW, 1)
	return leftTrunc + strings.Repeat(" ", pad) + rightStyled
}

// ── Pure formatters ───────────────────────────────────────────────────────────

// humanizeTokens renders a token count compactly: "942", "15.2k", "1.3M".
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

// formatCost renders a USD amount: "$0.0182" under $1 (4dp), else "$1.23" (2dp).
func formatCost(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// contextPct returns the rounded context-window utilisation percentage, or -1
// when it can't be computed (no prompt tokens or no known window).
func contextPct(promptTok, ctxWindow int) int {
	if promptTok <= 0 || ctxWindow <= 0 {
		return -1
	}
	return int(math.Round(float64(promptTok) / float64(ctxWindow) * 100))
}

// formatContextPct renders the context-window utilisation, e.g. "ctx 41%", or ""
// when it isn't computable.
func formatContextPct(promptTok, ctxWindow int) string {
	p := contextPct(promptTok, ctxWindow)
	if p < 0 {
		return ""
	}
	return fmt.Sprintf("ctx %d%%", p)
}

// contextStyle picks a subtle severity colour for the context widget: grey below
// 75%, amber from 75%, red from 90%. A nil result means default grey.
func contextStyle(pct int) func(string) string {
	switch {
	case pct >= 90:
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorRed) }
	case pct >= 75:
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorAmber) }
	default:
		return nil
	}
}
