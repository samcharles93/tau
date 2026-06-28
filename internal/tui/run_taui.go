package tui

import (
	"context"
	"fmt"
	"strings"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/pkg/taui"
)

// formatSkillsSummary returns a compact one-liner describing the loaded
// skill catalog, or empty string if there are no skills to report.
func formatSkillsSummary(skills []tauchat.SkillInfo) string {
	if len(skills) == 0 {
		return ""
	}
	var userCount, projectCount int
	for _, s := range skills {
		switch s.Scope {
		case "user":
			userCount++
		case "project":
			projectCount++
		}
	}
	parts := []string{}
	if userCount > 0 {
		parts = append(parts, fmt.Sprintf("%d user", userCount))
	}
	if projectCount > 0 {
		parts = append(parts, fmt.Sprintf("%d project", projectCount))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d", len(skills)))
	}
	return "skills: " + strings.Join(parts, ", ")
}

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

	// Show startup notification for loaded skills.
	if skills := formatSkillsSummary(cfg.InitialSkills); skills != "" {
		engine.PrintAbove("%s", chat.grey(skills))
	}

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
