package taui

import "github.com/samcharles93/tau/pkg/taui/termkit"

// Text is the simplest component — it renders a single string with optional
// foreground and background colour callbacks. Ported from Pi's components/text.ts.
type Text struct {
	text string
	fgFn func(string) string
	bgFn func(string) string
}

// NewText creates a Text component.
func NewText(text string) *Text {
	return &Text{text: text}
}

// NewStyledText creates a Text component with fg/bg colour callbacks.
func NewStyledText(text string, fgFn, bgFn func(string) string) *Text {
	return &Text{text: text, fgFn: fgFn, bgFn: bgFn}
}

// SetText updates the text content.
func (t *Text) SetText(text string) {
	t.text = text
}

// Text returns the current text.
func (t *Text) Text() string { return t.text }

// SetFgFn sets the foreground colour callback.
func (t *Text) SetFgFn(fn func(string) string) { t.fgFn = fn }

// SetBgFn sets the background colour callback.
func (t *Text) SetBgFn(fn func(string) string) { t.bgFn = fn }

// Render returns the styled text. Uses Wrap (no trailing Reset) so
// parent components like Box can safely apply background colours.
func (t *Text) Render(width int) []string {
	_ = width
	s := t.text
	if t.fgFn != nil && termkit.ColorEnabled() {
		s = t.fgFn(s)
	}
	if t.bgFn != nil && termkit.ColorEnabled() {
		s = t.bgFn(s)
	}
	return []string{s}
}

// HandleInput is a no-op for Text.
func (t *Text) HandleInput(data string) bool { return false }

// Invalidate is a no-op for Text.
func (t *Text) Invalidate() {}
