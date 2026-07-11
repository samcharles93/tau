package tui2

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
)

// helpRow is one keybinding entry: the key combo and what it does.
type helpRow struct {
	key  string
	desc string
}

// helpSection groups related keybindings under a titled divider.
type helpSection struct {
	title string
	rows  []helpRow
}

// helpSections is the single source of truth for /help's keybinding
// listing — slash commands are deliberately not repeated here, since they
// already live in the "/" completions dropdown (internal/tui2/completions.go).
// Indices into this slice are what helpRowKey.section refers to.
var helpSections = []helpSection{
	{
		title: "Input & Editing",
		rows: []helpRow{
			{"Ctrl+A / Home", "move cursor to start of line"},
			{"Ctrl+E / End", "move cursor to end of line"},
			{"Ctrl+Left / Alt+Left", "move cursor back one word"},
			{"Ctrl+Right / Alt+Right", "move cursor forward one word"},
			{"Ctrl+U", "delete from cursor to start of line"},
			{"Ctrl+K", "delete from cursor to end of line"},
			{"Ctrl+W / Ctrl+Backspace", "delete word before cursor"},
			{"Shift+Enter / Ctrl+J", "insert newline (multi-line input)"},
			{"Shift+Tab", "cycle chat and agent modes"},
			{"Up / Down", "recall history (at first / last line)"},
			{"Ctrl+D", "quit (when input is empty)"},
		},
	},
	{
		title: "Tool Interaction",
		rows: []helpRow{
			{"Tab", "navigate focus through completed tools"},
			{"Enter", "expand/collapse the focused tool's output"},
			{"Esc", "collapse tool, then clear focus, then clear input"},
			{"Up / Down", "navigate tool focus (input empty, no history)"},
		},
	},
	{
		title: "Screen & Navigation",
		rows: []helpRow{
			{"Ctrl+Shift+L", "clear the screen (keeps the session)"},
			{"Ctrl+Shift+G", "copy the assistant's last response"},
			{"PageUp / PageDown", "scroll through conversation history"},
			{"Ctrl+Home / Ctrl+End", "jump to oldest / newest content"},
			{"Tab", "cycle tab-completions when dropdown is visible"},
		},
	},
	{
		title: "Turn Control & Shell",
		rows: []helpRow{
			{"Ctrl+S", "steer the agent mid-turn"},
			{"Ctrl+C", "cancel the current turn (double-tap to quit)"},
			{"!", "run a shell command prefixed with ! (!! to hide from model)"},
		},
	},
}

// helpTwoColumnMinWidth is the narrowest terminal that still gets the
// side-by-side layout; anything narrower falls back to one section per row
// (stacked) so labels/descriptions never get crushed into truncation.
const helpTwoColumnMinWidth = 90

var (
	helpTitleStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	helpDividerStyle = lipgloss.NewStyle().Faint(true)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(themeHex(theme.ToneBody)).Bold(true)
	helpDescStyle    = lipgloss.NewStyle().Faint(true)
)

// helpRowKey identifies one row of helpSections[section].rows[row] — the
// unit a click expands/collapses.
type helpRowKey struct {
	section int
	row     int
}

// helpRowHit records where one row landed in the rendered box, for click
// hit-testing (see toggleCommittedHelpAtLine in model.go): startY/endY are
// line offsets relative to the box's first body line (0 = the line right
// after the top border); startX/endX are column offsets within a fully
// composed, padded body line (i.e. already accounting for the border/pad
// prefix and, for the right column, the left column's width + gap) — so a
// hit-test only ever needs rel-Y (idx - g.lineIdx - 1) and raw mouse X, no
// further translation.
type helpRowHit struct {
	startY, endY int
	startX, endX int
	key          helpRowKey
}

// helpBoxTitle is the /help overlay's title, embedded in its top border.
const helpBoxTitle = "Help: Keybindings & Controls"

// renderHelpBox builds the full, unclipped /help output: a bordered box
// containing every helpSections group, laid out as two columns (Input &
// Editing on the left; Tool Interaction and Screen & Navigation stacked on
// the right) with Turn Control & Shell spanning full width below,
// mirroring the two side columns' combined height. Below
// helpTwoColumnMinWidth it falls back to one section per row, stacked
// top-to-bottom. Used directly by tests and anywhere the whole box (no
// height clamping) is wanted; the live overlay instead calls
// renderHelpBody + renderBoxAround itself so it can clip to the terminal's
// available height — see renderHelpOverlayBox.
func renderHelpBox(width int, expanded map[helpRowKey]bool) (rendered string, hits []helpRowHit) {
	body, hits := renderHelpBody(width, expanded)
	return renderBoxAround(width, helpBoxTitle, body), hits
}

// renderHelpBody builds the /help content lines (no border) for the given
// outer box width — see renderHelpBox's doc comment for the layout this
// produces. expanded holds which rows are showing their full (word-wrapped)
// description instead of a truncated one-liner. The returned hits describe
// where each row landed, in body-relative (unclipped) line coordinates —
// callers that clip the body for a scrollable window (renderHelpOverlayBox)
// must translate a click's Y by their own scroll offset before comparing
// against these.
func renderHelpBody(width int, expanded map[helpRowKey]bool) (body []string, hits []helpRowHit) {
	if width <= 0 {
		width = 80
	}
	innerWidth := max(width-4, 20) // 2 border cols + 1 padding col each side
	const contentOffsetX = 2       // "│ " prefix added by renderBoxAround

	if innerWidth >= helpTwoColumnMinWidth-4 {
		leftSecs := []int{0}
		rightSecs := []int{1, 2}
		colGap := 3
		leftColWidth, rightColWidth := splitColumnWidths(
			helpColumnNaturalWidth(leftSecs),
			helpColumnNaturalWidth(rightSecs),
			innerWidth-colGap,
		)
		leftCol, leftHits := renderHelpColumn(leftSecs, leftColWidth, expanded)
		rightCol, rightHits := renderHelpColumn(rightSecs, rightColWidth, expanded)
		body = joinHelpColumns(leftCol, rightCol, leftColWidth, rightColWidth, colGap)

		for _, h := range leftHits {
			h.startX += contentOffsetX
			h.endX += contentOffsetX
			hits = append(hits, h)
		}
		rightOffsetX := contentOffsetX + leftColWidth + colGap
		for _, h := range rightHits {
			h.startX += rightOffsetX
			h.endX += rightOffsetX
			hits = append(hits, h)
		}

		bottomY := len(body) + 1
		body = append(body, "")
		bottomCol, bottomHits := renderHelpColumn([]int{3}, innerWidth, expanded)
		body = append(body, bottomCol...)
		for _, h := range bottomHits {
			h.startY += bottomY
			h.endY += bottomY
			h.startX += contentOffsetX
			h.endX += contentOffsetX
			hits = append(hits, h)
		}
	} else {
		for i, secIdx := range []int{0, 1, 2, 3} {
			if i > 0 {
				body = append(body, "")
			}
			y := len(body)
			col, colHits := renderHelpColumn([]int{secIdx}, innerWidth, expanded)
			body = append(body, col...)
			for _, h := range colHits {
				h.startY += y
				h.endY += y
				h.startX += contentOffsetX
				h.endX += contentOffsetX
				hits = append(hits, h)
			}
		}
	}

	return body, hits
}

// renderHelpColumn renders one or more helpSections (identified by index,
// so hits can carry the right section index) stacked vertically, each row
// keyed to keyWidth (the longest key label across all sections, so the
// description column lines up even when a column holds multiple sections of
// varying label lengths). A row in expanded shows its full description
// word-wrapped under the key instead of truncated to one line.
func renderHelpColumn(sectionIdxs []int, colWidth int, expanded map[helpRowKey]bool) (lines []string, hits []helpRowHit) {
	keyWidth := 0
	for _, si := range sectionIdxs {
		for _, r := range helpSections[si].rows {
			if l := len(r.key); l > keyWidth {
				keyWidth = l
			}
		}
	}
	descWidth := max(colWidth-keyWidth-1, 1)

	for i, si := range sectionIdxs {
		s := helpSections[si]
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, helpTitleStyle.Render(strings.ToUpper(s.title)))
		lines = append(lines, helpDividerStyle.Render(strings.Repeat("─", min(colWidth, max(len(s.title), keyWidth+10)))))

		for ri, r := range s.rows {
			startY := len(lines)
			key := helpKeyStyle.Render(padRight(r.key, keyWidth))
			if expanded[helpRowKey{section: si, row: ri}] {
				for wi, wl := range wrapWords(r.desc, descWidth) {
					if wi == 0 {
						lines = append(lines, key+" "+helpDescStyle.Render(wl))
					} else {
						lines = append(lines, strings.Repeat(" ", keyWidth+1)+helpDescStyle.Render(wl))
					}
				}
			} else {
				lines = append(lines, key+" "+helpDescStyle.Render(r.desc))
			}
			hits = append(hits, helpRowHit{
				startY: startY, endY: len(lines) - 1,
				startX: 0, endX: colWidth - 1,
				key: helpRowKey{section: si, row: ri},
			})
		}
	}
	return lines, hits
}

// joinHelpColumns lays left and right out side by side, padding the shorter
// column with blank rows so the box's right border stays flush all the way
// down, and truncating any line that overflows its own column's width.
func joinHelpColumns(left, right []string, leftWidth, rightWidth, gap int) []string {
	n := max(len(left), len(right))
	gapStr := strings.Repeat(" ", gap)
	out := make([]string, n)
	for i := range n {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out[i] = padVisible(l, leftWidth) + gapStr + padVisible(r, rightWidth)
	}
	return out
}

// helpColumnNaturalWidth is the width a column needs to show every row's key
// and description in full, with no truncation: the longer of its title and
// (widest key label + 1 space + widest description).
func helpColumnNaturalWidth(sectionIdxs []int) int {
	keyWidth := 0
	descWidth := 0
	titleWidth := 0
	for _, si := range sectionIdxs {
		s := helpSections[si]
		if l := len(s.title); l > titleWidth {
			titleWidth = l
		}
		for _, r := range s.rows {
			if l := len(r.key); l > keyWidth {
				keyWidth = l
			}
			if l := len(r.desc); l > descWidth {
				descWidth = l
			}
		}
	}
	return max(titleWidth, keyWidth+1+descWidth)
}

// splitColumnWidths divides available space between two columns according
// to what each naturally needs: if both fit as-is, they keep their natural
// width (leaving any remainder as trailing blank space rather than
// stretching either column). Otherwise the available space is split
// proportionally to each column's natural width, so the wider column still
// gets more room instead of an even (and often too-narrow) 50/50 split.
func splitColumnWidths(leftNatural, rightNatural, available int) (left, right int) {
	if leftNatural+rightNatural <= available {
		return leftNatural, rightNatural
	}
	total := leftNatural + rightNatural
	if total <= 0 {
		return available / 2, available - available/2
	}
	left = available * leftNatural / total
	return left, available - left
}

// padRight pads plain (unstyled) text to width — safe to call before
// applying a lipgloss style, since there's no ANSI to miscount yet.
func padRight(s string, width int) string {
	if w := len(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// padVisible pads/truncates already-styled text to width, measuring by
// visible (ANSI-stripped) width rather than byte length.
func padVisible(s string, width int) string {
	vw := visibleWidth(s)
	if vw > width {
		s = truncateANSIToWidth(s, width, "…")
		vw = visibleWidth(s)
	}
	if vw < width {
		s += strings.Repeat(" ", width-vw)
	}
	return s
}

// renderBoxAround wraps body lines in a box using titledBoxBorders — the
// same border convention renderInputBox uses (model.go), for visual
// consistency between the input box and /help. Unlike renderInputBoxLine
// (which sits content flush against the left border to match the input
// prompt/cursor), this adds a one-space pad on both sides for readability,
// since help content has no such anchor.
func renderBoxAround(width int, title string, body []string) string {
	contentWidth := max(width-4, 1) // 2 border cols + 1 padding col each side
	top, bottom := titledBoxBorders(width, title)
	border := inputBoxStyle.Render("│")

	out := make([]string, 0, len(body)+2)
	out = append(out, top)
	for _, line := range body {
		out = append(out, border+" "+padVisible(line, contentWidth)+" "+border)
	}
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

// --- /help overlay -----------------------------------------------------

// helpOverlayWidthFrac caps how much of the terminal the /help overlay
// spans — a floating reference card doesn't need (or want) to stretch
// edge-to-edge on a wide terminal the way the old scrollback-embedded box
// did; centered and modestly sized reads more like a help card than a wall
// of text.
const (
	helpOverlayWidthFrac = 0.9
	helpOverlayMinWidth  = 40
	helpOverlayMaxWidth  = 110
)

// helpOverlayHeightFrac caps the box height at a fraction of the terminal
// (matching diffViewerHeightFrac's own margin, diff.go) rather than letting
// it run edge-to-edge — content taller than that scrolls instead (see
// renderHelpOverlayBox), keeping the surrounding chrome visible as context
// that something is layered on top of it.
const helpOverlayHeightFrac = 0.9

// helpOverlayState is the state of an open /help overlay. A nil
// *helpOverlayState on model means none is open — mirrors diffViewerState's
// own nil-sentinel convention (see diff.go). Unlike the box's previous
// scrollback-embedded incarnation, opening /help again just replaces this
// state; nothing accumulates in history. scrollOffset is the index of the
// first visible body line when content is taller than the box's available
// height; renderHelpOverlayBox clamps it to [0, maxScroll] on every render,
// so an out-of-range value set by a key handler (e.g. scrolling past the
// top) is always safe.
type helpOverlayState struct {
	expanded     map[helpRowKey]bool
	scrollOffset int
}

// helpOverlayWidth is the box width used for both rendering
// (renderHelpOverlayBox) and click hit-testing (handleHelpOverlayClick) —
// the two MUST agree, or a click would hit-test against a differently laid
// out box than the one actually on screen.
func (m *model) helpOverlayWidth() int {
	w := int(float64(m.width) * helpOverlayWidthFrac)
	return max(helpOverlayMinWidth, min(w, helpOverlayMaxWidth))
}

// renderHelpOverlayBox renders the /help overlay clipped to fit within
// helpOverlayHeightFrac of the terminal: when the full body (renderHelpBody)
// is taller than that, it's clipped to m.helpOverlay.scrollOffset..+visible
// rows and a scroll hint is appended as an extra row, exactly like
// renderHelpBox's normal footer-less box when content already fits (a short
// help box, the common case, renders byte-for-byte the same as before this
// clipping existed — scrollOffset stays 0 and maxScroll is 0, so no hint
// line is added and nothing is clipped).
//
// hits are returned in full-body (unclipped) coordinates, same as
// renderHelpBody's — see handleHelpOverlayClick for how a click's Y is
// translated back into that space via scrollOffset.
func (m *model) renderHelpOverlayBox() (rendered string, hits []helpRowHit) {
	width := m.helpOverlayWidth()
	body, hits := renderHelpBody(width, m.helpOverlay.expanded)

	maxBoxHeight := max(10, int(float64(m.height)*helpOverlayHeightFrac))
	naturalHeight := len(body) + 2 // + top/bottom border
	boxHeight := min(naturalHeight, maxBoxHeight)
	visibleBodyLines := boxHeight - 2

	overflow := len(body) > visibleBodyLines
	if overflow {
		visibleBodyLines-- // reserve a row for the scroll hint
	}
	visibleBodyLines = max(visibleBodyLines, 1)

	maxScroll := max(len(body)-visibleBodyLines, 0)
	m.helpOverlay.scrollOffset = min(max(m.helpOverlay.scrollOffset, 0), maxScroll)

	end := min(m.helpOverlay.scrollOffset+visibleBodyLines, len(body))
	visible := body[m.helpOverlay.scrollOffset:end]
	if overflow {
		visible = append(append([]string{}, visible...), helpDescStyle.Render("↑/↓  PgUp/PgDn to scroll  ·  any other key closes"))
	}

	return renderBoxAround(width, helpBoxTitle, visible), hits
}

// handleHelpOverlayKey handles keyboard input while the /help overlay is
// open: Up/Down/PgUp/PgDn scroll it when content overflows (a no-op,
// harmlessly clamped by renderHelpOverlayBox, when it doesn't); any other
// key closes it — a reference card isn't something you type into, so
// there's no reason to route other keys anywhere else.
func (m *model) handleHelpOverlayKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up":
		m.helpOverlay.scrollOffset--
		return nil
	case "down":
		m.helpOverlay.scrollOffset++
		return nil
	case "pgup":
		m.helpOverlay.scrollOffset -= 10
		return nil
	case "pgdown":
		m.helpOverlay.scrollOffset += 10
		return nil
	}
	m.helpOverlay = nil
	return nil
}

// handleHelpOverlayClick resolves a left-click while the /help overlay is
// open: inside a row toggles that row's expansion; inside the box but not
// on a row is a no-op; outside the box closes it, mirroring
// handleContextMenuClick's click-away-to-dismiss convention. relY is
// translated from on-screen (clipped/scrolled) coordinates back into
// renderHelpBody's full-body coordinate space — the same space hits is
// already in — by adding the current scroll offset.
func (m *model) handleHelpOverlayClick(x, y int) tea.Cmd {
	rendered, hits := m.renderHelpOverlayBox()
	bx, by := centerRect(m.width, m.height, lipgloss.Width(rendered), lipgloss.Height(rendered))

	relX := x - bx
	relY := y - by - 1 + m.helpOverlay.scrollOffset // -1: body lines start after the top border
	for _, h := range hits {
		if relY < h.startY || relY > h.endY || relX < h.startX || relX > h.endX {
			continue
		}
		m.helpOverlay.expanded[h.key] = !m.helpOverlay.expanded[h.key]
		return nil
	}

	boxW, boxH := lipgloss.Width(rendered), lipgloss.Height(rendered)
	if x < bx || x >= bx+boxW || y < by || y >= by+boxH {
		m.helpOverlay = nil
	}
	return nil
}

// compositeHelpOverlay overlays the open /help box centered on top of base,
// following compositeDiffViewer's lipgloss.Compositor shape (see
// compositeContextMenu's doc comment for why a bare Layer.Draw isn't
// enough).
func (m *model) compositeHelpOverlay(base string) string {
	rendered, _ := m.renderHelpOverlayBox()
	bx, by := centerRect(m.width, m.height, lipgloss.Width(rendered), lipgloss.Height(rendered))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(rendered).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
