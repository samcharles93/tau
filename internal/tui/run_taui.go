package tui

import (
	"context"
	"fmt"
	"log/slog"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/pkg/taui"
)

// RunInline launches the chat TUI using taui's inline renderer.
func RunInline(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	if cfg.Bus == nil {
		return fmt.Errorf("event bus is required for TUI")
	}

	tuiClient := cfg.Bus.Client("tui")
	defer tuiClient.Close()

	chatSub := eventbus.Subscribe[tauchat.ChatEvent](tuiClient)
	defer chatSub.Close()

	// Subscribing the usage tracker to chat.MetricEvent is what activates the
	// coordinator's metric emission (it's a no-op until something subscribes).
	metricsClient := cfg.Bus.Client("tui-metrics")
	defer metricsClient.Close()
	tracker := metrics.NewUsageTracker(metricsClient)
	defer tracker.Close()

	// FileSubscriber: when MetricsConfig.Dir is set, append every MetricEvent
	// as JSONL to the configured directory for offline analysis.
	if cfg.MetricsConfig.Dir != "" {
		fsClient := cfg.Bus.Client("tui-metrics-file")
		fileSub, err := metrics.NewFileSubscriber(fsClient, cfg.MetricsConfig.Dir)
		if err != nil {
			// Metrics file export is best-effort; never block startup.
			slog.Warn("metrics file subscriber unavailable", "err", err)
			fsClient.Close()
		} else {
			defer fileSub.Close()
			defer fsClient.Close()
		}
	}

	term := taui.NewProcessTerminal()
	engine := taui.NewTUI(term)

	chat := newInlineChat(ctx, engine, runtime, chatSub, tracker, cfg)

	// Run the OnReady hook now that the TUI is subscribed to bus events.
	// Used to load plugins at the right moment so events are received.
	if cfg.OnReady != nil {
		cfg.OnReady()
	}

	go func() {
		<-ctx.Done()
		chat.close()
	}()

	engine.Start()
	return nil
}
