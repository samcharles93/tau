package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// Run launches the interactive chat TUI using taui's inline renderer. It blocks
// until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	return RunInline(ctx, runtime, cfg)
}
