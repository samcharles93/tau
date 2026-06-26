package tui

import (
	"context"
	"fmt"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/pkg/taui"
)

// RunInline launches the chat TUI using taui's inline renderer. Selected by
// TAU_TUI=taui (checked in internal/tui/run.go or internal/app).
func RunInline(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	if cfg.Bus == nil {
		return fmt.Errorf("event bus is required for TUI")
	}

	tuiClient := cfg.Bus.Client("tui")
	defer tuiClient.Close()

	chatSub := eventbus.Subscribe[tauchat.ChatEvent](tuiClient)
	defer chatSub.Close()

	term := taui.NewProcessTerminal()
	engine := taui.NewTUI(term)

	chat := newInlineChat(ctx, engine, runtime, chatSub, cfg)

	go func() {
		<-ctx.Done()
		chat.close()
	}()

	engine.Start()
	return nil
}
