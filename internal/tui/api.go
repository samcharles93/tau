package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
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
	// CheckUpdate backs /update: it checks for a newer release and returns the
	// line to show the user. Provided by the app layer, which knows the running
	// binary's version. Nil disables the command.
	CheckUpdate     func(context.Context) (string, error)
	ShowReasoning   bool
	ReasoningEffort string
	Debug           bool
	// WebURL is the local address of the optional web UI. Empty means no web UI.
	WebURL string
	// OnReady is called after the TUI subscribes to bus events but before it
	// blocks on the render loop. Used to defer plugin loading so the TUI
	// receives the initial ExtensionCommandsChangedEvent on the bus.
	OnReady func()
	// MetricsConfig controls observability export (file path for JSONL,
	// session persistence, TUI widget toggles).
	MetricsConfig tauconfig.MetricsConfig

	// ToolCallsDefaultCollapsed starts each new live tool-call group in its
	// one-line collapsed form rather than showing all rows.
	ToolCallsDefaultCollapsed bool

	// NewTUI selects the Bubbletea-based TUI instead of the legacy
	// taui inline renderer. True by default; --legacy-tui sets this false.
	NewTUI bool
}
