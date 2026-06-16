package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// Select is a horizontal option cycler. Left/Right arrows cycle through
// options. Enter confirms and blurs. The current value is bound to Value.
type Select struct {
	Value    *tui.State[string]
	Options  []SelectOption
	OnChange func(string)
	focused  *tui.State[bool]
}

// NewSelect creates a Select component.
func NewSelect(value *tui.State[string], options []SelectOption, onChange func(string)) *Select {
	return &Select{
		Value:    value,
		Options:  options,
		OnChange: onChange,
		focused:  tui.NewState(false),
	}
}

// currentLabel returns the label for the current value, or the value itself.
func (s *Select) currentLabel() string {
	v := s.Value.Get()
	for _, o := range s.Options {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

// currentIndex returns the index of the current value in options, or -1.
func (s *Select) currentIndex() int {
	v := s.Value.Get()
	for i, o := range s.Options {
		if o.Value == v {
			return i
		}
	}
	return -1
}

// BindApp wires state to the app.
func (s *Select) BindApp(app *tui.App) {
	s.Value.BindApp(app)
	s.focused.BindApp(app)
}

// KeyMap defines keyboard bindings for the Select.
func (s *Select) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyLeft, func(tui.KeyEvent) {
			idx := s.currentIndex()
			if idx <= 0 {
				return
			}
			next := s.Options[idx-1]
			s.Value.Set(next.Value)
			if s.OnChange != nil {
				s.OnChange(next.Value)
			}
		}),
		tui.OnFocused(tui.KeyRight, func(tui.KeyEvent) {
			idx := s.currentIndex()
			if idx < 0 || idx >= len(s.Options)-1 {
				return
			}
			next := s.Options[idx+1]
			s.Value.Set(next.Value)
			if s.OnChange != nil {
				s.OnChange(next.Value)
			}
		}),
		tui.OnFocused(tui.KeyEnter, func(tui.KeyEvent) {
			// Retain focus on confirm
		}),
	}
}

// Focus marks the select as focused.
func (s *Select) Focus() {
	s.focused.Set(true)
}

// Blur marks the select as blurred.
func (s *Select) Blur() {
	s.focused.Set(false)
}

// IsFocusable reports true so Tab navigation includes this component.
func (s *Select) IsFocusable() bool {
	return true
}

// IsTabStop reports true so Tab navigation includes this component.
func (s *Select) IsTabStop() bool {
	return true
}

// IsFocused reports whether the select is currently focused.
func (s *Select) IsFocused() bool {
	return s.focused.Get()
}

// HandleEvent is required by the Focusable interface; key events are
// handled by KeyMap and dispatched by the framework.
func (s *Select) HandleEvent(tui.Event) bool {
	return false
}

// Debug registration: /debug components select
func init() {
	Register("select", func(app *tui.App) (tui.Component, []DebugControl) {
		value := tui.NewStateForApp(app, "model-b")
		comp := NewSelect(value, []SelectOption{
			{Value: "model-a", Label: "GPT-4o"},
			{Value: "model-b", Label: "Claude Sonnet 4"},
			{Value: "model-c", Label: "Gemini 2.5 Pro"},
		}, nil)
		return comp, []DebugControl{
			{
				Label:   "value",
				Type:    "cycle",
				Options: []string{"model-a", "model-b", "model-c"},
				State:   value,
			},
		}
	})
}

// Render builds the select element tree.
templ (s *Select) Render() {
	<div class="flex-row items-center gap-1 px-1 border-single" onFocus={func(*tui.Element) { s.Focus() }} onBlur={func(*tui.Element) { s.Blur() }} autoFocus={true}>
		<span textStyle={theme.DimStyle().Bold()}>{"‹"}</span>
		if s.focused.Get() {
			<span textStyle={theme.SelectedStyle()}>{s.currentLabel()}</span>
		} else {
			<span textStyle={theme.BodyStyle()}>{s.currentLabel()}</span>
		}
		<span textStyle={theme.DimStyle().Bold()}>{"›"}</span>
	</div>
}
