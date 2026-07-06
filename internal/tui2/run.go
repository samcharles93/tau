// Package tui2 implements a new Bubbletea v2-based interactive chat TUI
// as a sibling to the legacy taui inline renderer (internal/tui). Both
// frontends share the same eventbus.Bus, agent.Coordinator, and plugin
// contracts — tui2 is just a different renderer.
//
// It is gated behind the --new-tui flag.
package tui2

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

// Run launches the Bubbletea v2 chat TUI. It creates bus clients, subscribes
// to chat events, wires metrics tracking, and blocks until the user exits.
//
// Parameters are passed individually rather than via TUIConfig to avoid a
// circular import between internal/tui (which calls this function) and
// internal/tui2.
func Run(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	bus *eventbus.Bus,
	onReady func(),
	sessionID, modelName, provider string,
	metricsCfg tauconfig.MetricsConfig,
	availableModels []tauchat.ChatModelRef,
	refresh func(context.Context) ([]tauchat.ChatModelRef, error),
	showReasoning bool,
	reasoningEffort string,
	webURL string,
	debug bool,
) error {
	if bus == nil {
		return fmt.Errorf("tui2: event bus is required")
	}

	// Create a dedicated bus client for this TUI instance.
	tuiClient := bus.Client("tui2")
	defer tuiClient.Close()

	chatSub := eventbus.Subscribe[tauchat.ChatEvent](tuiClient)
	defer chatSub.Close()

	// Subscribing the usage tracker to chat.MetricEvent is what activates the
	// coordinator's metric emission (it's a no-op until something subscribes).
	metricsClient := bus.Client("tui2-metrics")
	defer metricsClient.Close()
	tracker := metrics.NewUsageTracker(metricsClient)
	defer tracker.Close()

	// FileSubscriber: when MetricsConfig.Dir is set, append every MetricEvent
	// as JSONL to the configured directory for offline analysis.
	if metricsCfg.Dir != "" {
		fsClient := bus.Client("tui2-metrics-file")
		fileSub, err := metrics.NewFileSubscriber(fsClient, metricsCfg.Dir)
		if err != nil {
			slog.Warn("metrics file subscriber unavailable (tui2)", "err", err)
			fsClient.Close()
		} else {
			defer fileSub.Close()
			defer fsClient.Close()
		}
	}

	// Run the OnReady hook after subscribing but before the render loop so
	// that plugins load and publish their initial events while the TUI is
	// already listening.
	if onReady != nil {
		onReady()
	}

	m := newModel(ctx, runtime, chatSub, sessionID, modelName, provider, availableModels, refresh, showReasoning, reasoningEffort, tracker, webURL, debug)

	// Derive the colour profile from the environment rather than letting Bubble
	// Tea auto-detect it with OSC 10/11 queries. Those query responses can leak
	// into the captured output when stdin/stdout are not a real terminal. Falls
	// back to TrueColor because tau's theme already emits 24-bit RGB escapes.
	profile := colorprofile.Env(os.Environ())
	if profile == colorprofile.Unknown {
		profile = colorprofile.TrueColor
	}

	p := tea.NewProgram(
		m,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(), // tau manages signals at the app layer
		tea.WithColorProfile(profile),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui2: %w", err)
	}
	return nil
}
