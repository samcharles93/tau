package taui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// StatusLineSeg is one widget in a status line (the single-row bar shown at
// the top or bottom of a chat UI). Text is the PLAIN string used for width
// math; Style applies ANSI/lipgloss styling and is nil for the frontend's
// default styling.
//
// When StyledOverride is non-empty it replaces Style(Text) in the styled
// output (Text is still used for width measurement). This lets a segment
// embed raw escape sequences the width math can't see through - e.g. an
// OSC 8 hyperlink, whose terminator isn't the SGR 'm' StripANSI/VisibleWidth
// expect.
//
// Prio orders width-pressure dropping: lower-priority segments are dropped
// first. A segment with Prio >= StatusLinePrioTransient is never dropped by
// RenderStatusLine's width-pressure loop.
type StatusLineSeg struct {
	Text           string
	Style          func(string) string
	StyledOverride string
	Prio           int
}

// StatusLineSep is the separator joining segments within a group.
const StatusLineSep = " · "

// StatusLinePrioTransient marks a segment as effectively undroppable by
// RenderStatusLine's width-pressure loop. Frontends should use this exact
// value (not a locally redefined constant) for any segment that must
// survive at any width - e.g. the app's own identity label or an
// in-progress activity indicator - so the "never drop" threshold can't
// silently drift between frontends.
const StatusLinePrioTransient = 100

// JoinStatusLineSegs renders a group of segments to (styled, plain). The
// plain form carries no ANSI so it can be measured directly with
// VisibleWidth; the styled form is what gets emitted.
//
// defaultStyle styles any segment (and the separators between segments)
// that doesn't specify its own Style - e.g. internal/tui's grey-via-termkit
// or internal/tui2's dim-via-lipgloss. Styling is a frontend concern, so
// this shared function never picks a style itself; pass nil to leave
// unstyled segments and separators as plain text (e.g. in tests).
func JoinStatusLineSegs(segs []StatusLineSeg, defaultStyle func(string) string) (styled, plain string) {
	if defaultStyle == nil {
		defaultStyle = func(s string) string { return s }
	}
	var sb, pb strings.Builder
	first := true
	for _, s := range segs {
		if s.Text == "" {
			continue
		}
		if !first {
			sb.WriteString(defaultStyle(StatusLineSep))
			pb.WriteString(StatusLineSep)
		}
		first = false
		if s.StyledOverride != "" {
			sb.WriteString(s.StyledOverride)
		} else {
			style := s.Style
			if style == nil {
				style = defaultStyle
			}
			sb.WriteString(style(s.Text))
		}
		pb.WriteString(s.Text)
	}
	return sb.String(), pb.String()
}

// RenderStatusLine assembles left and right segment groups into a single
// line that never exceeds width. Both sides are priority-dropped whole-
// segment-at-a-time under width pressure before either resorts to
// character-level truncation - dropping whole segments first avoids
// chopping one mid-word (e.g. a provider name truncated to a meaningless
// "o…"). Width is measured on plain text so ANSI never inflates the
// budget.
func RenderStatusLine(width int, left, right []StatusLineSeg, defaultStyle func(string) string) string {
	if width < 1 {
		width = 1
	}

	lefts := nonEmptySegs(left)
	leftStyled, leftPlain := JoinStatusLineSegs(lefts, defaultStyle)
	leftW := VisibleWidth(leftPlain)

	rights := nonEmptySegs(right)
	rightStyled, rightPlain := JoinStatusLineSegs(rights, defaultStyle)
	rightW := VisibleWidth(rightPlain)

	leftFloorW := statusFloorWidth(lefts, defaultStyle)

	// Drop lowest-priority right segments until the right side no longer
	// crowds out the left side's guaranteed floor. This loop alone
	// establishes leftFloorW+gap+rightW <= width whenever right has
	// anything droppable left to give, and dropping left below (in the
	// next loop) never grows rightW back - so a second right-side pass
	// afterward would be redundant.
	for leftFloorW+statusGap(leftW, rightW)+rightW > width {
		next, styled, w, ok := dropLowestPrio(rights, defaultStyle)
		if !ok {
			break
		}
		rights, rightStyled, rightW = next, styled, w
	}

	// Then drop lowest-priority left segments (whole segments, not
	// characters) until the combined line fits, or only left's own floor
	// remains - symmetric with the right-side loop above.
	for leftW+statusGap(leftW, rightW)+rightW > width && leftW > leftFloorW {
		next, styled, w, ok := dropLowestPrio(lefts, defaultStyle)
		if !ok {
			break
		}
		lefts, leftStyled, leftW = next, styled, w
	}

	return fitStatusLine(width, leftStyled, leftW, rightStyled, rightW)
}

// nonEmptySegs returns the segments with non-empty text.
func nonEmptySegs(segs []StatusLineSeg) []StatusLineSeg {
	out := make([]StatusLineSeg, 0, len(segs))
	for _, s := range segs {
		if s.Text != "" {
			out = append(out, s)
		}
	}
	return out
}

// statusGap is the separator width between the left and right groups: one
// column when both sides are present, zero otherwise.
func statusGap(leftW, rightW int) int {
	if leftW > 0 && rightW > 0 {
		return 1
	}
	return 0
}

// dropLowestPrio removes the lowest-priority segment (skipping anything
// >= StatusLinePrioTransient, which is meant to survive any width) from
// segs, returning the updated slice/styled/width, or ok=false when nothing
// left is droppable.
func dropLowestPrio(segs []StatusLineSeg, defaultStyle func(string) string) (out []StatusLineSeg, styled string, w int, ok bool) {
	idx := -1
	for i, s := range segs {
		if s.Prio >= StatusLinePrioTransient {
			continue
		}
		if idx == -1 || s.Prio < segs[idx].Prio {
			idx = i
		}
	}
	if idx == -1 {
		return segs, "", 0, false
	}
	out = slices.Delete(slices.Clone(segs), idx, idx+1)
	styled, plain := JoinStatusLineSegs(out, defaultStyle)
	return out, styled, VisibleWidth(plain), true
}

// statusFloorWidth is the width of segs once every droppable (Prio <
// StatusLinePrioTransient) segment is gone - the minimum space this side is
// guaranteed to need no matter how much gets dropped from it. The right-drop
// loop must reserve this much room for the left side (and vice versa) rather
// than just checking whether it fits alone - otherwise an undroppable left
// could still lose content to the character-truncation fallback while purely
// decorative right segments survive untouched, which is backwards from
// "prioritize undroppable content over secondary metadata."
func statusFloorWidth(segs []StatusLineSeg, defaultStyle func(string) string) int {
	var floor []StatusLineSeg
	for _, s := range segs {
		if s.Prio >= StatusLinePrioTransient {
			floor = append(floor, s)
		}
	}
	_, plain := JoinStatusLineSegs(floor, defaultStyle)
	return VisibleWidth(plain)
}

// fitStatusLine assembles the final line from the pre-joined styled sides and
// their plain widths, falling back to character-level truncation when
// whole-segment dropping couldn't make it fit.
func fitStatusLine(width int, leftStyled string, leftW int, rightStyled string, rightW int) string {
	gap := statusGap(leftW, rightW)
	if rightW == 0 {
		if leftStyled == "" {
			return strings.Repeat(" ", width)
		}
		if leftW > width {
			return TruncateANSIToWidth(leftStyled, width, "…")
		}
		return leftStyled
	}

	if leftW+gap+rightW <= width {
		pad := max(width-leftW-rightW, 1)
		return leftStyled + strings.Repeat(" ", pad) + rightStyled
	}

	// Whole-segment dropping above couldn't make it fit (e.g. every
	// remaining segment is at or above StatusLinePrioTransient) - fall back
	// to character-level truncation as a last resort.
	avail := width - gap - rightW
	if avail < 1 {
		return TruncateANSIToWidth(rightStyled, width, "…")
	}
	leftTrunc := TruncateANSIToWidth(leftStyled, avail, "…")
	pad := max(width-VisibleWidth(leftTrunc)-rightW, 1)
	return leftTrunc + strings.Repeat(" ", pad) + rightStyled
}

// ── Pure formatters shared by every status-line frontend ──────────────────

// HumanizeTokens renders a token count compactly: "942", "15.2k", "1.3M".
func HumanizeTokens(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

// FormatCost renders a USD amount: "$0.0182" under $1 (4dp), else "$1.23" (2dp).
func FormatCost(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// ContextPct returns the rounded context-window utilisation percentage, or
// -1 when it can't be computed (no prompt tokens or no known window).
func ContextPct(promptTok, ctxWindow int) int {
	if promptTok <= 0 || ctxWindow <= 0 {
		return -1
	}
	return int(math.Round(float64(promptTok) / float64(ctxWindow) * 100))
}

// FormatContextPct renders the context-window utilisation, e.g. "41%", or
// "" when it isn't computable.
func FormatContextPct(promptTok, ctxWindow int) string {
	p := ContextPct(promptTok, ctxWindow)
	if p < 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", p)
}

// ContextSeverity buckets a context-window utilisation percentage into a
// severity tier, so both frontends apply the same 75%/90% thresholds
// without duplicating the magic numbers - each frontend still maps the
// tier to its own color system (termkit vs lipgloss/theme) locally.
type ContextSeverity int

const (
	ContextNormal ContextSeverity = iota
	ContextWarn
	ContextCritical
)

// ContextSeverityFor returns the severity tier for a context-window
// utilisation percentage: >=90% critical, >=75% warn, otherwise normal.
func ContextSeverityFor(pct int) ContextSeverity {
	switch {
	case pct >= 90:
		return ContextCritical
	case pct >= 75:
		return ContextWarn
	default:
		return ContextNormal
	}
}
