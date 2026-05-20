package completions

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap defines the key bindings for the completions component.
type KeyMap struct {
	Down   key.Binding
	Up     key.Binding
	Select key.Binding
	Cancel key.Binding
}

// DefaultKeyMap returns the default key bindings for completions.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "next"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "prev"),
		),
		Select: key.NewBinding(
			key.WithKeys("tab", "enter"),
			key.WithHelp("tab/enter", "select"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
	}
}
