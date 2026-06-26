package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// Run launches the interactive chat TUI using taui's inline renderer. It blocks
// until the user exits.
//
// The splash animation previously shown here depended on go-tui and has been
// moved to _archive/splash pending a taui-native reimplementation.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	return RunInline(ctx, runtime, cfg)
}
