package taui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// LineInput is a multi-line text input with a visible block cursor and the
// common readline-style editing keys. Shift+Enter or Ctrl+J inserts a newline;
// Enter submits. It is Focusable and implements PasteHandler. Slimmed port of
// Pi's editor-component.ts for inline prompts.
type LineInput struct {
	mu       sync.Mutex
	runes    []rune
	cursor   int // index into runes, 0..len(runes)
	prompt   string
	hint     string // placeholder shown when empty
	focused  bool
	onSubmit func(string)

	promptFn func(string) string
	textFn   func(string) string
	hintFn   func(string) string
	cmdFn    func(string) string // applied instead of textFn while the line begins with "/"

	cursorR, cursorG, cursorB uint8          // cursor background colour (0 = default grey)
	cmdCursor                 *termkit.Color // cursor background used instead while isCommand (see SetCommandCursorColor)

	// History buffer for readline-style recall of submitted prompts.
	// historyPos is -1 when live-editing; 0..len(history)-1 when browsing.
	history     []string
	historyPos  int
	historySave string
}

// NewLineInput creates an input with the given prompt prefix (e.g. "› ").
// The input starts focused so the block cursor is visible immediately.
func NewLineInput(prompt string) *LineInput {
	return &LineInput{prompt: prompt, focused: true, historyPos: -1}
}

// SetOnSubmit registers the callback fired (with the full multi-line text) when
// the user presses Enter. The input is cleared before the callback runs.
func (li *LineInput) SetOnSubmit(fn func(string)) { li.onSubmit = fn }

// SetPlaceholder sets hint text shown when the input is empty.
func (li *LineInput) SetPlaceholder(s string) { li.hint = s }

// SetCursorColor sets the background colour for the block cursor. Values of
// (0,0,0) use the default mid-grey.
func (li *LineInput) SetCursorColor(r, g, b uint8) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.cursorR, li.cursorG, li.cursorB = r, g, b
}

// SetStyles sets colour callbacks for the prompt, typed text, and placeholder.
func (li *LineInput) SetStyles(promptFn, textFn, hintFn func(string) string) {
	li.promptFn, li.textFn, li.hintFn = promptFn, textFn, hintFn
}

// SetCommandStyle sets a colour callback applied to the whole line, in place
// of textFn, whenever the current input begins with "/" — so a slash command
// reads visually distinct from a plain prompt while it's being typed, not
// just once submitted. Pass nil to disable.
func (li *LineInput) SetCommandStyle(fn func(string) string) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.cmdFn = fn
}

// SetCommandCursorColor sets the block-cursor background colour used while
// the line begins with "/" (see SetCommandStyle), so the caret matches the
// same accent as the coloured command text.
func (li *LineInput) SetCommandCursorColor(fg termkit.Color) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.cmdCursor = &fg
}

// Focused reports whether the input has focus.
func (li *LineInput) Focused() bool {
	li.mu.Lock()
	defer li.mu.Unlock()
	return li.focused
}

// SetFocused sets the focus state.
func (li *LineInput) SetFocused(f bool) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.focused = f
}

// Value returns the current text (may contain newlines).
func (li *LineInput) Value() string {
	li.mu.Lock()
	defer li.mu.Unlock()
	return string(li.runes)
}

// Clear empties the input.
func (li *LineInput) Clear() {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.runes = li.runes[:0]
	li.cursor = 0
}

// AddToHistory appends s to the history buffer, skipping adjacent duplicates.
// The history position is reset to live-editing mode.
func (li *LineInput) AddToHistory(s string) {
	li.mu.Lock()
	defer li.mu.Unlock()
	if len(li.history) > 0 && li.history[len(li.history)-1] == s {
		return
	}
	li.history = append(li.history, s)
	li.historyPos = -1
}

// SetHistory replaces the history buffer with entries. Used when loading a
// session to seed the buffer from persisted user messages.
func (li *LineInput) SetHistory(entries []string) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.history = append([]string(nil), entries...)
	li.historyPos = -1
}

// navigateHistory moves through the history buffer. dir < 0 goes older (up),
// dir > 0 goes newer (down). On first entry into history mode (from live
// editing), the current draft is saved so it can be restored when moving past
// the newest entry.
//
// Caller must hold li.mu.
func (li *LineInput) navigateHistory(dir int) {
	if len(li.history) == 0 {
		return
	}
	if li.historyPos == -1 {
		if dir > 0 {
			return // nothing newer than live editing
		}
		// Entering history mode: save the current draft.
		li.historySave = string(li.runes)
		li.historyPos = len(li.history) - 1
	} else {
		li.historyPos += dir
		// Clamp and restore draft when moving past the newest entry.
		if li.historyPos < 0 {
			li.historyPos = -1
			li.runes = []rune(li.historySave)
			li.cursor = len(li.runes)
			li.historySave = ""
			return
		}
		if li.historyPos >= len(li.history) {
			li.historyPos = len(li.history) - 1
			return
		}
	}
	entry := li.history[li.historyPos]
	li.runes = []rune(entry)
	li.cursor = len(li.runes)
}

// Invalidate is a no-op.
func (li *LineInput) Invalidate() {}

// HandleInput applies one key sequence. Returns true if it was consumed.
//
// The submit callback runs after the lock is released so it can safely call
// TUI methods without deadlocking. The submitted value is captured to a local
// before clearing state and unlocking — the render goroutine sees the cleared
// input for one frame, which is correct (the prompt was just submitted).
func (li *LineInput) HandleInput(data string) bool {
	li.mu.Lock()

	var submit string
	didSubmit := false

	switch data {
	case "\r":
		// Enter — submit.
		submit = string(li.runes)
		li.runes = li.runes[:0]
		li.cursor = 0
		li.historyPos = -1
		li.historySave = ""
		didSubmit = true

	case "\x0a",
		"\x1b[13;2u",    // Kitty CSI-u: Shift+Enter (codepoint 13, modifier 2)
		"\x1b[27;2;13~", // xterm modifyOtherKeys: CSI 27 ; modifier ; keycode ~
		"\x1b[27;13;2~": // (defensive: some terminals swap the two fields)
		// Ctrl+J / Shift+Enter — insert a newline.
		li.insertLocked([]rune{'\n'})

	case "\x7f": // Backspace (DEL) — single character
		if li.cursor > 0 {
			li.runes = append(li.runes[:li.cursor-1], li.runes[li.cursor:]...)
			li.cursor--
		}
	case "\x1b[3~": // Delete
		if li.cursor < len(li.runes) {
			li.runes = append(li.runes[:li.cursor], li.runes[li.cursor+1:]...)
		}

	case "\x1b[D", "\x1bOD": // Left
		if li.cursor > 0 {
			li.cursor--
		}
	case "\x1b[C", "\x1bOC": // Right
		if li.cursor < len(li.runes) {
			li.cursor++
		}

	case "\x1b[1;5D", "\x1b[1;3D": // Ctrl+Left / Alt+Left — jump word left
		li.cursor = li.wordLeftLocked()
	case "\x1b[1;5C", "\x1b[1;3C": // Ctrl+Right / Alt+Right — jump word right
		li.cursor = li.wordRightLocked()

	case "\x1b[A", "\x1bOA": // Up — history or previous logical line
		if li.atFirstLineStart() {
			li.navigateHistory(-1)
		} else {
			li.moveVertLocked(-1)
		}
	case "\x1b[B", "\x1bOB": // Down — history or next logical line
		if li.atLastLineEnd() {
			li.navigateHistory(1)
		} else {
			li.moveVertLocked(1)
		}

	case "\x01", "\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~": // Home / Ctrl+A
		li.cursor = 0
	case "\x05", "\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~": // End / Ctrl+E
		li.cursor = len(li.runes)

	case "\x15": // Ctrl+U: delete to start of line
		li.runes = append([]rune(nil), li.runes[li.cursor:]...)
		li.cursor = 0
	case "\x0b": // Ctrl+K: delete to end of line
		li.runes = li.runes[:li.cursor]
	case "\x08", "\x17", "\x1b[127;5u": // Ctrl+W / Ctrl+Backspace: delete previous word
		li.deleteWordLocked()

	default:
		if !insertable(data) {
			li.mu.Unlock()
			return false
		}
		li.insertLocked([]rune(data))
	}

	onSubmit := li.onSubmit
	li.mu.Unlock()

	if didSubmit && onSubmit != nil {
		onSubmit(submit)
	}
	return true
}

// atFirstLineStart reports whether the cursor is at the very start of the
// first logical line. Caller holds li.mu.
func (li *LineInput) atFirstLineStart() bool {
	return li.cursor == 0
}

// atLastLineEnd reports whether the cursor is at the very end of the last
// logical line. Caller holds li.mu.
func (li *LineInput) atLastLineEnd() bool {
	return li.cursor >= len(li.runes)
}

// moveVertLocked moves the cursor up (-1) or down (+1) one logical line within
// the text (delimited by newlines). Caller holds li.mu.
func (li *LineInput) moveVertLocked(dir int) {
	if li.cursor == 0 && dir < 0 {
		return
	}
	if li.cursor >= len(li.runes) && dir > 0 {
		return
	}
	lines := li.splitLines()
	curLine, curCol := li.cursorInLines(lines)
	target := curLine + dir
	if target < 0 || target >= len(lines) {
		return
	}
	col := min(curCol, len(lines[target]))
	li.cursor = li.linePos(lines, target, col)
}

// splitLines returns the logical lines (by newline) of the current text.
func (li *LineInput) splitLines() [][]rune {
	var out [][]rune
	start := 0
	for i, r := range li.runes {
		if r == '\n' {
			out = append(out, li.runes[start:i])
			start = i + 1
		}
	}
	out = append(out, li.runes[start:])
	return out
}

// cursorInLines returns (line index, column index) for the current cursor.
func (li *LineInput) cursorInLines(lines [][]rune) (int, int) {
	pos := 0
	for idx, ln := range lines {
		end := pos + len(ln)
		if li.cursor <= end {
			return idx, li.cursor - pos
		}
		pos = end + 1 // +1 for the newline
	}
	return len(lines) - 1, len(lines[len(lines)-1])
}

// linePos returns the absolute cursor index for (line, col) within lines.
func (li *LineInput) linePos(lines [][]rune, targetLine, col int) int {
	pos := 0
	for i := range targetLine {
		pos += len(lines[i]) + 1
	}
	return pos + min(col, len(lines[targetLine]))
}

// HandlePaste inserts pasted content at the cursor.
func (li *LineInput) HandlePaste(content string) bool {
	cleaned := make([]rune, 0, len(content))
	for _, r := range content {
		if r == '\r' {
			continue
		}
		if r == '\t' {
			r = ' '
		}
		if r < 0x20 && r != '\n' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	if len(cleaned) == 0 {
		return false
	}
	li.mu.Lock()
	li.insertLocked(cleaned)
	li.mu.Unlock()
	return true
}

// ReplaceRange replaces the runes in [start, end) with text and places the
// cursor just after the inserted text. Indices are rune offsets into the full
// value; they are clamped to valid bounds. Used by Completions to swap the
// token under the cursor for a chosen completion.
func (li *LineInput) ReplaceRange(start, end int, text string) {
	li.mu.Lock()
	defer li.mu.Unlock()

	start = max(0, min(start, len(li.runes)))
	end = max(start, min(end, len(li.runes)))
	ins := []rune(text)

	nr := make([]rune, 0, len(li.runes)-(end-start)+len(ins))
	nr = append(nr, li.runes[:start]...)
	nr = append(nr, ins...)
	nr = append(nr, li.runes[end:]...)
	li.runes = nr
	li.cursor = start + len(ins)
}

// Cursor returns the current cursor position as a rune offset.
func (li *LineInput) Cursor() int {
	li.mu.Lock()
	defer li.mu.Unlock()
	return li.cursor
}

// SetValueAndCursor replaces the content and sets the cursor. Used by
// Completions to extend the query via LCP without a full replace-select cycle.
func (li *LineInput) SetValueAndCursor(text string, cursor int) {
	li.mu.Lock()
	defer li.mu.Unlock()
	li.runes = []rune(text)
	li.cursor = max(0, min(cursor, len(li.runes)))
}

func (li *LineInput) insertLocked(rs []rune) {
	nr := make([]rune, 0, len(li.runes)+len(rs))
	nr = append(nr, li.runes[:li.cursor]...)
	nr = append(nr, rs...)
	nr = append(nr, li.runes[li.cursor:]...)
	li.runes = nr
	li.cursor += len(rs)
}

func (li *LineInput) deleteWordLocked() {
	i := li.cursor
	for i > 0 && li.runes[i-1] == ' ' {
		i--
	}
	for i > 0 && li.runes[i-1] != ' ' && li.runes[i-1] != '\n' {
		i--
	}
	li.runes = append(li.runes[:i], li.runes[li.cursor:]...)
	li.cursor = i
}

func (li *LineInput) wordLeftLocked() int {
	i := li.cursor
	for i > 0 && li.runes[i-1] == ' ' {
		i--
	}
	for i > 0 && li.runes[i-1] != ' ' && li.runes[i-1] != '\n' {
		i--
	}
	return i
}

func (li *LineInput) wordRightLocked() int {
	i := li.cursor
	for i < len(li.runes) && li.runes[i] != ' ' && li.runes[i] != '\n' {
		i++
	}
	for i < len(li.runes) && li.runes[i] == ' ' {
		i++
	}
	return i
}

// ── Render ───────────────────────────────────────────────────────────────────

const cursorOff = "\x1b[49m"

func (li *LineInput) cursorOn(isCommand bool) string {
	if isCommand && li.cmdCursor != nil {
		return li.cmdCursor.Bg()
	}
	r, g, b := li.cursorR, li.cursorG, li.cursorB
	if r == 0 && g == 0 && b == 0 {
		return "\x1b[48;2;128;134;150m" // default mid-grey
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// Render draws the prompt, the multi-line text, and a block cursor at the
// caret position. Wrapping and cursor injection happen in a single pass per
// logical line — no separate wrapping paths that can diverge.
func (li *LineInput) Render(width int) []string {
	li.mu.Lock()
	defer li.mu.Unlock()

	colour := termkit.ColorEnabled()
	prompt := li.prompt
	if li.promptFn != nil && colour {
		prompt = li.promptFn(li.prompt)
	}
	promptW := VisibleWidth(li.prompt)

	if len(li.runes) == 0 && li.hint != "" && !li.focused {
		hint := li.hint
		if li.hintFn != nil && colour {
			hint = li.hintFn(li.hint)
		}
		return []string{prompt + hint}
	}

	// A line beginning with "/" (slash command) or "!" (bash mode) is a
	// command in progress — colour the whole thing with cmdFn instead of
	// textFn, mirroring the accent used to echo it into scrollback once
	// submitted. cmdFn itself decides the exact colour per trigger/mode; this
	// gate only decides whether cmdFn applies at all, and also drives the
	// command cursor colour below, so a plain prompt with no trigger doesn't
	// get command styling just because cmdFn happens to be set.
	trimmed := strings.TrimSpace(string(li.runes))
	isCommand := li.cmdFn != nil && (strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "!"))
	paint := func(s string) string {
		if !colour {
			return s
		}
		if isCommand {
			return li.cmdFn(s)
		}
		if li.textFn != nil {
			return li.textFn(s)
		}
		return s
	}

	logicalLines := li.splitLines()
	curLine, curCol := li.cursorInLines(logicalLines)
	contentWidth := width - promptW
	if contentWidth < 1 {
		contentWidth = width
	}

	var out []string
	for i, ln := range logicalLines {
		prefix := prompt
		if i > 0 {
			prefix = strings.Repeat(" ", promptW)
		}
		out = append(out, li.renderOneLine(ln, i == curLine, curCol, prefix, contentWidth, paint, isCommand)...)
	}
	return out
}

// renderChunk is one segment of a wrapped display line with source rune offsets
// so cursor injection can target the exact character position.
type renderChunk struct {
	text        string
	runesBefore int
	runesAfter  int
}

// renderOneLine wraps a single logical line to maxW columns and injects the
// block cursor at cursorCol (when hasCursor is true). Wrapping and cursor
// tracking use the same word-scanning loop — no duplicated logic.
func (li *LineInput) renderOneLine(ln []rune, hasCursor bool, cursorCol int, prefix string, maxW int, paint func(string) string, isCommand bool) []string {
	if len(ln) == 0 {
		// Empty logical line (e.g. just after a newline). Still show the cursor
		// here when it falls on this line — otherwise it vanishes on blank lines.
		if hasCursor {
			return []string{prefix + li.cursorAtEnd("", paint, isCommand)}
		}
		return []string{prefix}
	}
	if maxW < 1 {
		maxW = 1
	}

	// Scan words + spaces, building wrapped display lines.
	var lines [][]renderChunk
	var cur []renderChunk
	vis := 0
	rp := 0
	rem := string(ln)

	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, cur)
			cur = nil
			vis = 0
		}
	}

	for len(rem) > 0 {
		// Count actual source spaces before this word.
		srcSp := 0
		for srcSp < len(rem) && rem[srcSp] == ' ' {
			srcSp++
		}
		end := srcSp
		for end < len(rem) && rem[end] != ' ' {
			end++
		}
		if end == srcSp {
			// Trailing spaces with no following word. Keep them as a chunk so
			// the cursor can rest on them and stay visible (the scan would
			// otherwise drop them entirely).
			cur = append(cur, renderChunk{
				text:        rem,
				runesBefore: rp,
				runesAfter:  rp + len(rem),
			})
			break
		}
		word := rem[srcSp:end]
		ww := VisibleWidth(word)

		// Display spaces: normally srcSp, but stripped when wrapping.
		dispSp := srcSp
		if len(cur) > 0 && vis+dispSp+ww > maxW {
			flush()
			dispSp = 0
		}

		segText := word
		if dispSp > 0 {
			segText = strings.Repeat(" ", dispSp) + word
		}

		cur = append(cur, renderChunk{
			text:        segText,
			runesBefore: rp,
			runesAfter:  rp + srcSp + len(word),
		})
		vis += VisibleWidth(segText)
		rp += srcSp + len(word)
		rem = rem[srcSp+len(word):]
	}
	flush()

	if len(lines) == 0 {
		return []string{prefix}
	}

	var out []string
	for _, chunks := range lines {
		if !hasCursor {
			out = append(out, prefix+li.joinRenderChunks(chunks, paint))
			continue
		}
		first := chunks[0].runesBefore
		last := chunks[len(chunks)-1].runesAfter
		if cursorCol < first || cursorCol > last {
			out = append(out, prefix+li.joinRenderChunks(chunks, paint))
			continue
		}
		// Cursor is on this display line — inject it into the right chunk.
		var sb strings.Builder
		sb.WriteString(prefix)
		placed := false
		for _, ck := range chunks {
			if placed || cursorCol < ck.runesBefore || cursorCol > ck.runesAfter {
				sb.WriteString(paint(ck.text))
				continue
			}
			localCol := cursorCol - ck.runesBefore
			flat := []rune(ck.text)
			if localCol > len(flat) {
				localCol = len(flat)
			}
			sb.WriteString(paint(string(flat[:localCol])))
			sb.WriteString(li.cursorAtEnd(string(flat[localCol:]), paint, isCommand))
			placed = true
		}
		if !placed {
			sb.WriteString(li.cursorAtEnd("", paint, isCommand))
		}
		out = append(out, sb.String())
	}
	return out
}

func (li *LineInput) joinRenderChunks(chunks []renderChunk, paint func(string) string) string {
	var sb strings.Builder
	for _, ck := range chunks {
		sb.WriteString(paint(ck.text))
	}
	return sb.String()
}

// cursorAtEnd applies cursor styling to tail text. The character under the
// cursor gets a coloured background. Spaces are replaced with a visible block
// (█) so the cursor is never invisible between words. The remainder after
// the cursor character is run through paint so it keeps whatever text
// styling (e.g. command colouring) applies to the rest of the line.
func (li *LineInput) cursorAtEnd(tail string, paint func(string) string, isCommand bool) string {
	if !li.focused || !termkit.ColorEnabled() {
		return tail
	}
	if tail == "" {
		return li.cursorOn(isCommand) + "█" + cursorOff
	}
	runes := []rune(tail)
	ch := string(runes[0])
	if ch == " " {
		ch = "█"
	}
	rest := string(runes[1:])
	if paint != nil {
		rest = paint(rest)
	}
	return li.cursorOn(isCommand) + ch + cursorOff + rest
}

// insertable reports whether a key sequence is printable text.
func insertable(s string) bool {
	if s == "" || s[0] == escByte {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
