package taui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestLineInputTypingAndValue(t *testing.T) {
	li := NewLineInput("› ")
	for _, k := range []string{"h", "e", "l", "l", "o"} {
		if !li.HandleInput(k) {
			t.Fatalf("HandleInput(%q) not consumed", k)
		}
	}
	if got := li.Value(); got != "hello" {
		t.Errorf("Value = %q, want %q", got, "hello")
	}
}

func TestLineInputBackspace(t *testing.T) {
	li := NewLineInput("")
	for _, k := range []string{"a", "b", "c"} {
		li.HandleInput(k)
	}
	li.HandleInput("\x7f") // backspace
	if got := li.Value(); got != "ab" {
		t.Errorf("after backspace Value = %q, want %q", got, "ab")
	}
}

func TestLineInputCursorMovementAndInsert(t *testing.T) {
	li := NewLineInput("")
	for _, k := range []string{"a", "c"} {
		li.HandleInput(k)
	}
	li.HandleInput("\x1b[D") // left → between a and c
	li.HandleInput("b")      // insert b at cursor
	if got := li.Value(); got != "abc" {
		t.Errorf("Value = %q, want %q", got, "abc")
	}
}

func TestLineInputHomeEndDelete(t *testing.T) {
	li := NewLineInput("")
	for _, k := range []string{"x", "y", "z"} {
		li.HandleInput(k)
	}
	li.HandleInput("\x01")    // Ctrl+A → home
	li.HandleInput("\x1b[3~") // delete → removes 'x'
	if got := li.Value(); got != "yz" {
		t.Errorf("Value = %q, want %q", got, "yz")
	}
	li.HandleInput("\x05") // Ctrl+E → end
	li.HandleInput("!")
	if got := li.Value(); got != "yz!" {
		t.Errorf("Value = %q, want %q", got, "yz!")
	}
}

func TestLineInputDeleteWord(t *testing.T) {
	li := NewLineInput("")
	for _, r := range "foo bar" {
		li.HandleInput(string(r))
	}
	li.HandleInput("\x17") // Ctrl+W → delete "bar"
	if got := li.Value(); got != "foo " {
		t.Errorf("Value = %q, want %q", got, "foo ")
	}
}

func TestLineInputSubmitClearsAndReports(t *testing.T) {
	li := NewLineInput("")
	var got string
	called := false
	li.SetOnSubmit(func(s string) { got = s; called = true })
	for _, r := range "ship it" {
		li.HandleInput(string(r))
	}
	li.HandleInput("\r")

	if !called {
		t.Fatal("onSubmit not called on Enter")
	}
	if got != "ship it" {
		t.Errorf("submitted %q, want %q", got, "ship it")
	}
	if li.Value() != "" {
		t.Errorf("input not cleared after submit: %q", li.Value())
	}
}

func TestLineInputPasteInserts(t *testing.T) {
	li := NewLineInput("")
	li.HandleInput("a")
	if !li.HandlePaste("X\nY") { // newline kept for multi-line paste
		t.Fatal("HandlePaste not consumed")
	}
	if got := li.Value(); got != "aX\nY" {
		t.Errorf("Value = %q, want %q", got, "aX\\nY")
	}
}

func TestLineInputIgnoresUnknownEscape(t *testing.T) {
	li := NewLineInput("")
	if li.HandleInput("\x1b[Z") { // shift-tab: not handled by the input
		t.Error("LineInput consumed an unhandled escape sequence")
	}
}

func TestLineInputNewlineInsertion(t *testing.T) {
	li := NewLineInput("")
	for _, r := range "a" {
		li.HandleInput(string(r))
	}
	// Ctrl+J inserts a newline.
	li.HandleInput("\x0a")
	for _, r := range "b" {
		li.HandleInput(string(r))
	}
	if got := li.Value(); got != "a\nb" {
		t.Errorf("Value = %q, want %q", got, "a\\nb")
	}
}

func TestLineInputShiftEnterInsertsNewline(t *testing.T) {
	// Both the Kitty CSI-u form and the xterm modifyOtherKeys form must insert
	// a newline (different terminals emit different sequences for Shift+Enter).
	for _, seq := range []string{"\x1b[13;2u", "\x1b[27;2;13~"} {
		li := NewLineInput("")
		li.HandleInput("x")
		li.HandleInput(seq)
		li.HandleInput("y")
		if got := li.Value(); got != "x\ny" {
			t.Errorf("Shift+Enter %q: Value = %q, want %q", seq, got, "x\\ny")
		}
	}
}

func TestLineInputEnterStillSubmits(t *testing.T) {
	li := NewLineInput("")
	var got string
	li.SetOnSubmit(func(s string) { got = s })
	for _, r := range "test" {
		li.HandleInput(string(r))
	}
	li.HandleInput("\r") // Enter submits
	if got != "test" {
		t.Errorf("submitted %q, want %q", got, "test")
	}
	if li.Value() != "" {
		t.Errorf("not cleared after submit: %q", li.Value())
	}
}

func TestLineInputMultiLineRender(t *testing.T) {
	li := NewLineInput("› ")
	li.focused = true
	for _, r := range "a\nb" {
		li.HandleInput(string(r))
	}
	lines := li.Render(40)
	if len(lines) < 2 {
		t.Fatalf("multi-line render produced %d lines, want >=2", len(lines))
	}
	if !strings.Contains(lines[0], "›") {
		t.Errorf("first line missing prompt: %q", lines[0])
	}
	if !strings.Contains(lines[0], "a") {
		t.Errorf("first line missing content: %q", lines[0])
	}
	if !strings.Contains(lines[1], "b") {
		t.Errorf("second line missing content: %q", lines[1])
	}
}

func TestLineInputWordJumpLeft(t *testing.T) {
	li := NewLineInput("")
	for _, r := range "hello world" {
		li.HandleInput(string(r))
	}
	// Cursor is at end (position 11). One Ctrl+Left should jump before "world".
	li.HandleInput("\x1b[1;5D")
	if li.cursor != 6 {
		t.Errorf("after Ctrl+Left: cursor = %d, want 6 (before 'world')", li.cursor)
	}
	// Another Ctrl+Left should jump to the start.
	li.HandleInput("\x1b[1;5D")
	if li.cursor != 0 {
		t.Errorf("after second Ctrl+Left: cursor = %d, want 0", li.cursor)
	}
}

func TestLineInputWordJumpRight(t *testing.T) {
	li := NewLineInput("")
	for _, r := range "foo bar baz" {
		li.HandleInput(string(r))
	}
	li.cursor = 0 // start at beginning
	li.HandleInput("\x1b[1;5C")
	if li.cursor != 4 {
		t.Errorf("after Ctrl+Right: cursor = %d, want 4 (after 'foo ')", li.cursor)
	}
	li.HandleInput("\x1b[1;5C")
	if li.cursor != 8 {
		t.Errorf("after second Ctrl+Right: cursor = %d, want 8 (after 'bar ')", li.cursor)
	}
}

func TestLineInputAltLeftRight(t *testing.T) {
	li := NewLineInput("")
	for _, r := range "a b" {
		li.HandleInput(string(r))
	}
	// Cursor at end (3). Alt+Left → back to 2 (before 'b').
	li.HandleInput("\x1b[1;3D")
	if li.cursor != 2 {
		t.Errorf("after Alt+Left: cursor = %d, want 2", li.cursor)
	}
	// Alt+Right → forward to end (3).
	li.HandleInput("\x1b[1;3C")
	if li.cursor != 3 {
		t.Errorf("after Alt+Right: cursor = %d, want 3", li.cursor)
	}
}

func TestLineInputCursorOnTrailingSpaces(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	li := NewLineInput("› ")
	for _, r := range "hello   " { // 3 trailing spaces, cursor at end
		li.HandleInput(string(r))
	}
	line := li.Render(40)[0]
	// Cursor sits on a trailing space — it must render a visible block, not
	// vanish (the bug was dropping trailing spaces from the rendered chunks).
	if !strings.Contains(line, "█") {
		t.Errorf("cursor block missing on trailing space: %q", line)
	}
	// The three spaces must be preserved before the cursor.
	if !strings.Contains(line, "hello   ") && !strings.Contains(line, "hello  \x1b") {
		t.Errorf("trailing spaces not preserved: %q", line)
	}
}

func TestLineInputCursorOnEmptyNewline(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	li := NewLineInput("› ")
	for _, r := range "line1" {
		li.HandleInput(string(r))
	}
	li.HandleInput("\x0a") // Ctrl+J → newline; cursor now on empty 2nd line
	lines := li.Render(40)
	if len(lines) < 2 {
		t.Fatalf("expected >=2 display lines, got %d", len(lines))
	}
	// The cursor is on the empty second line — it must render a block there.
	if !strings.Contains(lines[1], "█") {
		t.Errorf("cursor block missing on empty newline: %q", lines[1])
	}
}

func TestLineInputCursorColorDefault(t *testing.T) {
	li := NewLineInput("› ")
	// Default: (0,0,0) → mid-grey fallback in cursorOn().
	if got := li.cursorOn(false); got != "\x1b[48;2;128;134;150m" {
		t.Errorf("default cursorOn = %q, want mid-grey", got)
	}
}

func TestLineInputCursorColorCustom(t *testing.T) {
	li := NewLineInput("")
	li.SetCursorColor(255, 158, 0) // amber
	if got := li.cursorOn(false); got != "\x1b[48;2;255;158;0m" {
		t.Errorf("custom cursorOn = %q, want amber", got)
	}
}

func TestLineInputRenderShowsPromptAndText(t *testing.T) {
	li := NewLineInput("› ")
	for _, r := range "hi" {
		li.HandleInput(string(r))
	}
	line := li.Render(40)[0]
	if !strings.Contains(line, "›") || !strings.Contains(line, "hi") {
		t.Errorf("render %q missing prompt or text", line)
	}
}
