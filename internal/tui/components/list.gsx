package components

import (
	"fmt"
	"github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// List is a scrollable, selectable item list. Up/Down arrows move the
// selection. Enter calls OnSelect. Disabled items are skipped.
type List struct {
	Items    *tui.State[[]ListItem]
	Selected *tui.State[int]
	OnSelect func(ListItem)
	focused  *tui.State[bool]
}

// NewList creates a List component.
func NewList(items *tui.State[[]ListItem], selected *tui.State[int], onSelect func(ListItem)) *List {
	return &List{
		Items:    items,
		Selected: selected,
		OnSelect: onSelect,
		focused:  tui.NewState(false),
	}
}

// BindApp wires state to the app.
func (l *List) BindApp(app *tui.App) {
	l.Items.BindApp(app)
	l.Selected.BindApp(app)
	l.focused.BindApp(app)
}

// moveUp decrements selection, skipping disabled items.
func (l *List) moveUp() {
	items := l.Items.Get()
	idx := l.Selected.Get()
	for range len(items) {
		idx--
		if idx < 0 {
			idx = len(items) - 1
		}
		if !items[idx].Disabled {
			l.Selected.Set(idx)
			return
		}
	}
}

// moveDown increments selection, skipping disabled items.
func (l *List) moveDown() {
	items := l.Items.Get()
	idx := l.Selected.Get()
	for range len(items) {
		idx++
		if idx >= len(items) {
			idx = 0
		}
		if !items[idx].Disabled {
			l.Selected.Set(idx)
			return
		}
	}
}

// KeyMap defines keyboard bindings for the List.
func (l *List) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyUp, func(tui.KeyEvent) { l.moveUp() }),
		tui.OnFocused(tui.KeyDown, func(tui.KeyEvent) { l.moveDown() }),
		tui.OnFocused(tui.KeyEnter, func(tui.KeyEvent) {
			items := l.Items.Get()
			idx := l.Selected.Get()
			if idx < 0 || idx >= len(items) {
				return
			}
			item := items[idx]
			if item.Disabled {
				return
			}
			if l.OnSelect != nil {
				l.OnSelect(item)
			}
		}),
	}
}

// Focus marks the list as focused.
func (l *List) Focus() {
	l.focused.Set(true)
}

// Blur marks the list as blurred.
func (l *List) Blur() {
	l.focused.Set(false)
}

// IsFocusable reports true.
func (l *List) IsFocusable() bool {
	return true
}

// IsTabStop reports true.
func (l *List) IsTabStop() bool {
	return true
}

// IsFocused reports whether the list is currently focused.
func (l *List) IsFocused() bool {
	return l.focused.Get()
}

// HandleEvent is required by Focusable.
func (l *List) HandleEvent(tui.Event) bool {
	return false
}

// Debug registration.
func init() {
	Register("list", func(app *tui.App) (tui.Component, []DebugControl) {
		items := tui.NewStateForApp(app, []ListItem{
			{ID: "a", Label: "Alpha", Description: "First option"},
			{ID: "b", Label: "Bravo", Description: "Second option"},
			{ID: "c", Label: "Charlie (Disabled)", Description: "Third option (skipped)", Disabled: true},
			{ID: "d", Label: "Delta", Description: "Fourth option"},
		})
		selected := tui.NewStateForApp(app, 0)
		comp := NewList(items, selected, nil)
		return comp, []DebugControl{
			{Label: "selected", Type: "text", State: intState(selected)},
		}
	})
}

func intState(s *tui.State[int]) *tui.State[string] {
	state := tui.NewState("0")
	s.Bind(func(v int) { state.Set(fmt.Sprintf("%d", v)) })
	return state
}

// Render builds the list element tree.
templ (l *List) Render() {
	<div class="flex-col grow" onFocus={func(*tui.Element) { l.Focus() }} onBlur={func(*tui.Element) { l.Blur() }} scrollable={tui.ScrollVertical} autoFocus={true}>
		for i, item := range l.Items.Get() {
			<div class="flex-row px-1">
				if i == l.Selected.Get() {
					<span textStyle={theme.SelectedStyle()}>{item.Label}</span>
				} else if item.Disabled {
					<span class="text-dim italic">{item.Label}</span>
				} else {
					<span>{item.Label}</span>
				}
				if item.Description != "" {
					<span class="text-dim italic">{" — " + item.Description}</span>
				}
			</div>
		}
	</div>
}
