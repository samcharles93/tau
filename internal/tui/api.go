package tui

import (
	"context"
	"strings"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// notifyBufferSize is the subscriber buffer for external TUI notifications.
const notifyBufferSize = 16

// ModelRefresher is a function the app layer provides that re-discovers
// available models from the configured provider. The TUI calls it asynchronously when the
// user runs /refresh. This keeps the TUI decoupled from provider
// packages (per dependency rules).
type ModelRefresher func(ctx context.Context) ([]tauchat.ChatModelRef, error)

// Config holds parameters for constructing the TUI model.
type Config struct {
	SessionID          string
	ModelName          string
	Provider           string
	AvailableModels    []tauchat.ChatModelRef
	AvailableProviders []string
	NotifyBus          *pubsub.Bus[notify.Notification]
	RefreshModels      ModelRefresher
	ShowReasoning      bool
	Debug              bool
}

// BuiltinCommandNames returns built-in slash command names without the leading slash.
func BuiltinCommandNames() []string {
	commands := []string{
		"new",
		"system",
		"model",
		"refresh",
		"reload",
		"reasoning",
		"settings",
		"session",
		"resume",
		"debug",
		"exit",
	}
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(command)), "/")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
