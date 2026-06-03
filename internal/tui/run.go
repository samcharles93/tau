package tui

import (
	"context"
	"fmt"
	"log/slog"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/internal/tui/splash"
)

// eventBufferSize is the subscriber buffer for runtime events in
// interactive mode. Larger than one-shot because streaming deltas
// arrive at high frequency while the TUI renders.
const eventBufferSize = 128

func (cfg Config) notifyBusSubscription() *pubsub.Subscription[notify.Notification] {
	if cfg.NotifyBus == nil {
		return nil
	}
	notifySub, err := cfg.NotifyBus.Subscribe("notifications", notifyBufferSize)
	if err != nil {
		slog.Warn("notification subscription failed", "err", err)
		return nil
	}
	return notifySub
}

// Run launches the interactive chat TUI. It blocks until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg Config) error {
	// Show the τ glyph splash animation first. When the animation completes
	// (or the user presses q/Esc), the splash app exits and we proceed to chat.
	if err := showSplash(ctx); err != nil {
		return err
	}

	sub, err := runtime.SubscribeEvents(eventBufferSize)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	notifySub := cfg.notifyBusSubscription()
	if notifySub != nil {
		defer notifySub.Unsubscribe()
	}

	root := NewChatPanel(ctx, runtime, sub, notifySub, cfg)

	app, err := gt.NewApp(
		gt.WithRootComponent(root),
		gt.WithInlineHeight(3),
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

// showSplash runs the τ glyph splash animation in its own full-screen go-tui
// App. It blocks until the animation completes or the user presses q/Esc.
func showSplash(_ context.Context) error {
	splashApp, err := gt.NewApp(
		gt.WithRootComponent(splash.New()),
	)
	if err != nil {
		return fmt.Errorf("creating splash app: %w", err)
	}
	defer splashApp.Close()

	if err := splashApp.Run(); err != nil {
		return fmt.Errorf("splash animation: %w", err)
	}
	return nil
}
