package tui2

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- status bar segments ---------------------------------------------------

// statusSeg is one widget in the status bar.
type statusSeg struct {
	text  string
	style func(string) string // nil = default grey
	prio  int                 // lower is dropped first under width pressure
}

const (
	prioWeb       = 1
	prioCost      = 2
	prioTokens    = 3
	prioContext   = 4
	prioTransient = 100
)

const statusSep = " · "

func statusGrey(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) }

// --- rendering -------------------------------------------------------------

// renderStatusBar assembles left and right segment groups into a single line
// that never exceeds width. Right segments are priority-dropped under pressure.
// Width is measured on plain text so ANSI never inflates the budget.
func renderStatusBar(width int, left, right []statusSeg) string {
	if width < 1 {
		width = 1
	}

	leftStyled, leftPlain := joinSegs(left)
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

	// Drop lowest-priority right segments only once the right side can't
	// stand on its own within width — the left identity blob always yields
	// (via truncation below) before any right segment is dropped.
	for rightW+gap(rightW) > width && len(rights) > 0 {
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
			break
		}
		rights = append(rights[:idx], rights[idx+1:]...)
		rightStyled, rightPlain = joinSegs(rights)
		rightW = visibleWidth(rightPlain)
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
		style := s.style
		if style == nil {
			style = statusGrey
		}
		sb.WriteString(style(s.text))
		pb.WriteString(s.text)
	}
	return sb.String(), pb.String()
}

// --- computeStatusBar ------------------------------------------------------

// computeStatusBar builds the left and right segment groups from current model
// state. Called synchronously in View() — no goroutine, no Tick needed.
func (m *model) computeStatusBar() string {
	// Left group: identity.
	left := []statusSeg{
		{text: "τ tau"},
	}
	if m.modelName != "" {
		left = append(left, statusSeg{text: m.modelName, style: boldText})
	}
	if m.provider != "" && m.provider != m.modelName {
		left = append(left, statusSeg{text: m.provider})
	}
	// Reasoning effort.
	if m.reasoningEffort != "" && m.reasoningEffort != "auto" {
		left = append(left, statusSeg{text: m.reasoningEffort})
	}
	// Steering indicator — animated dots cycling 3 frames every 300ms.
	if m.steering {
		dots := int(time.Now().UnixMilli()/300) % 4
		steerText := "steering" + strings.Repeat(".", dots)
		left = append(left, statusSeg{
			text:  steerText,
			style: func(s string) string { return termkit.FgOnly(s, termkit.ColorAmber) },
		})
	}

	// Right group: metrics.
	var right []statusSeg

	// Token usage.
	if m.usage != nil {
		totals := m.usage.Snapshot(m.sessionID)
		if totals != nil && totals.TotalTokens > 0 {
			right = append(right, statusSeg{
				text: humanizeTokens(totals.TotalTokens) + " session tok",
				prio: prioTokens,
			})
			if totals.Cost > 0 {
				right = append(right, statusSeg{
					text: formatCost(totals.Cost),
					prio: prioCost,
				})
			}
			// Context window %.
			if pct := contextPct(totals.LastPromptTokens, m.ctxWindow); pct >= 0 {
				s := statusSeg{
					text:  fmt.Sprintf("ctx %d%%", pct),
					style: contextStyle(pct),
					prio:  prioContext,
				}
				right = append(right, s)
			}
		}
	}

	// Web URL.
	if m.webURL != "" {
		right = append(right, statusSeg{text: "web: " + m.webURL, prio: prioWeb})
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
// internal/tui/statusbar.go's function of the same name — used by /cost's
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
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorRed) }
	case pct >= 75:
		return func(s string) string { return termkit.FgOnly(s, termkit.ColorAmber) }
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

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteByte(s[i])
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
