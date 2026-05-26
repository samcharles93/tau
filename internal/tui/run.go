package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// eventBufferSize is the subscriber buffer for runtime events in
// interactive mode. Larger than one-shot because streaming deltas
// arrive at high frequency while the TUI renders.
const eventBufferSize = 128

// Run launches the interactive chat TUI. It blocks until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg Config) error {
	sub, err := runtime.SubscribeEvents(eventBufferSize)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	model := New(ctx, runtime, sub, cfg)
	defer func() {
		if model.notifySub != nil {
			model.notifySub.Unsubscribe()
		}
	}()

	p := tea.NewProgram(model, tea.WithContext(ctx))

	_, err = p.Run()
	return err
}
