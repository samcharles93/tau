package tui

import (
	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	tea "charm.land/bubbletea/v2"
)

// Run launches the interactive chat TUI. It blocks until the user exits.
func Run(runtime *aimchat.Runtime, cfg Config) error {
	sub, err := runtime.SubscribeEvents(128)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	model := New(runtime, sub, cfg)
	p := tea.NewProgram(model) // model is *Model, satisfies tea.Model interface

	_, err = p.Run()
	return err
}
