package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// clearInput resets the input buffer and cursor together - every reset site
// must clear both or the cursor can end up pointing past the end of a
// shorter (or empty) buffer.
func (m *model) clearInput() {
	m.input = ""
	m.inputCursor = 0
	m.inputModeCommand = ""
	m.inputSel.clear()
	m.compDismissed = false
	m.compDismissedToken = ""
}

// clearScreen wipes the visible scrollback (Ctrl+Shift+L) without touching the
// underlying chat session - unlike /clear, which sends a
// ResetChatSessionCommand and actually starts a new session, this only
// clears what's rendered locally. The next ChatSessionSnapshotEvent (e.g.
// from /session, /resume) still rebuilds renderedLines from the real
// session history, so nothing is actually lost.
func (m *model) clearScreen() {
	m.renderedLines = m.renderedLines[:0]
	m.committedGroups = m.committedGroups[:0]
	m.committedReasoning = m.committedReasoning[:0]
	m.viewport.SetContentLines(m.renderedLines)
	m.autoFollow = true
	m.viewportSel.clear()
	m.viewport.GotoBottom()
}

// deleteInputSelection removes the currently selected input range (if any),
// moves the cursor to where the selection started, and clears it. Reports
// whether there was a selection to consume, so insertAtCursor/backspaceAt-
// Cursor/deleteAtCursor can fall back to their normal single-character
// behavior when there wasn't one - this is what makes typing/backspace/
// delete replace or remove a mouse-dragged selection, exactly like any
// normal text field.
func (m *model) deleteInputSelection() bool {
	lo, hi, ok := m.inputSel.bounds()
	if !ok {
		return false
	}
	rs := []rune(m.input)
	lo = max(min(lo, len(rs)), 0)
	hi = max(min(hi, len(rs)), 0)
	rs = append(rs[:lo], rs[hi:]...)
	m.input = string(rs)
	m.inputCursor = lo
	m.inputSel.clear()
	return true
}

func (m *model) insertAtCursor(s string) {
	m.deleteInputSelection()
	rs := []rune(m.input)
	ins := []rune(s)
	nr := make([]rune, 0, len(rs)+len(ins))
	nr = append(nr, rs[:m.inputCursor]...)
	nr = append(nr, ins...)
	nr = append(nr, rs[m.inputCursor:]...)
	m.input = string(nr)
	m.inputCursor += len(ins)
}

func (m *model) backspaceAtCursor() {
	if m.deleteInputSelection() {
		return
	}
	if m.inputCursor <= 0 {
		return
	}
	rs := []rune(m.input)
	rs = append(rs[:m.inputCursor-1], rs[m.inputCursor:]...)
	m.input = string(rs)
	m.inputCursor--
}

func (m *model) deleteAtCursor() {
	if m.deleteInputSelection() {
		return
	}
	rs := []rune(m.input)
	if m.inputCursor >= len(rs) {
		return
	}
	rs = append(rs[:m.inputCursor], rs[m.inputCursor+1:]...)
	m.input = string(rs)
}

func (m *model) moveCursorLeft() {
	if m.inputCursor > 0 {
		m.inputCursor--
	}
}

func (m *model) moveCursorRight() {
	if m.inputCursor < utf8.RuneCountInString(m.input) {
		m.inputCursor++
	}
}

func (m *model) killToLineStart() {
	rs := []rune(m.input)
	m.input = string(rs[m.inputCursor:])
	m.inputCursor = 0
}

func (m *model) killToLineEnd() {
	rs := []rune(m.input)
	if m.inputCursor < len(rs) {
		m.input = string(rs[:m.inputCursor])
	}
}

func (m *model) wordLeft() int {
	rs := []rune(m.input)
	i := m.inputCursor
	for i > 0 && rs[i-1] == ' ' {
		i--
	}
	for i > 0 && rs[i-1] != ' ' && rs[i-1] != '\n' {
		i--
	}
	return i
}

func (m *model) wordRight() int {
	rs := []rune(m.input)
	i := m.inputCursor
	for i < len(rs) && rs[i] != ' ' && rs[i] != '\n' {
		i++
	}
	for i < len(rs) && rs[i] == ' ' {
		i++
	}
	return i
}

func (m *model) deleteWordBeforeCursor() {
	i := m.wordLeft()
	rs := []rune(m.input)
	rs = append(rs[:i], rs[m.inputCursor:]...)
	m.input = string(rs)
	m.inputCursor = i
}

// atFirstLineStart/atLastLineEnd gate Up/Down between history recall and
// intra-buffer cursor movement (see the "up"/"down" cases in handleKey).
func (m *model) atFirstLineStart() bool { return m.inputCursor == 0 }

func (m *model) atLastLineEnd() bool {
	return m.inputCursor >= utf8.RuneCountInString(m.input)
}

// splitInputLines returns the logical lines (split on '\n') of m.input.
func (m *model) splitInputLines() [][]rune {
	rs := []rune(m.input)
	var out [][]rune
	start := 0
	for i, r := range rs {
		if r == '\n' {
			out = append(out, rs[start:i])
			start = i + 1
		}
	}
	return append(out, rs[start:])
}

// cursorLineCol returns (line index, column) for the current cursor.
func (m *model) cursorLineCol(lines [][]rune) (int, int) {
	pos := 0
	for idx, ln := range lines {
		end := pos + len(ln)
		if m.inputCursor <= end {
			return idx, m.inputCursor - pos
		}
		pos = end + 1 // +1 for the newline
	}
	return len(lines) - 1, len(lines[len(lines)-1])
}

// linePos returns the absolute cursor index for (line, col) within lines.
func linePos(lines [][]rune, targetLine, col int) int {
	pos := 0
	for i := range targetLine {
		pos += len(lines[i]) + 1
	}
	return pos + min(col, len(lines[targetLine]))
}

// moveCursorVert moves the cursor up (-1) or down (+1) one logical line,
// preserving column where possible.
func (m *model) moveCursorVert(dir int) {
	lines := m.splitInputLines()
	curLine, curCol := m.cursorLineCol(lines)
	target := curLine + dir
	if target < 0 || target >= len(lines) {
		return
	}
	col := min(curCol, len(lines[target]))
	m.inputCursor = linePos(lines, target, col)
}

// renderInputArea draws the (possibly multi-line) input buffer with a
// highlighted block at the caret position - mirrors pkg/taui/lineinput.go's
// visual cursor (a coloured background over the character under the cursor)
// rather than the terminal's native cursor, so it composes as plain content
// alongside the rest of View()'s single string.
func (m *model) renderInputArea() string {
	lines := m.splitInputLines()
	curLine, curCol := m.cursorLineCol(lines)
	selLoAbs, selHiAbs, hasSel := m.inputSel.bounds()

	boxWidth := m.width
	if boxWidth <= 0 {
		boxWidth = 80
	}
	innerWidth := max(boxWidth-2, 1)
	body := make([]string, 0, len(lines))
	cursorBodyIdx := 0
	for i, ln := range lines {
		prefix := inputPromptStyle.Render("> ")
		if m.steering {
			prefix = inputSteerPromptStyle.Render("(steer) > ")
		}
		prefixWidth := visibleWidth(stripANSI(prefix))
		contentWidth := max(innerWidth-prefixWidth, 1)
		continuationPrefix := strings.Repeat(" ", prefixWidth)

		// Selection bounds are absolute rune positions into m.input; convert
		// to this line's own local rune range so per-chunk rendering below
		// can treat them the same way it already treats curCol.
		lineStartAbs := linePos(lines, i, 0)
		selLo, selHi, lineHasSel := 0, 0, false
		if hasSel {
			lo := max(selLoAbs-lineStartAbs, 0)
			hi := min(selHiAbs-lineStartAbs, len(ln))
			lineHasSel = lo < hi
			selLo, selHi = lo, hi
		}

		chunks := wrapInputLine(ln, contentWidth)
		if i == curLine && curCol == len(ln) && len(chunks) > 0 {
			last := chunks[len(chunks)-1]
			if visibleWidth(string(ln[last.start:last.end])) >= contentWidth {
				chunks = append(chunks, inputLineChunk{start: len(ln), end: len(ln)})
			}
		}
		for j, chunk := range chunks {
			rowPrefix := prefix
			if i > 0 || j > 0 {
				rowPrefix = continuationPrefix
			}

			hasCursor := i == curLine && curCol >= chunk.start && curCol < chunk.end
			if i == curLine && curCol == len(ln) && j == len(chunks)-1 {
				hasCursor = true
			}
			if hasCursor {
				cursorBodyIdx = len(body)
			}
			switch {
			case !hasCursor && !lineHasSel:
				body = append(body, rowPrefix+inputStyle.Render(string(ln[chunk.start:chunk.end])))
			default:
				body = append(body, rowPrefix+renderInputChunk(ln, chunk.start, chunk.end, hasCursor, curCol, lineHasSel, selLo, selHi))
			}
		}
	}
	if desc := m.inputModePlaceholder(); desc != "" {
		prefixWidth := visibleWidth(stripANSI(inputPromptStyle.Render("> ")))
		if m.steering {
			prefixWidth = visibleWidth(stripANSI(inputSteerPromptStyle.Render("(steer) > ")))
		}
		body = append(body, strings.Repeat(" ", prefixWidth)+inputPlaceholderStyle.Render(desc))
	}

	title := m.inputModeTitle()
	body, hint := m.clipInputBody(body, cursorBodyIdx)
	return renderInputBox(m.width, title, body, hint)
}

// inputPositionAt maps a (row, col) coordinate within the rendered input
// box's text area - row 0 is the box's first body row (i.e. right below
// its top border and hint row, see the bodyRowOffset constant below); col 0
// is the first column after the left border and the "> "/"(steer) > "
// prefix (or the matching blank indent on a continuation row) - to a rune
// index into m.input. This is the inverse of renderInputArea's own layout
// math (same wrapInputLine/linePos calls), so a click positions the cursor
// at exactly the character under the mouse.
//
// It does not replicate renderInputArea's one edge case where an extra
// trailing empty chunk is inserted when the *current* cursor sits exactly
// at a wrap boundary (that tweak depends on where the cursor already is,
// not on the click being mapped) - a click landing on that extra row in
// that narrow case can be off by one row. Acceptable: it only affects a
// click's target position, never what gets copied once selected, and it's
// a rare edge case (cursor exactly at a wrap boundary on a wrapped line).
func (m *model) inputPositionAt(row, col int) int {
	const bodyRowOffset = 2 // top border + hint row precede body rows

	lines := m.splitInputLines()
	bodyRow := row - bodyRowOffset
	if bodyRow < 0 {
		return 0
	}

	prefix := inputPromptStyle.Render("> ")
	if m.steering {
		prefix = inputSteerPromptStyle.Render("(steer) > ")
	}
	prefixWidth := visibleWidth(stripANSI(prefix))
	boxWidth := m.width
	if boxWidth <= 0 {
		boxWidth = 80
	}
	innerWidth := max(boxWidth-2, 1)
	contentWidth := max(innerWidth-prefixWidth, 1)
	textCol := max(col-1-prefixWidth, 0) // -1 for the left border column

	renderedRow := 0
	for i, ln := range lines {
		for _, chunk := range wrapInputLine(ln, contentWidth) {
			if renderedRow == bodyRow {
				rel := min(textCol, chunk.end-chunk.start)
				return linePos(lines, i, chunk.start+rel)
			}
			renderedRow++
		}
	}
	// Past all rendered lines (e.g. the placeholder row, or below the
	// content) - snap to the very end of the buffer.
	return utf8.RuneCountInString(m.input)
}

// inputSelectionText returns the substring of m.input between rune
// positions lo and hi (half-open - these are cursor positions, not
// inclusive line indices like the viewport's), for finalizeSelection.
func (m *model) inputSelectionText(lo, hi int) string {
	runes := []rune(m.input)
	lo = max(min(lo, len(runes)), 0)
	hi = max(min(hi, len(runes)), 0)
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

type inputLineChunk struct {
	start int
	end   int
}

func wrapInputLine(ln []rune, maxWidth int) []inputLineChunk {
	if maxWidth < 1 {
		maxWidth = 1
	}
	if len(ln) == 0 {
		return []inputLineChunk{{start: 0, end: 0}}
	}

	var chunks []inputLineChunk
	start, width := 0, 0
	for i, r := range ln {
		rw := max(visibleWidth(string(r)), 1)
		if width > 0 && width+rw > maxWidth {
			chunks = append(chunks, inputLineChunk{start: start, end: i})
			start = i
			width = 0
		}
		width += rw
	}
	chunks = append(chunks, inputLineChunk{start: start, end: len(ln)})
	return chunks
}

func (m *model) inputModePlaceholder() string {
	if m.inputModeCommand != "" && strings.TrimSpace(m.input) == "" {
		if entry, ok := slashIndex[m.inputModeCommand]; ok && entry.isAgent {
			return entry.description
		}
	}
	text := m.input
	if !strings.HasPrefix(text, "/") || !strings.HasSuffix(text, " ") {
		return ""
	}
	name, args := slashNameAndArgs(text)
	if args != "" {
		return ""
	}
	entry, ok := slashIndex[name]
	if !ok || !entry.isAgent {
		return ""
	}
	return entry.description
}

func (m *model) inputModeTitle() string {
	if m.steering {
		return "steer"
	}
	if m.inputModeCommand != "" {
		if entry, ok := slashIndex[m.inputModeCommand]; ok && entry.isAgent {
			return entry.modeLabel()
		}
	}
	text := strings.TrimSpace(m.input)
	switch {
	case strings.HasPrefix(text, "!!"):
		return "shell local"
	case strings.HasPrefix(text, "!"):
		return "shell"
	case strings.HasPrefix(text, "/"):
		name, _ := slashNameAndArgs(text)
		if entry, ok := slashIndex[name]; ok {
			if entry.isAgent {
				return entry.modeLabel()
			}
			return "command"
		}
		return "command"
	default:
		return "chat"
	}
}

func (m *model) cycleInputMode() {
	if m.inResponse() || m.activePrompt != nil || m.bashRunning {
		return
	}
	m.inputSel.clear()
	modes := inputModes()
	if len(modes) == 0 {
		return
	}

	current := m.currentInputModeIndex(modes)
	next := (current + 1) % len(modes)
	m.inputModeCommand = modes[next].command
}

func (m *model) currentInputModeIndex(modes []inputMode) int {
	for i, mode := range modes {
		if mode.command == m.inputModeCommand {
			return i
		}
	}
	return 0
}

// inputBoxHeightFrac caps how much of the terminal height the input box may
// grow to before its body scrolls, instead of pushing the viewport off the
// top of the screen - mirrors help.go's helpOverlayHeightFrac, the same
// "clip to a fraction of the terminal" idea. Unlike the /help overlay, the
// input box has no user-driven scroll state: it's an active text buffer, not
// a static reference panel, so the visible window just follows the cursor
// (see clipInputBody) rather than tracking a scrollOffset field.
const inputBoxHeightFrac = 0.6

// inputBoxChromeLines is the input box's fixed border overhead that a
// height cap must leave room for: top border, hint row, blank padding row,
// bottom border - see renderInputBox. The body itself is whatever's left.
const inputBoxChromeLines = 4

// clipInputBody windows body (already word-wrapped input rows) down to
// whatever fits within inputBoxHeightFrac of the terminal, keeping
// cursorIdx (body's index of the row the cursor is on) inside the visible
// window - so a long pasted block or many wrapped lines scrolls instead of
// growing the input box past most of the screen (and shoving the viewport
// off the top). Returns body unchanged with an empty hint when it already
// fits, or m.height is unset (0, e.g. before the first WindowSizeMsg).
func (m *model) clipInputBody(body []string, cursorIdx int) (clipped []string, hint string) {
	if m.height <= 0 {
		return body, ""
	}
	maxBoxHeight := max(int(float64(m.height)*inputBoxHeightFrac), inputBoxChromeLines+1)
	maxBodyLines := maxBoxHeight - inputBoxChromeLines
	if maxBodyLines >= len(body) {
		return body, ""
	}
	maxBodyLines = max(maxBodyLines, 1)

	// Bias the window toward showing up to the cursor (classic "scroll
	// minimally to keep the cursor in view" behavior) rather than centering
	// it, since the cursor is usually at the end while actively typing.
	start := cursorIdx - maxBodyLines + 1
	start = max(start, 0)
	start = min(start, len(body)-maxBodyLines)
	end := start + maxBodyLines

	switch {
	case start > 0 && end < len(body):
		hint = inputPlaceholderStyle.Render("⋮ scrolled - more above and below")
	case start > 0:
		hint = inputPlaceholderStyle.Render("⋮ more above")
	case end < len(body):
		hint = inputPlaceholderStyle.Render("⋮ more below")
	}
	return body[start:end], hint
}

func renderInputBox(width int, title string, lines []string, hint string) string {
	if width <= 0 {
		width = 80
	}
	if width < 8 {
		return strings.Join(lines, "\n")
	}

	innerWidth := max(width-2, 1)
	top, bottom := titledBoxBorders(width, title)

	out := make([]string, 0, len(lines)+2)
	out = append(out, top)
	out = append(out, renderInputBoxLine(innerWidth, hint))
	for _, line := range lines {
		out = append(out, renderInputBoxLine(innerWidth, line))
	}
	out = append(out, renderInputBoxLine(innerWidth, ""))
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

// titledBoxBorders builds the top/bottom rules for a box-drawn box with its
// title embedded in the top rule (e.g. "╭ chat ─...─╮") - shared by
// renderInputBox and renderBoxAround (help.go) so every bordered box in the
// TUI draws its border the same way.
func titledBoxBorders(width int, title string) (top, bottom string) {
	label := " " + title + " "
	topRuleWidth := max(width-2-lipgloss.Width(label), 0)
	top = inputBoxStyle.Render("╭" + label + strings.Repeat("─", topRuleWidth) + "╮")
	bottom = inputBoxStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")
	return top, bottom
}

// renderInputBoxLine pads/truncates a (possibly styled) line to innerWidth
// and wraps it in the box's side borders, flush against the left edge - the
// input box's own prompt/cursor anchors there, so no left pad is added. For
// content with no such anchor, see padVisible (help.go), which pads/truncates
// the same way without the border wrapping.
func renderInputBoxLine(innerWidth int, line string) string {
	return inputBoxStyle.Render("│") + padVisible(line, innerWidth) + inputBoxStyle.Render("│")
}

// renderInputChunk renders ln[chunkStart:chunkEnd], applying the block
// cursor (if hasCursor and cursorCol falls within this chunk, or sits
// exactly at chunkEnd - a cursor past the chunk's last rune still
// highlights a trailing blank cell so it's never invisible) and/or a
// reverse-video selection highlight for [selLo,selHi) - both absolute rune
// indices into the full ln, matching curCol's own convention. The cursor's
// own highlight takes visual priority over selection for the one rune it
// lands on.
func renderInputChunk(ln []rune, chunkStart, chunkEnd int, hasCursor bool, cursorCol int, hasSel bool, selLo, selHi int) string {
	var b strings.Builder
	for p := chunkStart; p < chunkEnd; p++ {
		switch {
		case hasCursor && p == cursorCol:
			b.WriteString(inputCursorStyle.Render(string(ln[p])))
		case hasSel && p >= selLo && p < selHi:
			b.WriteString("\x1b[7m")
			b.WriteString(inputStyle.Render(string(ln[p])))
			b.WriteString("\x1b[27m")
		default:
			b.WriteString(inputStyle.Render(string(ln[p])))
		}
	}
	if hasCursor && cursorCol == chunkEnd && cursorCol >= chunkStart {
		b.WriteString(inputCursorStyle.Render(" "))
	}
	return b.String()
}

func (m *model) recallHistory(delta int) tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	if m.historyIdx == -1 {
		// Start navigating: stash current input as draft before overwriting.
		m.draftInput = m.input
		if delta < 0 {
			m.historyIdx = len(m.history) - 1
		} else {
			m.historyIdx = 0
		}
	} else {
		m.historyIdx += delta
		if m.historyIdx < 0 {
			m.historyIdx = 0
		}
		if m.historyIdx > len(m.history) {
			m.historyIdx = len(m.history)
		}
	}
	if m.historyIdx == len(m.history) {
		m.input = m.draftInput
	} else {
		m.input = m.history[m.historyIdx]
	}
	m.inputCursor = utf8.RuneCountInString(m.input)
	return nil
}

// seedHistoryFromMessages seeds the input history (Up/Down recall) from a
// loaded session's user messages - mirrors
// internal/tui/inline_chat.go's function of the same name. Leaves the
// current history untouched when the session had no user messages, rather
// than clearing it.
func (m *model) seedHistoryFromMessages(messages []tauchat.ChatMessage) {
	var prompts []string
	for _, msg := range messages {
		if msg.Role == tauchat.ChatRoleUser && strings.TrimSpace(msg.Content) != "" {
			prompts = append(prompts, msg.Content)
		}
	}
	if len(prompts) > 0 {
		m.history = prompts
		m.historyIdx = -1
	}
}

func (m *model) submitInput() tea.Cmd {
	// Interactive prompt active: handle prompt input.
	if m.activePrompt != nil {
		return m.resolvePrompt()
	}

	// A response in-flight does NOT make Enter steer - that's the dedicated
	// Ctrl+S hotkey's job (see handleSteer's ctrl+s binding). A plain Enter
	// falls through to the normal submit path below, whose startOrQueueTurn
	// call queues the message behind the running turn instead.
	if m.bashRunning {
		m.clearInput()
		return m.setNotification("still waiting for a response…")
	}

	text := strings.TrimSpace(m.input)
	modeCommand := m.inputModeCommand
	if text == "" {
		// Empty Enter is a no-op and keeps the selected agent mode active.
		return nil
	}
	if modeCommand != "" && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "!") {
		entry, ok := slashIndex[modeCommand]
		if !ok || !entry.isAgent || !entry.modeSwitch {
			// Preserve the draft and selected mode when validation fails; the
			// user can recover by cycling back to chat or another valid mode.
			return m.setNotification("agent mode unavailable: " + modeCommand)
		}
	}
	m.clearInput()
	m.historyIdx = -1  // reset history navigation
	m.compSelected = 0 // reset completion dropdown selection
	m.compToken = ""
	// Record in history - every submitted line is recallable via up-arrow,
	// slash commands and bash commands included, not just LLM prompts.
	m.history = append(m.history, text)

	// Slash commands.
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// Skill invocation: $skillname [args].
	if strings.HasPrefix(text, "$") {
		return m.handleSkillInvocation(text)
	}

	// Bash mode: !command (or !!command, excluded from what the model sees)
	// runs outside the LLM turn loop. handleBashCommand does its own
	// bang-stripping on the full text, not just a single "!".
	if strings.HasPrefix(text, "!") {
		return m.handleBashCommand(text)
	}

	// Agent modes are control state, not input scaffolding. Apply the mode
	// only at submission so the composer contains exactly what the user
	// typed and copied/history text never leaks a synthetic "/plan" prefix.
	if modeCommand != "" {
		return m.runAgentCommand(modeCommand, text)
	}

	// Debounce guard: 300ms between submits (P2 #27).
	if elapsed := time.Since(m.lastSubmit); elapsed < 300*time.Millisecond {
		return m.setNotification("slow down - submit debounced")
	}
	m.lastSubmit = time.Now()

	// Queue or start a turn.
	return m.startOrQueueTurn(text)
}

// startOrQueueTurn sends a prompt, queueing it behind a running turn.
func (m *model) startOrQueueTurn(text string) tea.Cmd {
	if m.inResponse() {
		m.turnQueue = append(m.turnQueue, text)
		return m.setNotification("queued - will send after current response")
	}

	// Record the user message locally for immediate display. Submitting a
	// prompt always means "show me what happens next" - resume following.
	m.autoFollow = true
	m.appendMessage("user", text)
	m.steering = false
	// Error and cancellation describe the prior turn, not the session. Clear
	// either terminal label as soon as the user starts the next turn instead
	// of waiting for the runtime's response-start event.
	m.agentState = agentThinking

	return sendCommand(m.runtime, tauchat.SubmitChatPromptCommand{
		SessionID:   m.sessionID,
		RequestID:   tauchat.NewRequestID(),
		Prompt:      text,
		SubmittedAt: time.Now().UTC(),
	})
}

// drainTurnQueue sends the next queued prompt, if any.
func (m *model) drainTurnQueue() tea.Cmd {
	if len(m.turnQueue) == 0 {
		return nil
	}
	next := m.turnQueue[0]
	m.turnQueue = m.turnQueue[1:]
	return m.startOrQueueTurn(next)
}
