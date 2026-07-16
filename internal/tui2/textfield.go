package tui2

import "unicode/utf8"

// textField is a standalone, single-line text-input widget. It owns its own
// rune buffer and cursor position rather than sharing model.input, so it can
// be reused anywhere a small piece of UI needs to collect free text from the
// user - both the agent-question flow (formPrompt, see prompt.go) and local
// flows like the GitHub Copilot Enterprise-domain prompt hold their own
// *textField instance.
type textField struct {
	value       string
	cursor      int // rune index into value, 0..len(runes)
	placeholder string
}

func newTextField(placeholder string) *textField {
	return &textField{placeholder: placeholder}
}

func (f *textField) Value() string { return f.value }

func (f *textField) SetValue(s string) {
	f.value = s
	f.cursor = utf8.RuneCountInString(s)
}

func (f *textField) Insert(s string) {
	if s == "" {
		return
	}
	rs := []rune(f.value)
	ins := []rune(s)
	nr := make([]rune, 0, len(rs)+len(ins))
	nr = append(nr, rs[:f.cursor]...)
	nr = append(nr, ins...)
	nr = append(nr, rs[f.cursor:]...)
	f.value = string(nr)
	f.cursor += len(ins)
}

func (f *textField) Backspace() {
	if f.cursor <= 0 {
		return
	}
	rs := []rune(f.value)
	rs = append(rs[:f.cursor-1], rs[f.cursor:]...)
	f.value = string(rs)
	f.cursor--
}

func (f *textField) Delete() {
	rs := []rune(f.value)
	if f.cursor >= len(rs) {
		return
	}
	rs = append(rs[:f.cursor], rs[f.cursor+1:]...)
	f.value = string(rs)
}

func (f *textField) MoveCursor(delta int) {
	f.cursor = max(min(f.cursor+delta, utf8.RuneCountInString(f.value)), 0)
}

func (f *textField) Home() { f.cursor = 0 }

func (f *textField) End() { f.cursor = utf8.RuneCountInString(f.value) }

// Render draws the field as a single line, horizontally scrolling so the
// cursor stays in view when the value is wider than width. It reuses
// renderInputChunk/inputCursorStyle/inputPlaceholderStyle - the same
// primitives the main chat composer uses - so a prompt's text field looks
// identical to normal input text.
func (f *textField) Render(width int) string {
	width = max(width, 1)
	if f.value == "" {
		if f.placeholder == "" {
			return inputCursorStyle.Render(" ")
		}
		// Reserve one column for the leading cursor block, and truncate on a
		// rune boundary with an ellipsis rather than a hard mid-word cut.
		avail := max(width-1, 1)
		ph := []rune(f.placeholder)
		if len(ph) > avail {
			if avail > 1 {
				ph = append(ph[:avail-1], '…')
			} else {
				ph = ph[:avail]
			}
		}
		return inputCursorStyle.Render(" ") + inputPlaceholderStyle.Render(string(ph))
	}

	rs := []rune(f.value)
	start := 0
	if f.cursor > width-1 {
		start = f.cursor - (width - 1)
	}
	end := min(start+width, len(rs))
	if end-start < width && start > 0 {
		start = max(end-width, 0)
	}
	hasCursor := f.cursor >= start && f.cursor <= end
	return renderInputChunk(rs, start, end, hasCursor, f.cursor, false, 0, 0)
}
