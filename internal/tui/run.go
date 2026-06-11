package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/tui/splash"
)

// Run launches the interactive chat TUI. It blocks until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	// Show the τ glyph splash animation first. When the animation completes
	// (or the user presses q/Esc), the splash app exits and we proceed to chat.
	if err := showSplash(ctx); err != nil {
		return err
	}

	if cfg.Bus == nil {
		return fmt.Errorf("event bus is required for TUI")
	}

	tuiClient := cfg.Bus.Client("tui")
	defer tuiClient.Close()

	chatSub := eventbus.Subscribe[tauchat.ChatEvent](tuiClient)
	defer chatSub.Close()

	root := NewChatPanel(ctx, runtime, chatSub, cfg)

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

// splashMarker is a sentinel file created after the first splash run so
// subsequent launches skip the animation. Set TAU_SPLASH=always to override.
const splashMarker = ".splash_shown"

// showSplash runs the τ glyph splash animation in its own full-screen go-tui
// App. It blocks until the animation completes or the user presses q/Esc.
// After the first run it is skipped unless TAU_SPLASH=always.
func showSplash(_ context.Context) error {
	if os.Getenv("TAU_SPLASH") != "always" {
		home, _ := os.UserHomeDir()
		if home != "" {
			marker := filepath.Join(home, ".config", "tau", splashMarker)
			if _, err := os.Stat(marker); err == nil {
				return nil // already shown
			}
			// Ensure the config directory exists, then create the marker
			// after the splash completes successfully.
			defer func() {
				_ = os.MkdirAll(filepath.Dir(marker), 0o700)
				_ = os.WriteFile(marker, nil, 0o600)
			}()
		}
	}

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
