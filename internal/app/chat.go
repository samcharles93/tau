package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/extensions"
	"github.com/samcharles93/tau/internal/platform"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/streaming"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// ChatOptions holds the parameters for launching an interactive chat session.
type ChatOptions struct {
	Config       tauconfig.Config
	Provider     tauconfig.ProviderConfig
	Insecure     bool
	Model        string
	SystemPrompt string
	MaxTokens    int
	Temperature  float64
	Version      string
}

// RunChat orchestrates an interactive chat session: resolves tokens and model,
// creates the coordinator, then launches the TUI.
func RunChat(ctx context.Context, opts ChatOptions) error {
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return err
	}

	allModels, discoverErr := provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	coordinator, err := newCoordinator(ctx, opts, bearerToken)
	if err != nil {
		return err
	}
	defer coordinator.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	config := buildSessionConfig(opts, model)

	if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
		return err
	}

	notifyBus := pubsub.New[notify.Notification]()
	defer notifyBus.Close()

	available := buildModelRefs(allModels)

	// Build a refresher closure that captures the provider and token so the
	// TUI can re-discover models without importing infrastructure packages.
	refresher := buildModelRefresher(opts.Provider, bearerToken, opts.Insecure)

	tuiCfg := tui.Config{
		SessionID:          sessionID,
		ModelName:          model.ID,
		Provider:           opts.Provider.Name,
		AvailableModels:    available,
		AvailableProviders: tauconfig.ProviderNames(opts.Config),
		NotifyBus:          notifyBus,
		RefreshModels:      refresher,
		ShowReasoning:      opts.Config.UI.ShowReasoning,
		Debug:              isDevel(opts.Version, opts.Config),
	}

	// If model discovery failed at startup, notify the user in the TUI
	// rather than refusing to start.
	if discoverErr != nil {
		_ = notifyBus.Publish(ctx, "notifications", notify.Notification{
			Message:  "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:    notify.LevelWarn,
			Duration: 8 * time.Second, // longer for startup warnings
		})
	}

	return tui.Run(ctx, coordinator, tuiCfg)
}

// buildModelRefresher returns a ModelRefresher closure that re-discovers
// models from the configured provider. The closure captures the provider and token so the TUI
// does not need to import infrastructure packages.
func buildModelRefresher(selectedProvider tauconfig.ProviderConfig, bearerToken string, insecure bool) tui.ModelRefresher {
	return func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		models, err := provider.DiscoverModels(ctx, selectedProvider, bearerToken, insecure)
		if err != nil {
			return nil, err
		}
		return buildModelRefs(models), nil
	}
}

func buildSessionConfig(opts ChatOptions, model tauchat.ChatModelRef) tauchat.ChatSessionConfig {
	return tauchat.ChatSessionConfig{
		Provider:     opts.Provider,
		Model:        model,
		SystemPrompt: opts.SystemPrompt,
		Parameters: tauchat.ChatParameters{
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
		},
	}
}

func pickModel(models []provider.Model, requestedModel, defaultModel, baseURL string) (tauchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(defaultModel)
	}
	if selectedModel == "" {
		return tauchat.ChatModelRef{}, errors.New("chat model is required; pass --model or set default_model")
	}

	for _, model := range models {
		if model.ID != selectedModel {
			continue
		}
		if !model.Ready {
			return tauchat.ChatModelRef{}, fmt.Errorf("model %q is not ready", selectedModel)
		}
		return tauchat.ChatModelRef{
			ID:     model.ID,
			URL:    model.URL,
			Ready:  model.Ready,
			Config: model.Config,
		}, nil
	}

	return tauchat.ChatModelRef{ID: selectedModel, URL: strings.TrimRight(baseURL, "/"), Ready: true}, nil
}

func buildModelRefs(models []provider.Model) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		refs = append(refs, tauchat.ChatModelRef{
			ID:     m.ID,
			URL:    m.URL,
			Ready:  m.Ready,
			Config: m.Config,
		})
	}
	return refs
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry and config.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string) (*agent.Coordinator, error) {
	cwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	if err := tools.RegisterBuiltins(registry, cwd); err != nil {
		return nil, fmt.Errorf("registering built-in tools: %w", err)
	}
	extensionManager, err := extensions.NewManager(extensions.Config{
		WorkingDir:       cwd,
		Sources:          extensions.SourcesFromConfig(cwd, opts.Config.Extensions),
		Disabled:         opts.Config.Extensions.Disabled,
		ReservedCommands: tui.BuiltinCommandNames(),
		Registry:         registry,
	})
	if err != nil {
		return nil, fmt.Errorf("creating extension manager: %w", err)
	}
	if err := extensionManager.Load(ctx); err != nil {
		return nil, fmt.Errorf("loading extensions: %w", err)
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		TokenSource:   staticTokenSource(bearerToken),
		Streamer:      streaming.OpenAIStreamer{Insecure: opts.Insecure},
		Registry:      registry,
		ShowReasoning: opts.Config.UI.ShowReasoning,
		InteractiveUI: true,
		ExtensionReloader: extensionReloader{
			manager: extensionManager,
		},
		OnSessionStart: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventSessionStart, eventContext)
		},
		OnSessionShutdown: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventSessionShutdown, eventContext)
		},
		OnToolStarted: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventToolCallStarted, eventContext)
		},
		OnToolCompleted: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventToolCallCompleted, eventContext)
		},
		OnReasoningDelta: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventReasoningDelta, eventContext)
		},
		OnClose:       extensionManager.Unload,
		StartupEvents: extensionStartupEvents(extensionManager.Snapshot()),
	})
	if err != nil {
		extensionManager.Unload()
		return nil, err
	}
	return coordinator, nil
}

func extensionStartupEvents(snapshot extensions.Snapshot) []tauchat.ChatEvent {
	events := []tauchat.ChatEvent{tauchat.ExtensionCommandsChangedEvent{
		Commands:   extensionCommands(snapshot.Commands),
		OccurredAt: time.Now().UTC(),
	}}
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.Severity != extensions.SeverityError {
			continue
		}
		message := "Extension error"
		if diagnostic.ExtensionName != "" {
			message += " (" + diagnostic.ExtensionName + ")"
		}
		message += ": " + diagnostic.Message
		events = append(events, tauchat.ChatRuntimeErrorEvent{
			Message:    message,
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
	}
	return events
}

type extensionReloader struct {
	manager *extensions.Manager
}

func (r extensionReloader) ReloadExtensions(ctx context.Context, idle bool) (tauchat.ExtensionReloadResult, error) {
	if r.manager == nil {
		return tauchat.ExtensionReloadResult{}, errors.New("extension manager is not available")
	}
	if err := r.manager.ReloadIfIdle(ctx, idle); err != nil {
		return tauchat.ExtensionReloadResult{}, err
	}
	snapshot := r.manager.Snapshot()
	return tauchat.ExtensionReloadResult{
		ExtensionCount: len(snapshot.Extensions),
		Diagnostics:    extensionDiagnostics(snapshot.Diagnostics),
		Commands:       extensionCommands(snapshot.Commands),
	}, nil
}

func (r extensionReloader) ExtensionCommands() []tauchat.ExtensionCommand {
	if r.manager == nil {
		return nil
	}
	return extensionCommands(r.manager.Snapshot().Commands)
}

func (r extensionReloader) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error) {
	if r.manager == nil {
		return "", errors.New("extension manager is not available")
	}
	ui, _ := uiBridge.(tools.UIBridge)
	return r.manager.ExecuteCommand(ctx, name, args, ui)
}

func extensionDiagnostics(diagnostics []extensions.Diagnostic) []tauchat.ExtensionDiagnostic {
	out := make([]tauchat.ExtensionDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, tauchat.ExtensionDiagnostic{
			Path:          diagnostic.Path,
			ExtensionName: diagnostic.ExtensionName,
			Severity:      string(diagnostic.Severity),
			Message:       diagnostic.Message,
		})
	}
	return out
}

func extensionCommands(commands []extensions.Command) []tauchat.ExtensionCommand {
	out := make([]tauchat.ExtensionCommand, 0, len(commands))
	for _, command := range commands {
		out = append(out, tauchat.ExtensionCommand{
			Name:          command.Name,
			Description:   command.Description,
			ExtensionName: command.ExtensionName,
		})
	}
	return out
}

func staticTokenSource(token string) platform.TokenSource {
	trimmed := strings.TrimSpace(token)
	return func(ctx context.Context, _ tauconfig.ProviderConfig) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if trimmed == "" {
			return "", nil
		}
		return trimmed, nil
	}
}

func newID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	suffix := base64.RawURLEncoding.EncodeToString(bytes)
	return prefix + "_" + suffix, nil
}

func isDevel(version string, cfg tauconfig.Config) bool {
	if cfg.Debug {
		return true
	}
	v := strings.ToLower(version)
	return strings.Contains(v, "dev") || strings.Contains(v, "none")
}
