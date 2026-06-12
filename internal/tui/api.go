package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// ModelRefresher is a function the app layer provides that re-discovers
// available models from the configured provider. The TUI calls it asynchronously when the
// user runs /refresh. This keeps the TUI decoupled from provider
// packages (per dependency rules).
type ModelRefresher func(ctx context.Context) ([]tauchat.ChatModelRef, error)

// TUIConfig holds parameters for constructing the TUI model.
type TUIConfig struct {
	SessionID          string
	ModelName          string
	Provider           string
	AvailableModels    []tauchat.ChatModelRef
	AvailableProviders []string
	// InitialCommands is the command registry snapshot at startup.
	// The registry owns command state; bus events deliver deltas.
	InitialCommands []tauchat.CommandRef
	Bus             *eventbus.Bus
	RefreshModels   ModelRefresher
	ShowReasoning   bool
	Debug           bool
}
