package components

import (
	"fmt"
	"github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// Toggle is a boolean switch with a label. Enter or Space toggles the
// checked state and calls OnChange.
type Toggle struct {
	Checked  *tui.State[bool]
	Label    string
	OnChange func(bool)
	focused  *tui.State[bool]
}

// NewToggle creates a Toggle component.
func NewToggle(checked *tui.State[bool], label string, onChange func(bool)) *Toggle {
	return &Toggle{
		Checked:  checked,
		Label:    label,
		OnChange: onChange,
		focused:  tui.NewState(false),
	}
}

// BindApp wires state to the app.
func (t *Toggle) BindApp(app *tui.App) {
	t.Checked.BindApp(app)
	t.focused.BindApp(app)
}

// KeyMap defines keyboard bindings for the Toggle.
func (t *Toggle) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEnter, func(tui.KeyEvent) { t.doFlip() }),
		tui.OnFocused(tui.Rune(' '), func(tui.KeyEvent) { t.doFlip() }),
	}
}

func (t *Toggle) doFlip() {
	next := !t.Checked.Get()
	t.Checked.Set(next)
	if t.OnChange != nil {
		t.OnChange(next)
	}
}

// Focus marks the toggle as focused.
func (t *Toggle) Focus() {
	t.focused.Set(true)
}

// Blur marks the toggle as blurred.
func (t *Toggle) Blur() {
	t.focused.Set(false)
}

// IsFocusable reports true.
func (t *Toggle) IsFocusable() bool {
	return true
}

// IsTabStop reports true.
func (t *Toggle) IsTabStop() bool {
	return true
}

// IsFocused reports whether the toggle is currently focused.
func (t *Toggle) IsFocused() bool {
	return t.focused.Get()
}

// HandleEvent is required by Focusable.
func (t *Toggle) HandleEvent(tui.Event) bool {
	return false
}

// Debug registration.
func init() {
	Register("toggle", func(app *tui.App) (tui.Component, []DebugControl) {
		checked := tui.NewStateForApp(app, true)
		comp := NewToggle(checked, "Show reasoning", nil)
		return comp, []DebugControl{
			{Label: "checked", Type: "toggle", State: boolState(checked)},
		}
	})
}

func boolState(s *tui.State[bool]) *tui.State[string] {
	state := tui.NewState(boolToStr(s.Get()))
	s.Bind(func(v bool) { state.Set(boolToStr(v)) })
	return state
}

func boolToStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func strToBool(v string) bool {
	return v == "true"
}

// Render builds the toggle element tree.
templ (t *Toggle) Render() {
	<div class="flex-row items-center gap-1 px-1" onFocus={func(*tui.Element) { t.Focus() }} onBlur={func(*tui.Element) { t.Blur() }} autoFocus={true}>
		if t.focused.Get() {
			<span textStyle={theme.SelectedStyle()}>{fmt.Sprintf("[%s]", checkMark(t.Checked.Get()))}</span>
		} else {
			<span>{fmt.Sprintf("[%s]", checkMark(t.Checked.Get()))}</span>
		}
		<span>
			{t.Label}
		</span>
	</div>
}

func checkMark(checked bool) string {
	if checked {
		return "X"
	}
	return " "
}
