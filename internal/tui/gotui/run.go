package gotui

import (
	"context"
	"fmt"

	gotui "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
)

// Run launches the interactive chat TUI using go-tui.
func Run(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	eventSub *pubsub.Subscription[tauchat.ChatEvent],
	cfg Config,
) error {
	root := NewChatPanel(ctx, runtime, eventSub, cfg)

	app, err := gotui.NewApp(
		gotui.WithRootComponent(root),
		gotui.WithMouse(),
	)
	if err != nil {
		return fmt.Errorf("creating go-tui app: %w", err)
	}
	defer app.Close()

	if err := app.Run(); err != nil {
		return fmt.Errorf("running go-tui app: %w", err)
	}
	return nil
}
