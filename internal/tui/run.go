package tui

import (
	"context"
	"log/slog"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	gotuiui "github.com/samcharles93/tau/internal/tui/gotui"
	"github.com/samcharles93/tau/internal/tui/notify"
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
	sub, err := runtime.SubscribeEvents(eventBufferSize)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	notifySub := cfg.notifyBusSubscription()
	defer notifySub.Unsubscribe()

	return gotuiui.Run(ctx, runtime, sub, gotuiui.Config{
		SessionID:          cfg.SessionID,
		ModelName:          cfg.ModelName,
		Provider:           cfg.Provider,
		AvailableModels:    cfg.AvailableModels,
		AvailableProviders: cfg.AvailableProviders,
		NotifySub:          notifySub,
		RefreshModels:      gotuiui.ModelRefresher(cfg.RefreshModels),
		ShowReasoning:      cfg.ShowReasoning,
	})
}
