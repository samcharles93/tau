package tui

import "charm.land/bubbles/v2/key"

// keyMap defines keybindings for help display. Implements key.Map.
type keyMap struct {
	Submit  key.Binding
	NewChat key.Binding
	Model   key.Binding
	System  key.Binding
	Palette key.Binding
	Picker  key.Binding
	Escape  key.Binding
	Quit    key.Binding
	Help    key.Binding
}

// ShortHelp returns bindings shown in the compact status bar view.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Palette, k.Picker, k.Help}
}

// FullHelp returns bindings for the expanded help view.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.NewChat, k.Model, k.System},
		{k.Palette, k.Picker, k.Escape, k.Quit},
	}
}

func newKeyMap() keyMap {
	return keyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		NewChat: key.NewBinding(
			key.WithKeys("/new"),
			key.WithHelp("/new", "new chat"),
		),
		Model: key.NewBinding(
			key.WithKeys("/model"),
			key.WithHelp("/model", "switch model"),
		),
		System: key.NewBinding(
			key.WithKeys("/system"),
			key.WithHelp("/system", "set system prompt"),
		),
		Palette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		Picker: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "models"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+cx2", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	}
}
