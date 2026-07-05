package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui2"
)

// Run launches the interactive chat TUI. When cfg.NewTUI is true it delegates
// to the new Bubbletea-based frontend; otherwise it uses the legacy taui
// inline renderer. It blocks until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	if cfg.NewTUI {
		return tui2.Run(ctx, runtime, cfg.Bus, cfg.OnReady, cfg.SessionID, cfg.ModelName, cfg.Provider, cfg.MetricsConfig, cfg.AvailableModels, cfg.RefreshModels, cfg.WebURL, cfg.Debug)
	}
	return RunInline(ctx, runtime, cfg)
}
